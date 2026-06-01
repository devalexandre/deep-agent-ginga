package agent

import "testing"

func TestParseModelSpecDefaultsToOpenAI(t *testing.T) {
	provider, modelID := parseModelSpec("gpt-4o")
	if provider != "openai" || modelID != "gpt-4o" {
		t.Fatalf("unexpected spec: provider=%q model=%q", provider, modelID)
	}
}

func TestParseModelSpecSupportsProviderPrefix(t *testing.T) {
	tests := []struct {
		value    string
		provider string
		modelID  string
	}{
		{"openai-responses:gpt-5.2", "openai-responses", "gpt-5.2"},
		{"openrouter:anthropic/claude-3.5-sonnet", "openrouter", "anthropic/claude-3.5-sonnet"},
		{"ollama:llama3.1:8b", "ollama", "llama3.1:8b"},
		{"gemini:gemini-2.0-flash", "gemini", "gemini-2.0-flash"},
	}

	for _, tt := range tests {
		provider, modelID := parseModelSpec(tt.value)
		if provider != tt.provider || modelID != tt.modelID {
			t.Fatalf("%q: provider=%q model=%q", tt.value, provider, modelID)
		}
	}
}

func TestParseModelSpecSupportsProviderOnly(t *testing.T) {
	provider, modelID := parseModelSpec("ollama")
	if provider != "ollama" || modelID != "" {
		t.Fatalf("unexpected spec: provider=%q model=%q", provider, modelID)
	}
}

func TestSupportedModelProvidersIncludesCommonProviders(t *testing.T) {
	providers := map[string]bool{}
	for _, provider := range SupportedModelProviders() {
		providers[provider] = true
	}
	for _, want := range []string{"openai", "openrouter", "ollama", "gemini", "anthropic", "groq", "deepseek", "together", "dashscope", "vllm", "azure", "bedrock"} {
		if !providers[want] {
			t.Fatalf("expected provider %q in supported providers: %#v", want, SupportedModelProviders())
		}
	}
}
