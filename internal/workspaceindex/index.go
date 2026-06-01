package workspaceindex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	version          = 1
	maxDocFiles      = 40
	maxTestFiles     = 60
	maxEntrypoints   = 40
	maxRelevantFiles = 80
)

var skipDirs = map[string]struct{}{
	".deep-agent":  {},
	".git":         {},
	".ginga":       {},
	".hg":          {},
	".idea":        {},
	".svn":         {},
	".venv":        {},
	".vscode":      {},
	"bin":          {},
	"build":        {},
	"coverage":     {},
	"dist":         {},
	"node_modules": {},
	"target":       {},
	"vendor":       {},
}

type Index struct {
	Version        int             `json:"version"`
	Workspace      string          `json:"workspace"`
	WorkspaceHash  string          `json:"workspace_hash"`
	Fingerprint    string          `json:"fingerprint"`
	GeneratedAt    time.Time       `json:"generated_at"`
	FileCount      int             `json:"file_count"`
	DirCount       int             `json:"dir_count"`
	LanguageCounts map[string]int  `json:"language_counts,omitempty"`
	Documents      []IndexedFile   `json:"documents,omitempty"`
	Entrypoints    []IndexedFile   `json:"entrypoints,omitempty"`
	Tests          []IndexedFile   `json:"tests,omitempty"`
	RelevantFiles  []IndexedFile   `json:"relevant_files,omitempty"`
	Signals        WorkspaceSignal `json:"signals"`
}

type IndexedFile struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	Language string    `json:"language,omitempty"`
	Kind     string    `json:"kind,omitempty"`
}

type WorkspaceSignal struct {
	HasGit     bool     `json:"has_git,omitempty"`
	HasIADir   bool     `json:"has_ia_dir,omitempty"`
	GoModules  []string `json:"go_modules,omitempty"`
	GoWork     bool     `json:"go_work,omitempty"`
	Node       bool     `json:"node,omitempty"`
	Python     bool     `json:"python,omitempty"`
	Rust       bool     `json:"rust,omitempty"`
	Elixir     bool     `json:"elixir,omitempty"`
	Docker     bool     `json:"docker,omitempty"`
	Makefile   bool     `json:"makefile,omitempty"`
	Primary    string   `json:"primary_language,omitempty"`
	BuildFiles []string `json:"build_files,omitempty"`
}

type Options struct {
	CacheRoot string
	Workers   int
}

type BuildResult struct {
	Index     Index
	FromCache bool
	Duration  time.Duration
}

type candidate struct {
	abs  string
	rel  string
	info fs.FileInfo
}

func Build(workspace string, options Options) (Index, error) {
	result, err := BuildDetailed(workspace, options)
	if err != nil {
		return Index{}, err
	}
	return result.Index, nil
}

func BuildDetailed(workspace string, options Options) (BuildResult, error) {
	startedAt := time.Now()

	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resolve workspace: %w", err)
	}

	candidates, dirCount, err := collectCandidates(workspace)
	if err != nil {
		return BuildResult{}, err
	}

	fingerprint := fingerprintCandidates(candidates)
	if cached, ok := loadCached(workspace, options.CacheRoot, fingerprint); ok {
		return BuildResult{Index: cached, FromCache: true, Duration: time.Since(startedAt)}, nil
	}

	workers := options.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers < 2 {
		workers = 2
	}

	files := classifyCandidates(candidates, workers)
	index := assembleIndex(workspace, fingerprint, dirCount, files)
	if options.CacheRoot != "" {
		_ = saveCached(index, options.CacheRoot)
	}
	return BuildResult{Index: index, Duration: time.Since(startedAt)}, nil
}

func DefaultCacheRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ginga-index"), nil
}

func Format(index Index) string {
	if index.FileCount == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("WORKSPACE INDEX\n")
	fmt.Fprintf(&b, "- files: %d\n", index.FileCount)
	fmt.Fprintf(&b, "- directories: %d\n", index.DirCount)
	if index.Signals.Primary != "" {
		fmt.Fprintf(&b, "- primary language: %s\n", index.Signals.Primary)
	}
	if len(index.LanguageCounts) > 0 {
		b.WriteString("- languages: ")
		var parts []string
		for _, key := range sortedKeys(index.LanguageCounts) {
			parts = append(parts, fmt.Sprintf("%s=%d", key, index.LanguageCounts[key]))
		}
		b.WriteString(strings.Join(parts, ", "))
		b.WriteString("\n")
	}
	if len(index.Signals.BuildFiles) > 0 {
		fmt.Fprintf(&b, "- build/config files: %s\n", strings.Join(index.Signals.BuildFiles, ", "))
	}
	if index.Signals.HasIADir {
		b.WriteString("- .ia directory detected\n")
	}

	writeFiles := func(title string, files []IndexedFile, limit int) {
		if len(files) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s\n", title)
		for i, file := range files {
			if i >= limit {
				fmt.Fprintf(&b, "- ... %d more\n", len(files)-limit)
				break
			}
			if file.Language != "" {
				fmt.Fprintf(&b, "- %s (%s)\n", file.Path, file.Language)
				continue
			}
			fmt.Fprintf(&b, "- %s\n", file.Path)
		}
	}

	writeFiles("IMPORTANT DOCS", index.Documents, 20)
	writeFiles("ENTRYPOINTS", index.Entrypoints, 20)
	writeFiles("TEST FILES", index.Tests, 20)
	writeFiles("RELEVANT FILES", index.RelevantFiles, 30)
	return strings.TrimSpace(b.String())
}

func collectCandidates(workspace string) ([]candidate, int, error) {
	var candidates []candidate
	dirCount := 0
	err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipDir(workspace, path) {
				return filepath.SkipDir
			}
			dirCount++
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return nil
		}
		candidates = append(candidates, candidate{
			abs:  path,
			rel:  filepath.ToSlash(rel),
			info: info,
		})
		return nil
	})
	return candidates, dirCount, err
}

func shouldSkipDir(workspace, path string) bool {
	rel, err := filepath.Rel(workspace, path)
	if err != nil || rel == "." {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	name := parts[len(parts)-1]
	_, skip := skipDirs[name]
	return skip
}

func classifyCandidates(candidates []candidate, workers int) []IndexedFile {
	input := make(chan candidate)
	output := make(chan IndexedFile)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range input {
				output <- classifyCandidate(item)
			}
		}()
	}

	go func() {
		for _, item := range candidates {
			input <- item
		}
		close(input)
		wg.Wait()
		close(output)
	}()

	files := make([]IndexedFile, 0, len(candidates))
	for file := range output {
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func classifyCandidate(item candidate) IndexedFile {
	language := languageForPath(item.rel)
	return IndexedFile{
		Path:     item.rel,
		Size:     item.info.Size(),
		ModTime:  item.info.ModTime().UTC(),
		Language: language,
		Kind:     kindForPath(item.rel),
	}
}

func assembleIndex(workspace, fingerprint string, dirCount int, files []IndexedFile) Index {
	index := Index{
		Version:        version,
		Workspace:      workspace,
		WorkspaceHash:  hashString(workspace),
		Fingerprint:    fingerprint,
		GeneratedAt:    time.Now().UTC(),
		FileCount:      len(files),
		DirCount:       dirCount,
		LanguageCounts: map[string]int{},
		Signals: WorkspaceSignal{
			HasGit: filepathExists(filepath.Join(workspace, ".git")),
		},
	}

	for _, file := range files {
		if file.Language != "" {
			index.LanguageCounts[file.Language]++
		}
		if strings.HasPrefix(file.Path, ".ia/") {
			index.Signals.HasIADir = true
		}
		switch file.Kind {
		case "doc":
			index.Documents = appendLimited(index.Documents, file, maxDocFiles)
		case "entrypoint":
			index.Entrypoints = appendLimited(index.Entrypoints, file, maxEntrypoints)
		case "test":
			index.Tests = appendLimited(index.Tests, file, maxTestFiles)
		}
		if isRelevant(file) {
			index.RelevantFiles = appendLimited(index.RelevantFiles, file, maxRelevantFiles)
		}
		updateSignals(&index.Signals, file.Path)
	}

	index.Signals.Primary = primaryLanguage(index.LanguageCounts)
	sortFiles(index.Documents)
	sortFiles(index.Entrypoints)
	sortFiles(index.Tests)
	sortFiles(index.RelevantFiles)
	return index
}

func updateSignals(signals *WorkspaceSignal, path string) {
	name := filepath.Base(path)
	switch name {
	case "go.mod":
		signals.GoModules = append(signals.GoModules, path)
		signals.BuildFiles = appendUniqueString(signals.BuildFiles, path)
	case "go.work":
		signals.GoWork = true
		signals.BuildFiles = appendUniqueString(signals.BuildFiles, path)
	case "package.json", "pnpm-lock.yaml", "yarn.lock", "package-lock.json":
		signals.Node = true
		signals.BuildFiles = appendUniqueString(signals.BuildFiles, path)
	case "pyproject.toml", "requirements.txt", "poetry.lock":
		signals.Python = true
		signals.BuildFiles = appendUniqueString(signals.BuildFiles, path)
	case "Cargo.toml":
		signals.Rust = true
		signals.BuildFiles = appendUniqueString(signals.BuildFiles, path)
	case "mix.exs":
		signals.Elixir = true
		signals.BuildFiles = appendUniqueString(signals.BuildFiles, path)
	case "Dockerfile", "docker-compose.yml", "docker-compose.yaml":
		signals.Docker = true
		signals.BuildFiles = appendUniqueString(signals.BuildFiles, path)
	case "Makefile":
		signals.Makefile = true
		signals.BuildFiles = appendUniqueString(signals.BuildFiles, path)
	}
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".ex", ".exs":
		return "elixir"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".sh":
		return "shell"
	default:
		return ""
	}
}

func kindForPath(path string) string {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	if strings.EqualFold(filepath.Ext(path), ".md") {
		return "doc"
	}
	if strings.HasSuffix(lower, "_test.go") || strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.") {
		return "test"
	}
	if base == "main.go" || strings.HasPrefix(path, "cmd/") || base == "main.py" || base == "index.js" || base == "index.ts" {
		return "entrypoint"
	}
	return "source"
}

func isRelevant(file IndexedFile) bool {
	if file.Kind == "doc" || file.Kind == "entrypoint" || file.Kind == "test" {
		return true
	}
	switch filepath.Base(file.Path) {
	case "go.mod", "go.work", "package.json", "pyproject.toml", "Cargo.toml", "mix.exs", "Makefile", "Dockerfile":
		return true
	}
	return false
}

func fingerprintCandidates(candidates []candidate) string {
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].rel < candidates[j].rel })
	h := sha256.New()
	for _, item := range candidates {
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", item.rel, item.info.Size(), item.info.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

func loadCached(workspace, cacheRoot, fingerprint string) (Index, bool) {
	if cacheRoot == "" {
		return Index{}, false
	}
	data, err := os.ReadFile(cachePath(cacheRoot, workspace))
	if err != nil {
		return Index{}, false
	}
	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return Index{}, false
	}
	if index.Version != version || index.Fingerprint != fingerprint {
		return Index{}, false
	}
	return index, true
}

func saveCached(index Index, cacheRoot string) error {
	path := cachePath(cacheRoot, index.Workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func cachePath(cacheRoot, workspace string) string {
	return filepath.Join(cacheRoot, hashString(workspace)+".json")
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

func filepathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func appendLimited(files []IndexedFile, file IndexedFile, limit int) []IndexedFile {
	if len(files) >= limit {
		return files
	}
	return append(files, file)
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func sortFiles(files []IndexedFile) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}

func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func primaryLanguage(counts map[string]int) string {
	var best string
	bestCount := 0
	for language, count := range counts {
		if language == "markdown" || language == "json" || language == "yaml" || language == "toml" {
			continue
		}
		if count > bestCount || count == bestCount && language < best {
			best = language
			bestCount = count
		}
	}
	return best
}
