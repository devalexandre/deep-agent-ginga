package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCollectMarkdownFilesPrioritizesIADirectory(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, ".ia/architecture.md", "# IA Architecture")
	writeTestFile(t, workspace, ".ia/nested/rules.md", "# IA Rules")
	writeTestFile(t, workspace, "README.md", "# Readme")
	writeTestFile(t, workspace, "docs/guide.md", "# Guide")
	writeTestFile(t, workspace, "vendor/ignored.md", "# Ignored")

	files := collectMarkdownFiles(workspace)

	got := make([]string, 0, len(files))
	for _, file := range files {
		got = append(got, file.rel)
	}

	want := []string{
		".ia/architecture.md",
		".ia/nested/rules.md",
		"README.md",
		"docs/guide.md",
	}

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected markdown order:\nwant %v\ngot  %v", want, got)
	}
}

func TestWorkspaceMarkdownContextIncludesReadOrderAndContent(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, ".ia/architecture.md", "# IA Architecture\nUse DDD.")
	writeTestFile(t, workspace, "README.md", "# Readme\nProject docs.")

	context := workspaceMarkdownContext(workspace)

	assertContains(t, context, "Read order: .ia markdown first")
	assertContains(t, context, "--- .ia/architecture.md ---")
	assertContains(t, context, "Use DDD.")
	assertContains(t, context, "--- README.md ---")

	iaIndex := strings.Index(context, "--- .ia/architecture.md ---")
	readmeIndex := strings.Index(context, "--- README.md ---")
	if iaIndex == -1 || readmeIndex == -1 || iaIndex > readmeIndex {
		t.Fatalf(".ia markdown should appear before README:\n%s", context)
	}
}

func TestWorkspaceKnowledgeIncludesDDDGuidance(t *testing.T) {
	workspace := t.TempDir()

	knowledge := workspaceKnowledge(workspace)

	assertContains(t, knowledge, "GO DDD ARCHITECTURE REFERENCE")
	assertContains(t, knowledge, "internal/domain/<context>/")
	assertContains(t, knowledge, "internal/infra/")
	assertContains(t, knowledge, "internal/helpers/")
}

func TestWorkspaceKnowledgeWithDetailsCachesLocalSignalsByFingerprint(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example.com/app\n")
	writeTestFile(t, workspace, "cmd/main.go", "package main\n")

	resetLocalSignalsCacheForTests()
	originalRunner := localSignalCommandRunner
	defer func() {
		localSignalCommandRunner = originalRunner
	}()

	calls := map[string]int{}
	localSignalCommandRunner = func(workspace, command string, args []string, timeout time.Duration) (string, error) {
		key := command + " " + strings.Join(args, " ")
		calls[key]++
		switch command {
		case "rg":
			return "go.mod\ncmd/main.go\n", nil
		case "go":
			return "example.com/app/cmd\n", nil
		default:
			return "", fmt.Errorf("unexpected command: %s", command)
		}
	}

	knowledge, details := workspaceKnowledgeWithDetails(workspace)
	assertContains(t, knowledge, "LOCAL SIGNALS")
	assertContains(t, knowledge, "rg --files --hidden --glob !.git")
	assertContains(t, knowledge, "2 files listed")
	assertContains(t, knowledge, "go list ./...")

	if details.IndexFiles == 0 {
		t.Fatalf("expected indexed files in knowledge details")
	}
	if details.LocalSignals.CacheMisses != 1 {
		t.Fatalf("expected first run to miss local-signal cache, got %+v", details.LocalSignals)
	}
	if details.LocalSignals.CacheHits != 0 {
		t.Fatalf("expected first run to have no local-signal cache hit, got %+v", details.LocalSignals)
	}

	firstCallCount := len(calls)
	if firstCallCount == 0 {
		t.Fatal("expected local signal commands to run on first knowledge build")
	}

	_, detailsCached := workspaceKnowledgeWithDetails(workspace)
	if detailsCached.LocalSignals.CacheHits != 1 {
		t.Fatalf("expected second run to hit local-signal cache, got %+v", detailsCached.LocalSignals)
	}
	if detailsCached.LocalSignals.CacheMisses != 0 {
		t.Fatalf("expected second run to avoid local-signal cache miss, got %+v", detailsCached.LocalSignals)
	}
	if len(calls) != firstCallCount {
		t.Fatalf("expected no extra local signal command calls after cache hit, before=%d after=%d", firstCallCount, len(calls))
	}
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create test dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

func assertContains(t *testing.T, value, want string) {
	t.Helper()

	if !strings.Contains(value, want) {
		t.Fatalf("expected value to contain %q:\n%s", want, value)
	}
}
