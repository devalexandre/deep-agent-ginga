package agent

import (
	"context"
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

// defaultFreeLLMAPIBaseURL is the conventional local endpoint for FreeLLMAPI
// (https://github.com/tashfeenahmed/freellmapi). Used when no --model-base-url
// is provided.
const defaultFreeLLMAPIBaseURL = "http://localhost:3001/v1"

// defaultLlamafileBaseURL is llamafile's default local server address
// (https://github.com/mozilla-ai/llamafile), which exposes an
// OpenAI-compatible API under /v1. Used when no --model-base-url is provided.
const defaultLlamafileBaseURL = "http://127.0.0.1:8080/v1"

// defaultRepoAIBaseURL is the REPO AI gateway (https://repostore.shop/claude),
// which speaks the Anthropic Messages API. The SDK appends /v1/messages.
const defaultRepoAIBaseURL = "https://api.repostore.shop"

// defaultRepoAIModel is used when no model id is given; the Anthropic client's
// own default is not served by the gateway.
const defaultRepoAIModel = "claude-sonnet-5-5"

var supportedModelProviders = map[string]modelProvider{
	"anthropic":         anthropic.New,
	"aws":               bedrock.New,
	"azure":             azure.New,
	"azure-openai":      azure.New,
	"bedrock":           bedrock.New,
	"dashscope":         dashscope.NewDashScopeChat,
	"deepseek":          deepseek.New,
	"freellmapi":        newFreeLLMAPIChat,
	"gemini":            gemini.NewGemini,
	"google":            gemini.NewGemini,
	"groq":              groq.New,
	"llamafile":         newLlamafileChat,
	"ollama":            ollama.NewOllamaChat,
	"openai":            chat.NewOpenAIChat,
	"openai-chat":       chat.NewOpenAIChat,
	"openai-compatible": openailike.NewLikeOpenAIChat,
	"openai-like":       openailike.NewLikeOpenAIChat,
	"openai-responses":  chat.NewOpenAIChat,
	"openrouter":        openrouter.NewOpenRouterChat,
	"repoai":            newRepoAIChat,
	"together":          together.NewTogetherChat,
	"vllm":              vllm.NewVLLMProvider,
}

// noAPIKeyPlaceholder is sent as the API key when neither the caller nor a
// provider-specific env var configured one. It exists to stop client.NewClient
// (and the underlying openai-go SDK, which independently falls back to
// OPENAI_API_KEY from the environment) from silently reusing an unrelated
// key — e.g. a real OPENAI_API_KEY set for other tools — against a local or
// third-party endpoint the user never pointed it at. Local-only servers like
// llamafile don't check it; hosted free routers like FreeLLMAPI will reject
// it with a clear auth error instead of an unexplained leak.
const noAPIKeyPlaceholder = "not-required"

// newFreeLLMAPIChat wires up FreeLLMAPI (a local OpenAI-compatible router,
// see https://github.com/tashfeenahmed/freellmapi), defaulting the base URL
// and API key when the caller didn't supply one explicitly.
func newFreeLLMAPIChat(options ...models.OptionClient) (models.AgnoModelInterface, error) {
	opts := models.DefaultOptions()
	for _, option := range options {
		option(opts)
	}

	finalOptions := options
	if opts.BaseURL == "" {
		finalOptions = append([]models.OptionClient{models.WithBaseURL(defaultFreeLLMAPIBaseURL)}, finalOptions...)
	}
	if opts.APIKey == "" {
		apiKey := os.Getenv("FREELLMAPI_API_KEY")
		if apiKey == "" {
			apiKey = noAPIKeyPlaceholder
		}
		finalOptions = append(finalOptions, models.WithAPIKey(apiKey))
	}

	return openailike.NewLikeOpenAIChat(finalOptions...)
}

// newLlamafileChat wires up llamafile (a single-file local LLM server built
// on llama.cpp, see https://github.com/mozilla-ai/llamafile). It serves one
// model at a time and exposes an OpenAI-compatible API with no auth by
// default, so this defaults the base URL and API key when the caller didn't
// supply one explicitly.
func newLlamafileChat(options ...models.OptionClient) (models.AgnoModelInterface, error) {
	opts := models.DefaultOptions()
	for _, option := range options {
		option(opts)
	}

	finalOptions := options
	if opts.BaseURL == "" {
		finalOptions = append([]models.OptionClient{models.WithBaseURL(defaultLlamafileBaseURL)}, finalOptions...)
	}
	if opts.APIKey == "" {
		apiKey := os.Getenv("LLAMAFILE_API_KEY")
		if apiKey == "" {
			apiKey = noAPIKeyPlaceholder
		}
		finalOptions = append(finalOptions, models.WithAPIKey(apiKey))
	}
	if opts.ID == "" {
		finalOptions = append(finalOptions, models.WithID("llamafile"))
	}

	return openailike.NewLikeOpenAIChat(finalOptions...)
}

// newRepoAIChat wires up the REPO AI gateway, an Anthropic-compatible API.
// The key comes from the caller (--api-key / GINGA_API_KEY) or REPOAI_API_KEY.
// It never falls back to ANTHROPIC_API_KEY: that key belongs to Anthropic and
// must not be sent to a third-party gateway.
func newRepoAIChat(options ...models.OptionClient) (models.AgnoModelInterface, error) {
	opts := models.DefaultOptions()
	for _, option := range options {
		option(opts)
	}

	finalOptions := options
	if opts.BaseURL == "" {
		finalOptions = append([]models.OptionClient{models.WithBaseURL(defaultRepoAIBaseURL)}, finalOptions...)
	}
	if opts.APIKey == "" {
		apiKey := strings.TrimSpace(os.Getenv("REPOAI_API_KEY"))
		if apiKey == "" {
			return nil, fmt.Errorf("REPO AI needs an API key: set REPOAI_API_KEY or GINGA_API_KEY, or pass --api-key")
		}
		finalOptions = append(finalOptions, models.WithAPIKey(apiKey))
	}
	if opts.ID == "" {
		finalOptions = append(finalOptions, models.WithID(defaultRepoAIModel))
	}

	return anthropic.New(finalOptions...)
}

// providersWithOptionalModelID tolerate (and default sensibly for) a model
// id that was never specified — llamafile only ever serves the one model it
// was started with, so the id is a formality. newModel() normally skips
// models.WithID() entirely when modelID is empty, which leaves the model's
// ID at models.DefaultOptions()'s baked-in "gpt-4o" rather than truly empty,
// so these providers need the empty id forwarded explicitly to tell them
// apart from "the user asked for gpt-4o".
var providersWithOptionalModelID = map[string]bool{
	"llamafile": true,
	"repoai":    true,
}

func newModel(config DeepAgentConfig) (models.AgnoModelInterface, string, error) {
	provider, modelID := parseModelSpec(config.ModelID)
	constructor, ok := supportedModelProviders[provider]
	if !ok {
		// Unknown provider names are common for custom/self-hosted endpoints
		// (e.g. an aggregator like "xkiro:some-model"). As long as a base URL
		// is configured, treat them as a generic OpenAI-compatible endpoint
		// instead of failing outright.
		if strings.TrimSpace(config.ModelBaseURL) == "" {
			return nil, "", fmt.Errorf("unsupported model provider %q; supported providers: %s (or set a model base URL to use any OpenAI-compatible endpoint)", provider, strings.Join(SupportedModelProviders(), ", "))
		}
		constructor = openailike.NewLikeOpenAIChat
	}

	options := []models.OptionClient{models.WithClientMaxTokens(outputTokenLimit(provider, modelID, config.MaxOutputTokens))}
	if modelID != "" || providersWithOptionalModelID[provider] {
		options = append(options, models.WithID(modelID))
	}
	if apiKey := firstNonEmpty(config.APIKey, os.Getenv("GINGA_API_KEY")); apiKey != "" {
		options = append(options, models.WithAPIKey(apiKey))
	}
	if strings.TrimSpace(config.ModelBaseURL) != "" {
		options = append(options, models.WithBaseURL(strings.TrimSpace(config.ModelBaseURL)))
	}

	if provider == "ollama" {
		// Ollama loads a model with a 4,096-token window unless the request
		// says otherwise, and silently cuts whatever does not fit. The agent's
		// own instructions are larger than that.
		params := map[string]interface{}{"num_ctx": config.ContextTokens}
		if config.ContextTokens <= 0 {
			params["num_ctx"] = DefaultOllamaContext
		}
		if !config.SkipModelCheck {
			thinking, err := checkOllamaModel(context.Background(), config.ModelBaseURL, firstNonEmpty(config.APIKey, os.Getenv("GINGA_API_KEY")), modelID)
			if err != nil {
				return nil, "", err
			}
			if thinking {
				// Small thinking models often stop after reasoning, with no
				// answer and no tool call. The agent's phases already make
				// the model plan, so the hidden reasoning is turned off.
				params["think"] = false
			}
		}
		options = append(options, models.WithClientParams(params))
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
