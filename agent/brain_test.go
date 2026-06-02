package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestBrain(t *testing.T) *BrainStore {
	t.Helper()
	store, err := NewBrainStore(t.TempDir(), "My Project")
	if err != nil {
		t.Fatalf("NewBrainStore: %v", err)
	}
	if store.Project() != "my-project" {
		t.Fatalf("project slug = %q, want my-project", store.Project())
	}
	return store
}

func TestBrainRememberAndRecall(t *testing.T) {
	store := newTestBrain(t)

	if err := store.Remember("Build & Test", "Commands", "Run `go test ./...` to test.", false, "", nil); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if err := store.Remember("Architecture", "Layout", "Domain rules live under internal/domain.", false, "", nil); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	// Recall by keyword present only in the build topic.
	matches, err := store.Recall("how do I run the tests", "")
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(matches) != 1 || matches[0].Topic != "Build & Test" {
		t.Fatalf("recall by keyword = %+v, want one Build & Test match", matches)
	}

	// Recall restricted to a topic returns its sections regardless of query.
	matches, err = store.Recall("", "Architecture")
	if err != nil {
		t.Fatalf("Recall topic: %v", err)
	}
	if len(matches) != 1 || matches[0].Subtopic != "Layout" {
		t.Fatalf("recall by topic = %+v, want Architecture/Layout", matches)
	}
}

func TestBrainAppendAndReplace(t *testing.T) {
	store := newTestBrain(t)
	if err := store.Remember("Gotchas", "CGO", "first note", false, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Remember("Gotchas", "CGO", "second note", false, "", nil); err != nil {
		t.Fatal(err)
	}
	matches, _ := store.Recall("", "Gotchas")
	if len(matches) != 1 || !strings.Contains(matches[0].Content, "first note") || !strings.Contains(matches[0].Content, "second note") {
		t.Fatalf("append failed: %+v", matches)
	}

	if err := store.Remember("Gotchas", "CGO", "only this", true, "", nil); err != nil {
		t.Fatal(err)
	}
	matches, _ = store.Recall("", "Gotchas")
	if len(matches) != 1 || matches[0].Content != "only this" {
		t.Fatalf("replace failed: %+v", matches)
	}
}

func TestBrainForget(t *testing.T) {
	store := newTestBrain(t)
	_ = store.Remember("Topic", "A", "a", false, "", nil)
	_ = store.Remember("Topic", "B", "b", false, "", nil)

	if err := store.Forget("Topic", "A"); err != nil {
		t.Fatal(err)
	}
	matches, _ := store.Recall("", "Topic")
	if len(matches) != 1 || matches[0].Subtopic != "B" {
		t.Fatalf("forget subtopic failed: %+v", matches)
	}

	if err := store.Forget("Topic", ""); err != nil {
		t.Fatal(err)
	}
	topics, _ := store.ListTopics()
	if len(topics) != 0 {
		t.Fatalf("forget topic failed, topics=%+v", topics)
	}
}

func TestBrainIndexOnlyTitles(t *testing.T) {
	store := newTestBrain(t)
	_ = store.Remember("Architecture", "Layout", "secret-body-should-not-appear", false, "", nil)

	index, err := store.Index()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(index, "Architecture") || !strings.Contains(index, "Layout") {
		t.Fatalf("index missing titles: %q", index)
	}
	if strings.Contains(index, "secret-body-should-not-appear") {
		t.Fatalf("index leaked body content: %q", index)
	}

	// index.md is written to disk and excluded from topic listing.
	if _, err := os.Stat(filepath.Join(store.Dir(), "index.md")); err != nil {
		t.Fatalf("index.md not written: %v", err)
	}
	topics, _ := store.ListTopics()
	if len(topics) != 1 {
		t.Fatalf("index.md leaked into topics: %+v", topics)
	}
}

func TestBrainPersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	first, err := NewBrainStore(dir, "proj")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Remember("Conventions", "Errors", "wrap with %w", false, "", nil); err != nil {
		t.Fatal(err)
	}

	second, err := NewBrainStore(dir, "proj")
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := second.Recall("errors", "")
	if len(matches) != 1 || matches[0].Content != "wrap with %w" {
		t.Fatalf("not persisted across instances: %+v", matches)
	}
}

func TestBrainToolRecallEmpty(t *testing.T) {
	store := newTestBrain(t)
	tool := NewBrainTool(store)

	out, err := tool.Recall(BrainRecallParams{Query: "anything"})
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := out.(string); !ok || !strings.Contains(s, "No matching knowledge") {
		t.Fatalf("empty recall = %v", out)
	}

	// Remember through the tool, then recall returns formatted markdown.
	res, err := tool.Remember(BrainRememberParams{Topic: "T", Subtopic: "S", Content: "hello world"})
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := res.(BrainOpResult); !ok || !r.Success {
		t.Fatalf("remember result = %v", res)
	}
	out, _ = tool.Recall(BrainRecallParams{Query: "hello"})
	if s, ok := out.(string); !ok || !strings.Contains(s, "hello world") || !strings.Contains(s, "## T › S") {
		t.Fatalf("recall after remember = %v", out)
	}
}

func TestBrainWiringInDeepAgent(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GINGA_API_KEY", "")

	deep, err := NewDeepAgentWithConfig(DeepAgentConfig{
		ModelID:      "ollama:llama3.1:8b",
		DisableShell: true,
		Workspace:    t.TempDir(),
		Brain:        true,
		BrainDir:     t.TempDir(),
		BrainProject: "wiring",
	})
	if err != nil {
		t.Fatalf("construct with brain: %v", err)
	}
	if deep.brainStore == nil {
		t.Fatal("brainStore not set")
	}
	if deep.curator == nil {
		t.Fatal("curator agent not built")
	}
	if !strings.Contains(deep.knowledge, "PROJECT BRAIN") {
		t.Fatalf("brain index not surfaced into knowledge: %q", deep.knowledge)
	}
	hasBrainTool := false
	for _, tool := range deep.toolset {
		if tool.GetName() == "Brain" {
			hasBrainTool = true
		}
	}
	if !hasBrainTool {
		t.Fatalf("Brain tool not added to toolset")
	}
	if wf := deep.buildDeepWorkflow(); wf == nil {
		t.Fatal("deep workflow build returned nil with brain enabled")
	}
}

func TestBrainDisabledByDefault(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GINGA_API_KEY", "")

	deep, err := NewDeepAgentWithConfig(DeepAgentConfig{
		ModelID:      "ollama:llama3.1:8b",
		DisableShell: true,
		Workspace:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("construct without brain: %v", err)
	}
	if deep.brainStore != nil {
		t.Fatal("brainStore should be nil when Brain is false")
	}
	for _, tool := range deep.toolset {
		if tool.GetName() == "Brain" {
			t.Fatal("Brain tool present without Brain enabled")
		}
	}
}

func TestBrainFrontmatterRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewBrainStore(dir, "proj")
	if err != nil {
		t.Fatal(err)
	}
	meta := map[string]any{
		"method": "func envBool(name string, def bool) bool",
		"tags":   []any{"env", "config"},
		"file":   "agent/config.go",
	}
	if err := store.Remember("Config", "Env bool parser",
		"Returns the default when the var is unset.", false,
		"Reads a boolean flag from the environment with a default", meta); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	// The on-disk file must carry a YAML frontmatter block.
	raw, err := os.ReadFile(filepath.Join(store.Dir(), "config.md"))
	if err != nil {
		t.Fatalf("read topic file: %v", err)
	}
	for _, want := range []string{"---", "description: Reads a boolean flag", "metadata:", "agent/config.go"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("topic file missing %q:\n%s", want, raw)
		}
	}

	// A fresh store parses the frontmatter back into structured fields.
	reloaded, err := NewBrainStore(dir, "proj")
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := reloaded.Recall("", "Config")
	if len(matches) != 1 {
		t.Fatalf("recall = %+v, want one match", matches)
	}
	m := matches[0]
	if m.Description != "Reads a boolean flag from the environment with a default" {
		t.Fatalf("description = %q", m.Description)
	}
	if m.Metadata["file"] != "agent/config.go" {
		t.Fatalf("metadata not parsed: %+v", m.Metadata)
	}
	if !strings.Contains(m.Content, "Returns the default") {
		t.Fatalf("content = %q", m.Content)
	}
}

func TestBrainAppendMergesMetadata(t *testing.T) {
	store := newTestBrain(t)
	_ = store.Remember("Config", "Parser", "body one", false, "first desc",
		map[string]any{"file": "a.go", "tags": []any{"env"}})
	// Append: content appended, description updated when provided, metadata merged.
	_ = store.Remember("Config", "Parser", "body two", false, "second desc",
		map[string]any{"method": "func Parse()"})

	matches, _ := store.Recall("", "Config")
	if len(matches) != 1 {
		t.Fatalf("matches = %+v", matches)
	}
	m := matches[0]
	if !strings.Contains(m.Content, "body one") || !strings.Contains(m.Content, "body two") {
		t.Fatalf("content not appended: %q", m.Content)
	}
	if m.Description != "second desc" {
		t.Fatalf("description not updated: %q", m.Description)
	}
	if m.Metadata["file"] != "a.go" || m.Metadata["method"] != "func Parse()" {
		t.Fatalf("metadata not merged: %+v", m.Metadata)
	}
}

func TestBrainReplaceOverwritesMetadata(t *testing.T) {
	store := newTestBrain(t)
	_ = store.Remember("Config", "Parser", "old body", false, "old desc",
		map[string]any{"file": "a.go"})
	_ = store.Remember("Config", "Parser", "new body", true, "new desc",
		map[string]any{"method": "func Parse()"})

	matches, _ := store.Recall("", "Config")
	m := matches[0]
	if m.Content != "new body" || m.Description != "new desc" {
		t.Fatalf("replace did not overwrite: %+v", m)
	}
	if _, stale := m.Metadata["file"]; stale {
		t.Fatalf("replace kept stale metadata: %+v", m.Metadata)
	}
	if m.Metadata["method"] != "func Parse()" {
		t.Fatalf("replace lost new metadata: %+v", m.Metadata)
	}
}

func TestBrainParsesLegacyFilesWithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()
	store, err := NewBrainStore(dir, "proj")
	if err != nil {
		t.Fatal(err)
	}
	// Hand-write an old-style topic file with no frontmatter.
	legacy := "# Architecture\n\n## Layout\n\nDomain rules live under internal/domain.\n"
	if err := os.WriteFile(filepath.Join(store.Dir(), "architecture.md"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	matches, _ := store.Recall("", "Architecture")
	if len(matches) != 1 {
		t.Fatalf("matches = %+v", matches)
	}
	m := matches[0]
	if m.Description != "" || m.Metadata != nil {
		t.Fatalf("legacy entry got non-empty hints: desc=%q meta=%+v", m.Description, m.Metadata)
	}
	if m.Content != "Domain rules live under internal/domain." {
		t.Fatalf("legacy content mangled: %q", m.Content)
	}
}

func TestBrainSearchByMetadata(t *testing.T) {
	store := newTestBrain(t)
	_ = store.Remember("Config", "Env bool parser", "plain body text", false,
		"reads a flag", map[string]any{
			"method": "func envBool(name string, def bool) bool",
			"tags":   []any{"env", "config"},
		})

	// Query term appears only in the method signature.
	matches, _ := store.Recall("envBool", "")
	if len(matches) != 1 || matches[0].Subtopic != "Env bool parser" {
		t.Fatalf("search by method signature = %+v", matches)
	}
	// Query term appears only in a tag.
	matches, _ = store.Recall("config", "")
	if len(matches) != 1 {
		t.Fatalf("search by tag = %+v", matches)
	}
}

func TestBrainIndexShowsHintsNotBody(t *testing.T) {
	store := newTestBrain(t)
	_ = store.Remember("Config", "Env bool parser", "secret-body-should-not-appear", false,
		"reads a boolean flag", map[string]any{"file": "agent/config.go"})

	index, err := store.Index()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(index, "reads a boolean flag") {
		t.Fatalf("index missing description: %q", index)
	}
	if !strings.Contains(index, "agent/config.go") {
		t.Fatalf("index missing metadata: %q", index)
	}
	if strings.Contains(index, "secret-body-should-not-appear") {
		t.Fatalf("index leaked body: %q", index)
	}
}

func TestBrainToolSchemaRegistered(t *testing.T) {
	store := newTestBrain(t)
	tool := NewBrainTool(store)
	methods := tool.GetMethods()
	for _, want := range []string{"Brain_Remember", "Brain_Recall", "Brain_ListTopics", "Brain_Forget"} {
		if _, ok := methods[want]; !ok {
			t.Fatalf("method %s not registered; have %v", want, methods)
		}
	}
	// Execute round-trips JSON like the agent runtime does.
	in, _ := json.Marshal(BrainRememberParams{Topic: "Exec", Subtopic: "Path", Content: "via execute"})
	if _, err := tool.Execute("Brain_Remember", in); err != nil {
		t.Fatalf("Execute Remember: %v", err)
	}
	matches, _ := store.Recall("execute", "")
	if len(matches) != 1 {
		t.Fatalf("execute did not persist: %+v", matches)
	}
}
