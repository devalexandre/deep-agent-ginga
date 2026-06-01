package agent

import "testing"

// TestChatSessionEnabledAndResettable verifies that the chat agent gets agno
// session history wired in, and that ResetChatSession rebuilds it with a fresh
// session id (the only way to clear agno's in-memory history).
func TestChatSessionEnabledAndResettable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GINGA_API_KEY", "")

	a, err := NewCoderAgentWithConfig(CoderAgentConfig{
		ModelID:      "ollama:llama3.1:8b",
		DisableShell: true,
	})
	if err != nil {
		t.Fatalf("NewCoderAgentWithConfig: %v", err)
	}

	if a.sessionDB == nil {
		t.Fatal("expected session DB to be initialized")
	}
	if a.ChatSessionID() == "" {
		t.Fatal("expected a non-empty chat session id")
	}
	if !a.agent.GetAddHistoryToMessages() {
		t.Fatal("expected chat agent to have history enabled")
	}
	// Deep role agents must NOT use session history.
	if a.explorer.GetAddHistoryToMessages() {
		t.Fatal("deep explorer agent must not have history enabled")
	}

	prev := a.agent
	if err := a.ResetChatSession("session-2"); err != nil {
		t.Fatalf("ResetChatSession: %v", err)
	}
	if a.ChatSessionID() != "session-2" {
		t.Fatalf("expected session id 'session-2', got %q", a.ChatSessionID())
	}
	if a.agent == prev {
		t.Fatal("expected the chat agent to be rebuilt on reset")
	}
}
