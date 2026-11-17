package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// scriptedAnthropic answers like the Anthropic Messages API with a fixed
// sequence of turns and records the requests it got.
type scriptedAnthropic struct {
	mu       sync.Mutex
	turns    []string
	requests []map[string]interface{}
}

func (s *scriptedAnthropic) serve(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.requests = append(s.requests, body)
		turn := len(s.requests) - 1
		s.mu.Unlock()
		if turn >= len(s.turns) {
			t.Errorf("unexpected request %d", turn+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(s.turns[turn]))
	}))
	t.Cleanup(server.Close)
	return server
}

func toolUseTurn(t *testing.T, id, name string, input map[string]interface{}) string {
	t.Helper()
	raw, _ := json.Marshal(input)
	return `{"id":"msg_` + id + `","type":"message","role":"assistant","model":"m",` +
		`"content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + string(raw) + `}],` +
		`"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`
}

func textTurn(text string) string {
	raw, _ := json.Marshal(text)
	return `{"id":"msg_end","type":"message","role":"assistant","model":"m",` +
		`"content":[{"type":"text","text":` + string(raw) + `}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
}

func TestDeepAgentStrategyRunsOneLoop(t *testing.T) {
	clearRepoAIEnv(t)
	t.Setenv("REPOAI_API_KEY", "repo_sk_test")
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "calc.go"), []byte("package calc\n\nfunc Divide(a, b int) int { return a / b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	script := &scriptedAnthropic{turns: []string{
		toolUseTurn(t, "t1", "Plan_Update", map[string]interface{}{"steps": []string{"doing: guard against zero", "todo: report"}}),
		toolUseTurn(t, "t2", "Code_EditFile", map[string]interface{}{
			"path":       "calc.go",
			"old_string": "func Divide(a, b int) int { return a / b }",
			"new_string": "func Divide(a, b int) int {\n\tif b == 0 {\n\t\treturn 0\n\t}\n\treturn a / b\n}",
		}),
		textTurn("Divide agora retorna 0 quando b é zero. Arquivo: calc.go."),
	}}
	server := script.serve(t)

	deep, err := NewDeepAgentWithConfig(DeepAgentConfig{
		ModelID:      "repoai:claude-haiku-4-5",
		ModelBaseURL: server.URL,
		Workspace:    workspace,
		DisableShell: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	var steps []string
	var activities []AgentActivity
	var mu sync.Mutex
	answer, err := deep.DeepWorkWithActivity(context.Background(), "Evite a divisão por zero em Divide.",
		func(step string) { steps = append(steps, step) },
		func(a AgentActivity) { mu.Lock(); activities = append(activities, a); mu.Unlock() })
	if err != nil {
		t.Fatalf("deep: %v", err)
	}

	if !strings.Contains(answer, "Divide agora retorna 0") {
		t.Fatalf("answer = %q", answer)
	}
	data, _ := os.ReadFile(filepath.Join(workspace, "calc.go"))
	if !strings.Contains(string(data), "if b == 0") {
		t.Fatalf("calc.go was not edited:\n%s", data)
	}
	if strings.Join(steps, ",") != "agent" {
		t.Fatalf("progress steps = %v, want one agent run", steps)
	}
	if len(script.requests) != 3 {
		t.Fatalf("model calls = %d, want 3", len(script.requests))
	}

	// Every request offers the plan and the code tools, and no git tools.
	var toolNames []string
	for _, tool := range script.requests[0]["tools"].([]interface{}) {
		toolNames = append(toolNames, tool.(map[string]interface{})["name"].(string))
	}
	joined := strings.Join(toolNames, " ")
	if !strings.Contains(joined, "Plan_Update") || !strings.Contains(joined, "Code_EditFile") || strings.Contains(joined, "Push") {
		t.Fatalf("tools offered: %s", joined)
	}
	if script.requests[0]["max_tokens"].(float64) != anthropicNonStreamingCap {
		t.Fatalf("max_tokens = %v", script.requests[0]["max_tokens"])
	}

	mu.Lock()
	defer mu.Unlock()
	var plan string
	for _, a := range activities {
		if a.Type == "plan" {
			plan = a.Result
		}
	}
	if plan != "[>] guard against zero\n[ ] report\n" {
		t.Fatalf("plan activity = %q (activities: %+v)", plan, activities)
	}
	var edited string
	for _, a := range activities {
		if a.Type == "tool_done" && strings.Contains(a.ToolName, "EditFile") {
			edited = a.Path
		}
	}
	if edited != filepath.Join(workspace, "calc.go") {
		t.Fatalf("edit activity path = %q, want the absolute path", edited)
	}
}

func TestPhasedStrategyStillAvailable(t *testing.T) {
	if deepStrategy("phased") != DeepStrategyPhased || deepStrategy(" PHASED ") != DeepStrategyPhased {
		t.Fatal("phased strategy not recognized")
	}
	if deepStrategy("") != DeepStrategyAgent || deepStrategy("whatever") != DeepStrategyAgent {
		t.Fatal("the agent strategy is not the default")
	}
}
