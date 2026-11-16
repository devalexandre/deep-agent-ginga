package agent

import (
	"strings"
	"testing"
)

func TestCompactPhaseContextKeepsImportantLinesAndTail(t *testing.T) {
	agent := &DeepAgent{
		config: DeepAgentConfig{CompressResults: true},
	}

	var b strings.Builder
	b.WriteString("explore output\n")
	b.WriteString("Updated internal/api/server.go and ran go test ./...\n")
	for i := 0; i < 900; i++ {
		b.WriteString("verbose tool output that is not important for the next model call\n")
	}
	b.WriteString("final verification passed\n")

	got := agent.compactPhaseContext(b.String())
	if len(got) > compressedPhaseContextChars {
		t.Fatalf("expected compacted context under %d chars, got %d", compressedPhaseContextChars, len(got))
	}
	for _, want := range []string{
		"COMPACTED PREVIOUS PHASE OUTPUTS",
		"internal/api/server.go",
		"go test ./...",
		"final verification passed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected compacted context to contain %q:\n%s", want, got)
		}
	}
}

func TestCompactPhaseContextLeavesSmallContextUnchanged(t *testing.T) {
	agent := &DeepAgent{}
	value := "small previous output"
	if got := agent.compactPhaseContext(value); got != value {
		t.Fatalf("expected small context unchanged, got %q", got)
	}
}
