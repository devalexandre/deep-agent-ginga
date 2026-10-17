package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devalexandre/agno-golang/agno/models"
)

// capturedRequest records what the Go HTTP client actually sent, so tests
// can assert on it instead of trusting that option-building code did the
// right thing. This is how the API-key-leak and model-id-defaulting bugs
// were found in the first place: inspecting the real outgoing request is
// the only way to see env-var fallbacks that live several layers down in
// agno-golang / openai-go.
type capturedRequest struct {
	Authorization string
	Model         string
}

func newCapturingServer(t *testing.T) (*httptest.Server, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Authorization = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured.Model = body.Model

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "test",
			"object": "chat.completion",
			"created": 0,
			"model": "test",
			"choices": [{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]
		}`))
	}))
	t.Cleanup(server.Close)
	return server, captured
}

// clearProviderEnv isolates tests from whatever the developer's shell
// happens to have set (e.g. a real OPENAI_API_KEY for unrelated tools),
// which is exactly the condition that caused the key-leak bug to go
// unnoticed during manual testing.
func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"OPENAI_API_KEY", "GINGA_API_KEY", "FREELLMAPI_API_KEY", "LLAMAFILE_API_KEY"} {
		t.Setenv(key, "")
	}
}

func invoke(t *testing.T, model models.AgnoModelInterface) {
	t.Helper()
	if _, err := model.Invoke(context.Background(), []models.Message{{Role: models.TypeUserRole, Content: "hi"}}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
}

func TestNewLlamafileChatDoesNotLeakUnrelatedAPIKey(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-should-not-be-used")
	server, captured := newCapturingServer(t)

	model, err := newLlamafileChat(models.WithBaseURL(server.URL))
	if err != nil {
		t.Fatalf("newLlamafileChat: %v", err)
	}
	invoke(t, model)

	if captured.Authorization == "Bearer sk-should-not-be-used" {
		t.Fatalf("leaked unrelated OPENAI_API_KEY to llamafile request")
	}
	if captured.Authorization != "Bearer "+noAPIKeyPlaceholder {
		t.Fatalf("Authorization = %q, want %q", captured.Authorization, "Bearer "+noAPIKeyPlaceholder)
	}
}

func TestNewLlamafileChatDefaultsModelIDWhenNotSpecified(t *testing.T) {
	clearProviderEnv(t)
	server, captured := newCapturingServer(t)

	// models.WithID("") simulates what newModel() forwards for providers in
	// providersWithOptionalModelID when the caller gave no model id at all.
	model, err := newLlamafileChat(models.WithBaseURL(server.URL), models.WithID(""))
	if err != nil {
		t.Fatalf("newLlamafileChat: %v", err)
	}
	if got := model.GetID(); got != "llamafile" {
		t.Fatalf("GetID() = %q, want %q (regression: models.DefaultOptions()'s gpt-4o leaking through)", got, "llamafile")
	}

	invoke(t, model)
	if captured.Model != "llamafile" {
		t.Fatalf("request model = %q, want %q", captured.Model, "llamafile")
	}
}

func TestNewLlamafileChatRespectsExplicitAPIKeyAndModel(t *testing.T) {
	clearProviderEnv(t)
	server, captured := newCapturingServer(t)

	model, err := newLlamafileChat(
		models.WithBaseURL(server.URL),
		models.WithID("my-local-model"),
		models.WithAPIKey("secret-key"),
	)
	if err != nil {
		t.Fatalf("newLlamafileChat: %v", err)
	}
	if got := model.GetID(); got != "my-local-model" {
		t.Fatalf("GetID() = %q, want %q", got, "my-local-model")
	}

	invoke(t, model)
	if captured.Authorization != "Bearer secret-key" {
		t.Fatalf("Authorization = %q, want %q", captured.Authorization, "Bearer secret-key")
	}
	if captured.Model != "my-local-model" {
		t.Fatalf("request model = %q, want %q", captured.Model, "my-local-model")
	}
}

func TestNewLlamafileChatUsesEnvAPIKeyFallback(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("LLAMAFILE_API_KEY", "env-key")
	server, captured := newCapturingServer(t)

	model, err := newLlamafileChat(models.WithBaseURL(server.URL))
	if err != nil {
		t.Fatalf("newLlamafileChat: %v", err)
	}
	invoke(t, model)
	if captured.Authorization != "Bearer env-key" {
		t.Fatalf("Authorization = %q, want %q", captured.Authorization, "Bearer env-key")
	}
}

func TestNewFreeLLMAPIChatDoesNotLeakUnrelatedAPIKey(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-should-not-be-used")
	server, captured := newCapturingServer(t)

	model, err := newFreeLLMAPIChat(models.WithBaseURL(server.URL), models.WithID("kilo-auto"))
	if err != nil {
		t.Fatalf("newFreeLLMAPIChat: %v", err)
	}
	invoke(t, model)

	if captured.Authorization == "Bearer sk-should-not-be-used" {
		t.Fatalf("leaked unrelated OPENAI_API_KEY to freellmapi request")
	}
	if captured.Authorization != "Bearer "+noAPIKeyPlaceholder {
		t.Fatalf("Authorization = %q, want %q", captured.Authorization, "Bearer "+noAPIKeyPlaceholder)
	}
}

func TestNewFreeLLMAPIChatUsesEnvAPIKeyFallback(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("FREELLMAPI_API_KEY", "env-key")
	server, captured := newCapturingServer(t)

	model, err := newFreeLLMAPIChat(models.WithBaseURL(server.URL), models.WithID("kilo-auto"))
	if err != nil {
		t.Fatalf("newFreeLLMAPIChat: %v", err)
	}
	invoke(t, model)
	if captured.Authorization != "Bearer env-key" {
		t.Fatalf("Authorization = %q, want %q", captured.Authorization, "Bearer env-key")
	}
}

func TestNewFreeLLMAPIChatDefaultsBaseURLWhenNotConfigured(t *testing.T) {
	clearProviderEnv(t)

	model, err := newFreeLLMAPIChat(models.WithID("kilo-auto"))
	if err != nil {
		t.Fatalf("newFreeLLMAPIChat: %v", err)
	}
	if model == nil {
		t.Fatal("expected a non-nil model")
	}
	// We can't dial the real default host in a unit test, but construction
	// must succeed without requiring an explicit base URL.
}

func TestNewModelFallsBackToOpenAICompatibleForUnknownProvider(t *testing.T) {
	clearProviderEnv(t)
	server, captured := newCapturingServer(t)

	model, label, err := newModel(CoderAgentConfig{
		ModelID:      "xkiro:some-model",
		ModelBaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	if label != "xkiro:some-model" {
		t.Fatalf("label = %q, want %q", label, "xkiro:some-model")
	}

	invoke(t, model)
	if captured.Model != "some-model" {
		t.Fatalf("request model = %q, want %q", captured.Model, "some-model")
	}
}

func TestNewModelUnknownProviderWithoutBaseURLErrors(t *testing.T) {
	_, _, err := newModel(CoderAgentConfig{ModelID: "xkiro:some-model"})
	if err == nil {
		t.Fatal("expected an error for an unrecognized provider with no base URL configured")
	}
}

func TestNewModelLlamafileWithoutModelIDDefaultsCorrectly(t *testing.T) {
	clearProviderEnv(t)
	server, captured := newCapturingServer(t)

	model, label, err := newModel(CoderAgentConfig{
		ModelID:      "llamafile",
		ModelBaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	if label != "llamafile:llamafile" {
		t.Fatalf("label = %q, want %q", label, "llamafile:llamafile")
	}

	invoke(t, model)
	if captured.Model != "llamafile" {
		t.Fatalf("request model = %q, want %q", captured.Model, "llamafile")
	}
	if captured.Authorization != "Bearer "+noAPIKeyPlaceholder {
		t.Fatalf("Authorization = %q, want %q", captured.Authorization, "Bearer "+noAPIKeyPlaceholder)
	}
}

func TestNewModelOtherProvidersStillRequireExplicitModelID(t *testing.T) {
	// Providers not in providersWithOptionalModelID must keep their existing
	// behavior: an empty model id is simply not forwarded, leaving whatever
	// default the underlying constructor picks (unchanged by this change).
	if providersWithOptionalModelID["openai-compatible"] {
		t.Fatal("openai-compatible should not be in providersWithOptionalModelID")
	}
}
