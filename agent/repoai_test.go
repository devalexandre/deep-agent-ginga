package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type capturedAnthropicRequest struct {
	Path   string
	APIKey string
	Model  string
}

// newAnthropicCapturingServer answers like the Anthropic Messages API and
// records what the client sent.
func newAnthropicCapturingServer(t *testing.T) (*httptest.Server, *capturedAnthropicRequest) {
	t.Helper()
	captured := &capturedAnthropicRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Path = r.URL.Path
		captured.APIKey = r.Header.Get("X-Api-Key")
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured.Model = body.Model

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "test",
			"content": [{"type": "text", "text": "ok"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	t.Cleanup(server.Close)
	return server, captured
}

func clearRepoAIEnv(t *testing.T) {
	t.Helper()
	clearProviderEnv(t)
	for _, key := range []string{"REPOAI_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL"} {
		t.Setenv(key, "")
	}
}

func TestRepoAIProviderIsSupported(t *testing.T) {
	if _, ok := supportedModelProviders["repoai"]; !ok {
		t.Fatal("repoai provider not registered")
	}
}

func TestRepoAIUsesEnvKeyAndDefaultModel(t *testing.T) {
	clearRepoAIEnv(t)
	t.Setenv("REPOAI_API_KEY", "repo_sk_test")
	server, captured := newAnthropicCapturingServer(t)

	model, label, err := newModel(DeepAgentConfig{ModelID: "repoai", ModelBaseURL: server.URL})
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	if label != "repoai:"+defaultRepoAIModel {
		t.Fatalf("label = %q, want %q", label, "repoai:"+defaultRepoAIModel)
	}
	invoke(t, model)

	if captured.Path != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", captured.Path)
	}
	if captured.APIKey != "repo_sk_test" {
		t.Fatalf("x-api-key = %q, want repo_sk_test", captured.APIKey)
	}
	if captured.Model != defaultRepoAIModel {
		t.Fatalf("model = %q, want %q", captured.Model, defaultRepoAIModel)
	}
}

func TestRepoAIExplicitModelAndKey(t *testing.T) {
	clearRepoAIEnv(t)
	t.Setenv("REPOAI_API_KEY", "repo_sk_env")
	server, captured := newAnthropicCapturingServer(t)

	model, _, err := newModel(DeepAgentConfig{ModelID: "repoai:claude-opus-5-5", ModelBaseURL: server.URL, APIKey: "repo_sk_flag"})
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	invoke(t, model)

	if captured.APIKey != "repo_sk_flag" {
		t.Fatalf("x-api-key = %q, want the explicit key", captured.APIKey)
	}
	if captured.Model != "claude-opus-5-5" {
		t.Fatalf("model = %q, want claude-opus-5-5", captured.Model)
	}
}

func TestRepoAINeverSendsAnthropicKey(t *testing.T) {
	clearRepoAIEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-should-not-be-used")

	_, _, err := newModel(DeepAgentConfig{ModelID: "repoai:claude-sonnet-5-5"})
	if err == nil || !strings.Contains(err.Error(), "REPOAI_API_KEY") {
		t.Fatalf("expected a missing-key error, got %v", err)
	}
}

// Anthropic rejects a request whose tool names repeat ("tools: Tool names
// must be unique"), so the shared toolset must not register a method twice.
func TestToolsetMethodNamesAreUnique(t *testing.T) {
	clearRepoAIEnv(t)
	deep, err := NewDeepAgentWithConfig(DeepAgentConfig{
		ModelID:        "ollama:llama3.1:8b",
		SkipModelCheck: true,
		Workspace:      t.TempDir(),
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	seen := map[string]bool{}
	for _, tool := range deep.toolset {
		for method := range tool.GetMethods() {
			if seen[method] {
				t.Errorf("tool method %q registered more than once", method)
			}
			seen[method] = true
		}
	}
}
