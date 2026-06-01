package agent

import (
	"context"
	"testing"

	"github.com/devalexandre/agno-golang/agno/storage"
)

func TestDefaultSessionDBRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	db, err := defaultSessionDB()
	if err != nil {
		t.Fatalf("defaultSessionDB: %v", err)
	}

	ctx := context.Background()
	run := &storage.AgentRun{
		SessionID:    "s1",
		UserID:       "u1",
		UserMessage:  "remember 42",
		AgentMessage: "got it",
	}
	if err := db.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	runs, err := db.GetRunsForSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetRunsForSession: %v", err)
	}
	if len(runs) != 1 || runs[0].UserMessage != "remember 42" || runs[0].AgentMessage != "got it" {
		t.Fatalf("unexpected runs: %+v", runs)
	}

	// A fresh session id starts with empty history — the basis for /reset.
	if other, err := db.GetRunsForSession(ctx, "s2"); err != nil || len(other) != 0 {
		t.Fatalf("expected empty history for new session, got %d runs (err=%v)", len(other), err)
	}
}
