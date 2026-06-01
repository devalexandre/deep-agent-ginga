package workspaceindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildIndexesWorkspaceAndUsesCache(t *testing.T) {
	workspace := t.TempDir()
	cache := t.TempDir()
	writeFile(t, workspace, ".ia/rules.md", "# Rules")
	writeFile(t, workspace, "go.mod", "module example.com/app\n")
	writeFile(t, workspace, "cmd/api/main.go", "package main\n")
	writeFile(t, workspace, "internal/service/service_test.go", "package service\n")
	writeFile(t, workspace, "node_modules/ignored.js", "console.log('ignored')\n")

	index, err := Build(workspace, Options{CacheRoot: cache, Workers: 2})
	if err != nil {
		t.Fatalf("build index: %v", err)
	}

	if index.FileCount != 4 {
		t.Fatalf("expected 4 indexed files, got %d", index.FileCount)
	}
	if index.Signals.Primary != "go" {
		t.Fatalf("expected primary language go, got %q", index.Signals.Primary)
	}
	if !index.Signals.HasIADir {
		t.Fatal("expected .ia signal")
	}
	assertFileListed(t, index.Documents, ".ia/rules.md")
	assertFileListed(t, index.Entrypoints, "cmd/api/main.go")
	assertFileListed(t, index.Tests, "internal/service/service_test.go")

	cached, err := Build(workspace, Options{CacheRoot: cache, Workers: 2})
	if err != nil {
		t.Fatalf("build cached index: %v", err)
	}
	if !cached.GeneratedAt.Equal(index.GeneratedAt) {
		t.Fatalf("expected cached generated_at to be reused")
	}

	time.Sleep(time.Nanosecond)
	writeFile(t, workspace, "internal/service/service.go", "package service\n")
	updated, err := Build(workspace, Options{CacheRoot: cache, Workers: 2})
	if err != nil {
		t.Fatalf("build updated index: %v", err)
	}
	if updated.Fingerprint == index.Fingerprint {
		t.Fatalf("expected fingerprint to change after new file")
	}
}

func TestBuildDetailedIncludesTimingAndCacheUsage(t *testing.T) {
	workspace := t.TempDir()
	cache := t.TempDir()
	writeFile(t, workspace, "go.mod", "module example.com/app\n")
	writeFile(t, workspace, "cmd/main.go", "package main\n")

	first, err := BuildDetailed(workspace, Options{CacheRoot: cache, Workers: 2})
	if err != nil {
		t.Fatalf("build detailed (first): %v", err)
	}
	if first.Duration <= 0 {
		t.Fatalf("expected positive build duration on first run, got %s", first.Duration)
	}
	if first.FromCache {
		t.Fatal("expected first build to not come from cache")
	}
	if first.Index.FileCount == 0 {
		t.Fatal("expected first build to include indexed files")
	}

	second, err := BuildDetailed(workspace, Options{CacheRoot: cache, Workers: 2})
	if err != nil {
		t.Fatalf("build detailed (second): %v", err)
	}
	if !second.FromCache {
		t.Fatal("expected second build to come from cache")
	}
	if second.Duration <= 0 {
		t.Fatalf("expected positive build duration on cached run, got %s", second.Duration)
	}
	if second.Index.Fingerprint != first.Index.Fingerprint {
		t.Fatalf("expected cached run fingerprint %q to match first run %q", second.Index.Fingerprint, first.Index.Fingerprint)
	}
}

func TestFormatIncludesUsefulWorkspaceSignals(t *testing.T) {
	index := Index{
		FileCount:      3,
		DirCount:       2,
		LanguageCounts: map[string]int{"go": 2, "markdown": 1},
		Signals: WorkspaceSignal{
			Primary:    "go",
			HasIADir:   true,
			BuildFiles: []string{"go.mod"},
		},
		Documents: []IndexedFile{{Path: ".ia/rules.md", Language: "markdown"}},
		Tests:     []IndexedFile{{Path: "service_test.go", Language: "go"}},
	}

	formatted := Format(index)
	for _, want := range []string{
		"WORKSPACE INDEX",
		"primary language: go",
		".ia directory detected",
		".ia/rules.md",
		"service_test.go",
	} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("expected formatted index to contain %q:\n%s", want, formatted)
		}
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func assertFileListed(t *testing.T, files []IndexedFile, path string) {
	t.Helper()
	for _, file := range files {
		if file.Path == path {
			return
		}
	}
	t.Fatalf("expected %s in %#v", path, files)
}
