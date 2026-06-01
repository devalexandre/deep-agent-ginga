package agent

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/devalexandre/agno-golang/agno/skill"
)

type multiSkillsLoader struct {
	loaders []skill.SkillLoader

	mu               sync.Mutex
	cached           []skill.Skill
	loaded           bool
	loadCalls        int
	cacheHits        int
	lastLoadDuration time.Duration
}

func newSkillsLoader(config DeepAgentConfig) (skill.SkillLoader, error) {
	var loaders []skill.SkillLoader

	// Built-in embedded skills (validated). Empty when materialization failed.
	if builtin := resolveSkillsPath(); builtin != "" {
		loaders = append(loaders, skill.NewLocalSkills(builtin))
	}

	if strings.TrimSpace(config.SkillsPath) != "" {
		path, err := expandHome(config.SkillsPath)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve skills path: %w", err)
		}
		loaders = append(loaders, skill.NewLocalSkills(path, skill.WithValidation(false)))
	}

	for _, source := range uniqueSkills(config.SkillURLs) {
		path, err := resolveRemoteSkills(source, config.SkillsCacheDir)
		if err != nil {
			return nil, err
		}
		loaders = append(loaders, skill.NewLocalSkills(path, skill.WithValidation(false)))
	}

	return &multiSkillsLoader{loaders: loaders}, nil
}

func (m *multiSkillsLoader) Load() ([]skill.Skill, error) {
	startedAt := time.Now()
	m.mu.Lock()
	m.loadCalls++
	if m.loaded {
		m.cacheHits++
		cached := cloneSkills(m.cached)
		m.lastLoadDuration = time.Since(startedAt)
		m.mu.Unlock()
		return cached, nil
	}
	m.mu.Unlock()

	var loaded []skill.Skill
	for _, loader := range m.loaders {
		skills, err := loader.Load()
		if err != nil {
			log.Printf("Warning: failed to load skills: %v", err)
			continue
		}
		loaded = append(loaded, skills...)
	}

	m.mu.Lock()
	m.cached = cloneSkills(loaded)
	m.loaded = true
	m.lastLoadDuration = time.Since(startedAt)
	m.mu.Unlock()

	return cloneSkills(loaded), nil
}

func (m *multiSkillsLoader) CacheStats() SkillsLoaderCacheStats {
	m.mu.Lock()
	defer m.mu.Unlock()

	return SkillsLoaderCacheStats{
		LoadCalls:        m.loadCalls,
		CacheHits:        m.cacheHits,
		LoadedSkills:     len(m.cached),
		LastLoadDuration: m.lastLoadDuration,
	}
}

func cloneSkills(values []skill.Skill) []skill.Skill {
	if len(values) == 0 {
		return nil
	}
	result := make([]skill.Skill, len(values))
	copy(result, values)
	return result
}

func resolveRemoteSkills(source, cacheDir string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", fmt.Errorf("skills URL cannot be empty")
	}

	normalized, err := normalizeSkillsURL(source)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(cacheDir) == "" {
		cacheDir, err = defaultSkillsCacheDir()
		if err != nil {
			return "", err
		}
	}

	cacheID := cacheKey(source)
	target := filepath.Join(cacheDir, cacheID)
	if hasSkillContent(target) {
		return target, nil
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}

	archivePath, err := downloadSkillsArchive(normalized, cacheDir)
	if err != nil {
		return "", err
	}
	defer os.Remove(archivePath)

	tmpDir, err := os.MkdirTemp(cacheDir, cacheID+"-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	if err := unzipSkillsArchive(archivePath, tmpDir); err != nil {
		return "", err
	}

	skillRoot, err := findSkillRoot(tmpDir)
	if err != nil {
		return "", fmt.Errorf("failed to find skills in %s: %w", source, err)
	}

	if err := os.RemoveAll(target); err != nil {
		return "", err
	}
	if err := os.Rename(skillRoot, target); err != nil {
		return "", err
	}

	return target, nil
}

func normalizeSkillsURL(source string) (string, error) {
	parsed, err := url.Parse(source)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("skills URL must use http or https: %s", source)
	}

	if parsed.Host == "github.com" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 2 {
			owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
			branch := "main"
			if len(parts) >= 4 && parts[2] == "tree" && parts[3] != "" {
				branch = parts[3]
			}
			return fmt.Sprintf("https://codeload.github.com/%s/%s/zip/refs/heads/%s", owner, repo, branch), nil
		}
	}

	return source, nil
}

func downloadSkillsArchive(source, cacheDir string) (string, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(source)
	if err != nil {
		return "", fmt.Errorf("failed to download skills URL %s: %w", source, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("failed to download skills URL %s: HTTP %s", source, resp.Status)
	}

	file, err := os.CreateTemp(cacheDir, "skills-*.zip")
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		os.Remove(file.Name())
		return "", err
	}

	return file.Name(), nil
}

func unzipSkillsArchive(archivePath, targetDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("skills URL must point to a zip archive: %w", err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		name := filepath.Clean(file.Name)
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("unsafe path in skills archive: %s", file.Name)
		}

		target := filepath.Join(targetDir, name)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			src.Close()
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			src.Close()
			dst.Close()
			return err
		}
		src.Close()
		dst.Close()
	}

	return nil
}

func findSkillRoot(root string) (string, error) {
	var candidates []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if hasSkillContent(path) {
			candidates = append(candidates, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("archive does not contain SKILL.md files")
	}

	sort.Slice(candidates, func(i, j int) bool {
		return skillRootScore(root, candidates[i]) < skillRootScore(root, candidates[j])
	})
	return candidates[0], nil
}

func hasSkillContent(path string) bool {
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil {
		return true
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(path, entry.Name(), "SKILL.md")); err == nil {
			return true
		}
	}
	return false
}

func skillRootScore(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 1000
	}
	rel = filepath.ToSlash(rel)
	switch {
	case rel == ".":
		return 0
	case strings.HasSuffix(rel, "internal/skills"):
		return 1
	case filepath.Base(path) == "skills":
		return 2
	default:
		return 10 + strings.Count(rel, "/")
	}
}

func expandHome(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func defaultSkillsCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".skills"), nil
}

func cacheKey(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])[:16]
}

func uniqueSkills(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))

	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			result = append(result, part)
		}
	}

	return result
}
