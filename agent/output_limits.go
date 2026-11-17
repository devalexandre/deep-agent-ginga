package agent

import "strings"

const (
	// defaultOutputTokens is used for a model this table does not know:
	// enough for a whole source file, and accepted by OpenAI-compatible
	// endpoints and local servers.
	defaultOutputTokens = 8192

	// anthropicNonStreamingCap bounds Claude requests while they are sent
	// without streaming: the Anthropic SDK refuses a non-streaming request
	// whose max_tokens implies more than ten minutes (about 21,333 tokens),
	// and Anthropic recommends about 16,000 for non-streaming calls.
	anthropicNonStreamingCap = 16000
)

// outputTokenLimit is the max_tokens sent with each request: the explicit
// override when set, otherwise what the model can produce, bounded by what
// the provider's transport accepts.
func outputTokenLimit(provider, modelID string, override int) int {
	if override > 0 {
		return override
	}
	limit := modelOutputTokens(modelID)
	if isAnthropicTransport(provider, modelID) && limit > anthropicNonStreamingCap {
		limit = anthropicNonStreamingCap
	}
	return limit
}

// isAnthropicTransport reports whether requests go through the Anthropic
// SDK's non-streaming Messages call.
func isAnthropicTransport(provider, modelID string) bool {
	switch provider {
	case "anthropic", "repoai":
		return true
	case "aws", "bedrock":
		return strings.Contains(strings.ToLower(modelID), "claude")
	}
	return false
}

// modelOutputTokens is the most output a model accepts in one answer. Model
// ids are matched by family, so dated and provider-prefixed ids
// ("anthropic/claude-sonnet-4-5", "anthropic.claude-opus-5-5",
// "claude-3-5-sonnet-20241022") resolve too. Unknown models get
// defaultOutputTokens.
func modelOutputTokens(modelID string) int {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if id == "" {
		return defaultOutputTokens
	}
	// The name without a router prefix such as "openai/".
	name := id[strings.LastIndex(id, "/")+1:]
	for _, family := range outputTokenFamilies {
		for _, marker := range family.markers {
			// "^o3" matches only at the start of the name.
			if prefix, ok := strings.CutPrefix(marker, "^"); ok {
				if strings.HasPrefix(name, prefix) {
					return family.tokens
				}
			} else if strings.Contains(id, marker) {
				return family.tokens
			}
		}
	}
	return defaultOutputTokens
}

// outputTokenFamilies is checked in order: more specific markers first.
var outputTokenFamilies = []struct {
	markers []string
	tokens  int
}{
	// Claude: the 5 family and 4.6 onward write up to 128K.
	{[]string{"claude-fable", "claude-mythos", "claude-opus-5", "claude-sonnet-5", "claude-haiku-5",
		"claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-4-6"}, 128000},
	{[]string{"claude-haiku-4-5", "claude-sonnet-4-5", "claude-opus-4-5", "claude-3-7-sonnet", "claude-sonnet-4"}, 64000},
	{[]string{"claude-opus-4"}, 32000},
	{[]string{"claude-3-5"}, 8192},
	{[]string{"claude-3"}, 4096},

	// OpenAI.
	{[]string{"gpt-5"}, 128000},
	{[]string{"^o1", "^o3", "^o4"}, 100000},
	{[]string{"gpt-4.1"}, 32768},
	{[]string{"gpt-4o"}, 16384},
	{[]string{"gpt-4-turbo", "gpt-4", "gpt-3.5"}, 4096},

	// Google.
	{[]string{"gemini-3", "gemini-2.5"}, 65536},
	{[]string{"gemini-2.0", "gemini-1.5"}, 8192},

	// DeepSeek.
	{[]string{"deepseek-reasoner", "deepseek-r1"}, 64000},
	{[]string{"deepseek-chat", "deepseek-v3"}, 8192},
}
