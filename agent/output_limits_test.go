package agent

import "testing"

func TestOutputTokenLimit(t *testing.T) {
	cases := []struct {
		provider, model string
		override, want  int
	}{
		// Claude through the Anthropic SDK: the model's limit, bounded by
		// what a non-streaming request accepts.
		{"anthropic", "claude-opus-5-5", 0, anthropicNonStreamingCap},
		{"repoai", "claude-haiku-4-5", 0, anthropicNonStreamingCap},
		{"anthropic", "claude-3-5-sonnet-20241022", 0, 8192},
		{"anthropic", "claude-3-haiku-20240307", 0, 4096},
		// The same Claude model elsewhere keeps its full limit.
		{"openrouter", "anthropic/claude-sonnet-5-5", 0, 128000},
		{"openrouter", "anthropic/claude-haiku-4-5", 0, 64000},
		{"openai", "gpt-5.2", 0, 128000},
		{"openai", "gpt-4.1-mini", 0, 32768},
		{"openai", "gpt-4o", 0, 16384},
		{"openai", "o4-mini", 0, 100000},
		{"gemini", "gemini-2.5-pro", 0, 65536},
		{"deepseek", "deepseek-chat", 0, 8192},
		{"ollama", "qwen3.5:4b", 0, defaultOutputTokens},
		{"openrouter", "openai/o3", 0, 100000},
		{"ollama", "phi-o3-local", 0, defaultOutputTokens},
		{"openai-compatible", "", 0, defaultOutputTokens},
		// An explicit value always wins.
		{"anthropic", "claude-opus-5-5", 64000, 64000},
		{"ollama", "qwen3.5:4b", 2048, 2048},
	}
	for _, c := range cases {
		if got := outputTokenLimit(c.provider, c.model, c.override); got != c.want {
			t.Errorf("outputTokenLimit(%q, %q, %d) = %d, want %d", c.provider, c.model, c.override, got, c.want)
		}
	}
}
