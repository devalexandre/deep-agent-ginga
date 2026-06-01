package agent

import (
	"strings"
	"testing"
)

func TestNewCoderAgent(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GINGA_API_KEY", "")
	apiKey := "test-key"
	agent, err := NewCoderAgent(apiKey)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	if agent == nil {
		t.Fatal("Agent should not be nil")
	}

	if agent.agent == nil {
		t.Fatal("Internal agno agent should not be nil")
	}
}

func TestNewCoderAgent_NoApiKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GINGA_API_KEY", "")
	_, err := NewCoderAgent("")
	if err == nil {
		t.Fatal("Should fail when API key is missing")
	}
}

func TestNewCoderAgentWithOllamaDoesNotRequireAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GINGA_API_KEY", "")

	agent, err := NewCoderAgentWithConfig(CoderAgentConfig{
		ModelID:      "ollama:llama3.1:8b",
		DisableShell: true,
	})
	if err != nil {
		t.Fatalf("expected ollama agent without API key: %v", err)
	}
	if agent.ModelID() != "ollama:llama3.1:8b" {
		t.Fatalf("unexpected model id: %q", agent.ModelID())
	}
}

func TestPhasePromptContractsSupportDeepWorkflow(t *testing.T) {
	// Structured phases keep the rigid "Return these sections:" contract.
	for _, phase := range []string{"explore", "plan", "implement", "verify"} {
		objective := phaseObjective(phase)
		contract := phaseOutputContract(phase)

		if strings.TrimSpace(objective) == "" {
			t.Fatalf("expected objective for phase %q", phase)
		}
		if !strings.Contains(contract, "Return these sections:") {
			t.Fatalf("expected structured output contract for phase %q, got %q", phase, contract)
		}
	}

	// final-report is intentionally adaptive: it must not force a fixed section
	// template, but must steer toward the user's language and the substance.
	finalObjective := phaseObjective("final-report")
	if strings.TrimSpace(finalObjective) == "" {
		t.Fatal("expected objective for phase \"final-report\"")
	}
	finalContract := phaseOutputContract("final-report")
	for _, want := range []string{"user's language", "substance", "Written deliverable"} {
		if !strings.Contains(finalContract, want) {
			t.Fatalf("final-report contract missing %q: %s", want, finalContract)
		}
	}
}

func TestPhaseOutputContractIncludesVerificationSignals(t *testing.T) {
	planContract := phaseOutputContract("plan")
	for _, want := range []string{"Files to change", "Implementation steps", "Verification commands", "Risks"} {
		if !strings.Contains(planContract, want) {
			t.Fatalf("plan contract missing %q: %s", want, planContract)
		}
	}

	verifyContract := phaseOutputContract("verify")
	for _, want := range []string{"Commands run", "Results", "Checks not run and why"} {
		if !strings.Contains(verifyContract, want) {
			t.Fatalf("verify contract missing %q: %s", want, verifyContract)
		}
	}
}
