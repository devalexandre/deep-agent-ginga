package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/devalexandre/deep-agent-ginga/internal/workspaceindex"
)

const (
	maxMarkdownContextBytes = 24000
	maxMarkdownFileBytes    = 6000
	commandTimeout          = 4 * time.Second
	maxCommandOutputBytes   = 12000
	maxSignalOutputLines    = 24
)

var skippedMarkdownDirs = map[string]struct{}{
	".deep-agent":  {},
	".git":         {},
	".venv":        {},
	"build":        {},
	"coverage":     {},
	"dist":         {},
	"node_modules": {},
	"vendor":       {},
}

type workspaceKnowledgeDetails struct {
	Duration         time.Duration
	BuiltAt          time.Time
	IndexDuration    time.Duration
	IndexFromCache   bool
	IndexFiles       int
	IndexFingerprint string
	LocalSignals     localSignalsDetails
}

type cachedLocalSignals struct {
	Fingerprint string
	Content     string
}

var localSignalsCache = struct {
	mu      sync.Mutex
	entries map[string]cachedLocalSignals
}{
	entries: map[string]cachedLocalSignals{},
}

var localSignalCommandRunner = runSafeCommand

func workspaceKnowledge(workspace string) string {
	knowledge, _ := workspaceKnowledgeWithDetails(workspace)
	return knowledge
}

func workspaceKnowledgeWithDetails(workspace string) (string, workspaceKnowledgeDetails) {
	startedAt := time.Now()
	details := workspaceKnowledgeDetails{}

	var b strings.Builder
	b.WriteString(golangDDDTemplateGuidance())

	indexContext, index, indexDetails := workspaceIndexContextDetailed(workspace)
	details.IndexDuration = indexDetails.Duration
	details.IndexFromCache = indexDetails.FromCache
	details.IndexFiles = index.FileCount
	details.IndexFingerprint = index.Fingerprint
	if indexContext != "" {
		b.WriteString("\n\n")
		b.WriteString(indexContext)
	}

	localSignals, localDetails := workspaceLocalSignalsContext(workspace, index)
	details.LocalSignals = localDetails
	if localSignals != "" {
		b.WriteString("\n\n")
		b.WriteString(localSignals)
	}

	markdown := workspaceMarkdownContext(workspace)
	if markdown != "" {
		b.WriteString("\n\n")
		b.WriteString(markdown)
	}

	details.BuiltAt = time.Now().UTC()
	details.Duration = time.Since(startedAt)
	return b.String(), details
}

func workspaceIndexContext(workspace string) string {
	indexContext, _, _ := workspaceIndexContextDetailed(workspace)
	return indexContext
}

func workspaceIndexContextDetailed(workspace string) (string, workspaceindex.Index, workspaceindex.BuildResult) {
	cacheRoot, err := workspaceindex.DefaultCacheRoot()
	if err != nil {
		result, buildErr := workspaceindex.BuildDetailed(workspace, workspaceindex.Options{})
		if buildErr != nil {
			return "", workspaceindex.Index{}, workspaceindex.BuildResult{}
		}
		return workspaceindex.Format(result.Index), result.Index, result
	}
	result, err := workspaceindex.BuildDetailed(workspace, workspaceindex.Options{CacheRoot: cacheRoot})
	if err != nil {
		return "", workspaceindex.Index{}, workspaceindex.BuildResult{}
	}
	return workspaceindex.Format(result.Index), result.Index, result
}

func workspaceLocalSignalsContext(workspace string, index workspaceindex.Index) (string, localSignalsDetails) {
	startedAt := time.Now()
	details := localSignalsDetails{}

	if strings.TrimSpace(workspace) == "" || index.FileCount == 0 || strings.TrimSpace(index.Fingerprint) == "" {
		details.Duration = time.Since(startedAt)
		return "", details
	}

	workspace = filepath.Clean(workspace)
	localSignalsCache.mu.Lock()
	if cached, ok := localSignalsCache.entries[workspace]; ok && cached.Fingerprint == index.Fingerprint {
		localSignalsCache.mu.Unlock()
		details.CacheHits = 1
		details.Duration = time.Since(startedAt)
		return cached.Content, details
	}
	localSignalsCache.mu.Unlock()

	content := buildLocalSignalsContext(workspace, index)
	localSignalsCache.mu.Lock()
	localSignalsCache.entries[workspace] = cachedLocalSignals{
		Fingerprint: index.Fingerprint,
		Content:     content,
	}
	localSignalsCache.mu.Unlock()

	details.CacheMisses = 1
	details.Duration = time.Since(startedAt)
	return content, details
}

type localSignalTask struct {
	Name    string
	Command string
	Args    []string
}

type localSignalResult struct {
	Order   int
	Title   string
	Summary string
}

func buildLocalSignalsContext(workspace string, index workspaceindex.Index) string {
	tasks := []localSignalTask{{
		Name:    "rg --files --hidden --glob !.git",
		Command: "rg",
		Args:    []string{"--files", "--hidden", "--glob", "!.git"},
	}}

	if index.Signals.HasGit {
		tasks = append(tasks, localSignalTask{
			Name:    "git status --short --untracked-files=normal",
			Command: "git",
			Args:    []string{"status", "--short", "--untracked-files=normal"},
		})
	}
	if len(index.Signals.GoModules) > 0 || index.Signals.GoWork || index.Signals.Primary == "go" {
		tasks = append(tasks, localSignalTask{
			Name:    "go list ./...",
			Command: "go",
			Args:    []string{"list", "./..."},
		})
	}

	if len(tasks) == 0 {
		return ""
	}

	resultsCh := make(chan localSignalResult, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(order int, task localSignalTask) {
			defer wg.Done()
			output, err := localSignalCommandRunner(workspace, task.Command, task.Args, commandTimeout)
			resultsCh <- localSignalResult{
				Order:   order,
				Title:   task.Name,
				Summary: summarizeCommandResult(task.Command, output, err),
			}
		}(i, task)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	results := make([]localSignalResult, 0, len(tasks))
	for result := range resultsCh {
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Order < results[j].Order })

	var b strings.Builder
	b.WriteString("LOCAL SIGNALS\n")
	for _, result := range results {
		if strings.TrimSpace(result.Summary) == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", result.Title)
		for _, line := range strings.Split(strings.TrimSpace(result.Summary), "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	return strings.TrimSpace(b.String())
}

func summarizeCommandResult(command, output string, err error) string {
	if err != nil {
		return fmt.Sprintf("[unavailable] %s", strings.TrimSpace(err.Error()))
	}

	output = strings.TrimSpace(output)
	if output == "" {
		if command == "git" {
			return "clean working tree"
		}
		return "no output"
	}

	lines := strings.Split(output, "\n")
	if command == "rg" {
		summary := make([]string, 0, maxSignalOutputLines+2)
		summary = append(summary, fmt.Sprintf("%d files listed", len(lines)))
		limit := min(len(lines), maxSignalOutputLines)
		summary = append(summary, lines[:limit]...)
		if len(lines) > limit {
			summary = append(summary, fmt.Sprintf("... %d more", len(lines)-limit))
		}
		return strings.Join(summary, "\n")
	}

	limit := min(len(lines), maxSignalOutputLines)
	if len(lines) > limit {
		lines = append(lines[:limit], fmt.Sprintf("... %d more", len(lines)-limit))
	} else {
		lines = lines[:limit]
	}
	return strings.Join(lines, "\n")
}

func runSafeCommand(workspace, command string, args []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	if len(output) > maxCommandOutputBytes {
		output = append(output[:maxCommandOutputBytes], []byte("\n[output truncated]\n")...)
	}

	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return string(output), fmt.Errorf("timeout after %s", timeout)
		}
		return string(output), ctx.Err()
	}
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

func resetLocalSignalsCacheForTests() {
	localSignalsCache.mu.Lock()
	defer localSignalsCache.mu.Unlock()
	localSignalsCache.entries = map[string]cachedLocalSignals{}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func golangDDDTemplateGuidance() string {
	return `GO DDD ARCHITECTURE REFERENCE
When working in Go repositories, prefer the devalexandre/golang-ddd-template shape unless local docs or existing code say otherwise:
- cmd/main.go is the application entry point.
- internal/domain/<context>/ owns business rules, factories, services, repositories, contracts, mocks, and colocated *_test.go files.
- internal/infra/ owns infrastructure adapters such as database and OpenAPI integrations.
- internal/helpers/ owns support packages such as config, errors, and logger.
- Keep business rules out of infrastructure and delivery code.
- Prefer tests beside the code they cover.`
}

func workspaceMarkdownContext(workspace string) string {
	files := collectMarkdownFiles(workspace)
	if len(files) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("WORKSPACE MARKDOWN CONTEXT\n")
	b.WriteString("Read order: .ia markdown first, then other workspace markdown files.\n")

	for _, file := range files {
		if b.Len() >= maxMarkdownContextBytes {
			b.WriteString("\n[markdown context truncated]\n")
			break
		}

		content, err := os.ReadFile(file.abs)
		if err != nil {
			continue
		}

		text := string(content)
		if len(text) > maxMarkdownFileBytes {
			text = text[:maxMarkdownFileBytes] + "\n[file truncated]\n"
		}

		entry := fmt.Sprintf("\n--- %s ---\n%s\n", file.rel, strings.TrimSpace(text))
		remaining := maxMarkdownContextBytes - b.Len()
		if len(entry) > remaining {
			b.WriteString(entry[:remaining])
			b.WriteString("\n[markdown context truncated]\n")
			break
		}
		b.WriteString(entry)
	}

	return strings.TrimSpace(b.String())
}

type markdownFile struct {
	abs string
	rel string
}

func collectMarkdownFiles(workspace string) []markdownFile {
	workspace = filepath.Clean(workspace)
	var iaFiles []markdownFile
	var otherFiles []markdownFile

	_ = filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if entry.IsDir() {
			if shouldSkipMarkdownDir(workspace, path) {
				return filepath.SkipDir
			}
			return nil
		}

		if !isMarkdownFile(path) {
			return nil
		}

		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		file := markdownFile{abs: path, rel: rel}
		if isInIADir(rel) {
			iaFiles = append(iaFiles, file)
			return nil
		}
		otherFiles = append(otherFiles, file)
		return nil
	})

	sortMarkdownFiles(iaFiles)
	sortMarkdownFiles(otherFiles)

	result := make([]markdownFile, 0, len(iaFiles)+len(otherFiles))
	result = append(result, iaFiles...)
	result = append(result, otherFiles...)
	return result
}

func shouldSkipMarkdownDir(workspace, path string) bool {
	rel, err := filepath.Rel(workspace, path)
	if err != nil || rel == "." {
		return false
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	name := parts[len(parts)-1]
	if name == ".ia" {
		return false
	}

	_, skip := skippedMarkdownDirs[name]
	return skip
}

func isMarkdownFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".md")
}

func isInIADir(rel string) bool {
	rel = filepath.ToSlash(rel)
	return rel == ".ia" || strings.HasPrefix(rel, ".ia/")
}

func sortMarkdownFiles(files []markdownFile) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].rel < files[j].rel
	})
}
