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

	if err := store.Remember("Build & Test", "Commands", "Run `go test ./...` to test.", false); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if err := store.Remember("Architecture", "Layout", "Domain rules live under internal/domain.", false); err != nil {
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
	if err := store.Remember("Gotchas", "CGO", "first note", false); err != nil {
		t.Fatal(err)
	}
	if err := store.Remember("Gotchas", "CGO", "second note", false); err != nil {
		t.Fatal(err)
	}
	matches, _ := store.Recall("", "Gotchas")
	if len(matches) != 1 || !strings.Contains(matches[0].Content, "first note") || !strings.Contains(matches[0].Content, "second note") {
		t.Fatalf("append failed: %+v", matches)
	}

	if err := store.Remember("Gotchas", "CGO", "only this", true); err != nil {
		t.Fatal(err)
	}
	matches, _ = store.Recall("", "Gotchas")
	if len(matches) != 1 || matches[0].Content != "only this" {
		t.Fatalf("replace failed: %+v", matches)
	}
}

func TestBrainForget(t *testing.T) {
	store := newTestBrain(t)
	_ = store.Remember("Topic", "A", "a", false)
	_ = store.Remember("Topic", "B", "b", false)

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
	_ = store.Remember("Architecture", "Layout", "secret-body-should-not-appear", false)

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
	if err := first.Remember("Conventions", "Errors", "wrap with %w", false); err != nil {
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
