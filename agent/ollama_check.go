package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	defaultOllamaBaseURL = "http://localhost:11434"

	// DefaultOllamaContext is the context window requested from Ollama. The
	// instructions, the workspace knowledge and the tool schemas take about
	// 6,000 tokens, so this is close to the minimum that works. It is kept
	// low on purpose: a larger window can exhaust the memory of a small GPU.
	// Raise it with CoderAgentConfig.ContextTokens when the hardware allows.
	DefaultOllamaContext = 8192
)

// checkOllamaModel asks the Ollama server about the model before the first
// request, so a missing model or one without tool calling fails at startup
// with the fix, instead of failing later with a provider error after retries.
//
// It also reports whether the model reasons before answering ("thinking").
func checkOllamaModel(ctx context.Context, baseURL, apiKey, modelID string) (thinking bool, err error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	body, _ := json.Marshal(map[string]string{"model": modelID})
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("Ollama não respondeu em %s: %w (o servidor está rodando? confira model_base_url)", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, fmt.Errorf("o modelo %q não existe no Ollama em %s; rode: ollama pull %s", modelID, baseURL, modelID)
	}
	if resp.StatusCode != http.StatusOK {
		// Servidor compatível que não implementa /api/show: não há o que conferir.
		return false, nil
	}
	var info struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return false, nil
	}
	if len(info.Capabilities) > 0 {
		tools := false
		for _, c := range info.Capabilities {
			tools = tools || c == "tools"
			thinking = thinking || c == "thinking"
		}
		if !tools {
			return false, fmt.Errorf("o modelo %q não aceita chamadas de ferramenta no Ollama, e o agente depende delas para ler e editar arquivos; escolha um modelo com suporte a tools", modelID)
		}
	}
	return thinking, nil
}
