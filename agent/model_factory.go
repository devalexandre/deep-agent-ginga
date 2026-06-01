package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/devalexandre/agno-golang/agno/models"
	"github.com/devalexandre/agno-golang/agno/models/anthropic"
	bedrock "github.com/devalexandre/agno-golang/agno/models/aws"
	"github.com/devalexandre/agno-golang/agno/models/azure"
	"github.com/devalexandre/agno-golang/agno/models/dashscope"
	"github.com/devalexandre/agno-golang/agno/models/deepseek"
	"github.com/devalexandre/agno-golang/agno/models/google/gemini"
	"github.com/devalexandre/agno-golang/agno/models/groq"
	"github.com/devalexandre/agno-golang/agno/models/ollama"
	"github.com/devalexandre/agno-golang/agno/models/openai/chat"
	openailike "github.com/devalexandre/agno-golang/agno/models/openai/like"
	"github.com/devalexandre/agno-golang/agno/models/openrouter"
	"github.com/devalexandre/agno-golang/agno/models/together"
	"github.com/devalexandre/agno-golang/agno/models/vllm"
)

type modelProvider func(...models.OptionClient) (models.AgnoModelInterface, error)

var supportedModelProviders = map[string]modelProvider{
	"anthropic":         anthropic.New,
	"aws":               bedrock.New,
	"azure":             azure.New,
	"azure-openai":      azure.New,
	"bedrock":           bedrock.New,
	"dashscope":         dashscope.NewDashScopeChat,
	"deepseek":          deepseek.New,
	"gemini":            gemini.NewGemini,
	"google":            gemini.NewGemini,
	"groq":              groq.New,
	"ollama":            ollama.NewOllamaChat,
	"openai":            chat.NewOpenAIChat,
	"openai-chat":       chat.NewOpenAIChat,
	"openai-compatible": openailike.NewLikeOpenAIChat,
	"openai-like":       openailike.NewLikeOpenAIChat,
	"openai-responses":  chat.NewOpenAIChat,
	"openrouter":        openrouter.NewOpenRouterChat,
	"together":          together.NewTogetherChat,
	"vllm":              vllm.NewVLLMProvider,
}

func newModel(config CoderAgentConfig) (models.AgnoModelInterface, string, error) {
	provider, modelID := parseModelSpec(config.ModelID)
	constructor, ok := supportedModelProviders[provider]
	if !ok {
		return nil, "", fmt.Errorf("unsupported model provider %q; supported providers: %s", provider, strings.Join(SupportedModelProviders(), ", "))
	}

	options := []models.OptionClient{models.WithClientMaxTokens(4096)}
	if modelID != "" {
		options = append(options, models.WithID(modelID))
	}
	if apiKey := firstNonEmpty(config.APIKey, os.Getenv("GINGA_API_KEY")); apiKey != "" {
		options = append(options, models.WithAPIKey(apiKey))
	}
	if strings.TrimSpace(config.ModelBaseURL) != "" {
		options = append(options, models.WithBaseURL(strings.TrimSpace(config.ModelBaseURL)))
	}

	model, err := constructor(options...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create %s model %q: %w", provider, modelID, err)
	}

	label := provider
	if id := strings.TrimSpace(model.GetID()); id != "" {
		label = provider + ":" + id
	}
	return model, label, nil
}

func parseModelSpec(value string) (provider, modelID string) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultModelID
	}

	lower := strings.ToLower(value)
	if _, ok := supportedModelProviders[lower]; ok {
		return lower, ""
	}

	before, after, found := strings.Cut(value, ":")
	if !found {
		return "openai", value
	}

	provider = strings.ToLower(strings.TrimSpace(before))
	modelID = strings.TrimSpace(after)
	if provider == "" {
		provider = "openai"
	}
	return provider, modelID
}

func SupportedModelProviders() []string {
	providers := make([]string, 0, len(supportedModelProviders))
	for provider := range supportedModelProviders {
		providers = append(providers, provider)
	}
	sortStrings(providers)
	return providers
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		value := values[i]
		j := i - 1
		for j >= 0 && values[j] > value {
			values[j+1] = values[j]
			j--
		}
		values[j+1] = value
	}
}
