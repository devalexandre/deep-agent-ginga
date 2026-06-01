package deepagent

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestResolveDeepAgentConfigDefaults(t *testing.T) {
	t.Setenv("GINGA_API_KEY", "env-key")
	t.Setenv("OPENAI_API_KEY", "")

	config, err := resolveDeepAgentConfig(DeepAgentConfig{})
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}

	if config.APIKey != "env-key" {
		t.Fatalf("expected env API key, got %q", config.APIKey)
	}
	if config.Workspace != "." {
		t.Fatalf("expected default workspace, got %q", config.Workspace)
	}
	if config.Model != "gpt-4o" {
		t.Fatalf("expected default model, got %q", config.Model)
	}
	if config.MaxIterations != 8 {
		t.Fatalf("expected default max iterations, got %d", config.MaxIterations)
	}
}

func TestResolveDeepAgentConfigAllowsLocalModelsWithoutAPIKey(t *testing.T) {
	t.Setenv("GINGA_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	_ = os.Unsetenv("GINGA_API_KEY")
	_ = os.Unsetenv("OPENAI_API_KEY")

	config, err := resolveDeepAgentConfig(DeepAgentConfig{Model: "ollama:llama3.1:8b"})
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}
	if config.APIKey != "" {
		t.Fatalf("expected empty API key for local model, got %q", config.APIKey)
	}
}

func TestNormalizeModelKeepsProviderPrefix(t *testing.T) {
	got := normalizeModel("openai-responses:gpt-5.2")
	if got != "openai-responses:gpt-5.2" {
		t.Fatalf("unexpected model: %q", got)
	}
}

func TestBuildSDKTaskAddsRunContext(t *testing.T) {
	got := buildSDKTask("Map the project.", runOptions{
		userID:    "dev@example.com",
		sessionID: "demo",
	})

	for _, want := range []string{
		"SDK RUN CONTEXT",
		"user_id: dev@example.com",
		"session_id: demo",
		"TASK\nMap the project.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected task to contain %q:\n%s", want, got)
		}
	}
}

func TestUniqueCleanSplitsAndDedupes(t *testing.T) {
	got := uniqueClean([]string{"go-expert, custom", "go-expert", " other "})
	want := []string{"go-expert", "custom", "other"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected values:\nwant %#v\ngot  %#v", want, got)
	}
}
