package rules

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/ingest"
	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

func helperState(t *testing.T) *state.State {
	t.Helper()
	s, err := state.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func helperInsertEvent(t *testing.T, s *state.State, ev ingest.Event) {
	t.Helper()
	pj, _ := json.Marshal(ev)
	_, err := s.DB().Exec(`INSERT INTO events (event_id, timestamp, hook_type, session_id, project_path, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
		ev.EventID, ev.Timestamp, ev.HookType, ev.SessionID, ev.ProjectPath, string(pj))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

// ─── R1 ─────────────────────────────────────────────────────────

func TestR1FiresOnTwoErrorsSameSession(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		helperInsertEvent(t, s, ingest.Event{
			EventID:        "e" + string(rune('0'+i)),
			Timestamp:      now.Add(-time.Duration(i) * time.Minute),
			HookType:       "PostToolUse",
			SessionID:      "sess-1",
			ProjectPath:    "/p",
			ToolName:       "Bash",
			ToolExitStatus: "error",
		})
	}
	r := &R1ToolErrorBurst{}
	tr, err := r.Evaluate(context.Background(), s, now)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(tr) != 1 {
		t.Errorf("triggers = %d, want 1", len(tr))
	}
}

func TestR1DoesNotFireOnSingleError(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	helperInsertEvent(t, s, ingest.Event{
		EventID:        "e0",
		Timestamp:      now,
		HookType:       "PostToolUse",
		SessionID:      "sess-1",
		ProjectPath:    "/p",
		ToolName:       "Bash",
		ToolExitStatus: "error",
	})
	r := &R1ToolErrorBurst{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 0 {
		t.Errorf("triggers = %d, want 0", len(tr))
	}
}

func TestR1DoesNotFireAcrossSessions(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	for i, sess := range []string{"a", "b"} {
		helperInsertEvent(t, s, ingest.Event{
			EventID:        "e" + sess,
			Timestamp:      now.Add(-time.Duration(i) * time.Minute),
			HookType:       "PostToolUse",
			SessionID:      sess,
			ProjectPath:    "/p",
			ToolName:       "Bash",
			ToolExitStatus: "error",
		})
	}
	r := &R1ToolErrorBurst{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 0 {
		t.Errorf("triggers = %d, want 0", len(tr))
	}
}

// ─── R2 ─────────────────────────────────────────────────────────

func TestR2FiresOnTestCommandError(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	helperInsertEvent(t, s, ingest.Event{
		EventID:          "e1",
		Timestamp:        now,
		HookType:         "PostToolUse",
		SessionID:        "s",
		ProjectPath:      "/p",
		ToolName:         "Bash",
		ToolInputSummary: `{"command":"go test ./..."}`,
		ToolExitStatus:   "error",
	})
	r := &R2TestFail{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 1 {
		t.Errorf("triggers = %d, want 1", len(tr))
	}
}

func TestR2IgnoresPassingTest(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	helperInsertEvent(t, s, ingest.Event{
		EventID:          "e1",
		Timestamp:        now,
		HookType:         "PostToolUse",
		SessionID:        "s",
		ProjectPath:      "/p",
		ToolName:         "Bash",
		ToolInputSummary: `{"command":"npm test"}`,
		ToolExitStatus:   "ok",
	})
	r := &R2TestFail{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 0 {
		t.Errorf("triggers = %d, want 0", len(tr))
	}
}

// ─── R3 ─────────────────────────────────────────────────────────

func TestR3FiresOnDeployFail(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	helperInsertEvent(t, s, ingest.Event{
		EventID:          "e1",
		Timestamp:        now,
		HookType:         "PostToolUse",
		SessionID:        "s",
		ProjectPath:      "/p",
		ToolName:         "Bash",
		ToolInputSummary: `{"command":"vercel deploy --prod"}`,
		ToolExitStatus:   "error",
	})
	r := &R3DeployFail{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 1 {
		t.Errorf("triggers = %d, want 1", len(tr))
	}
}

// ─── R8 ─────────────────────────────────────────────────────────

func TestR8FiresEvery30Min(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	r := &R8PeriodicOverview{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 1 {
		t.Fatalf("first eval, triggers = %d, want 1", len(tr))
	}
	tr2, _ := r.Evaluate(context.Background(), s, now.Add(10*time.Minute))
	if len(tr2) != 0 {
		t.Errorf("10 min later triggers = %d, want 0", len(tr2))
	}
	tr3, _ := r.Evaluate(context.Background(), s, now.Add(31*time.Minute))
	if len(tr3) != 1 {
		t.Errorf("31 min later triggers = %d, want 1", len(tr3))
	}
}

// All()-count assertion lives in r9_r10_test.go now (6 rules in v0.3).
