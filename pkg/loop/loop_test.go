package loop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/action"
	"github.com/sunrf-renlab-ai/pace/pkg/ingest"
	"github.com/sunrf-renlab-ai/pace/pkg/rules"
	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

type fakeNotifier struct{ count int }

func (f *fakeNotifier) Notify(t, b string) error { f.count++; return nil }

func TestOnceFiresNotifyOnTriggerWhenBrainNil(t *testing.T) {
	s, _ := state.Open(t.TempDir() + "/db")
	defer s.Close()
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		ev := ingest.Event{
			EventID:        "e" + string(rune('0'+i)),
			Timestamp:      now,
			HookType:       "PostToolUse",
			SessionID:      "s",
			ProjectPath:    "/p",
			ToolName:       "Bash",
			ToolExitStatus: "error",
		}
		pj, _ := json.Marshal(ev)
		s.DB().Exec(`INSERT INTO events (event_id, timestamp, hook_type, session_id, project_path, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
			ev.EventID, ev.Timestamp, ev.HookType, ev.SessionID, ev.ProjectPath, string(pj))
	}
	fn := &fakeNotifier{}
	reg := action.NewRegistry(fn)
	l := New(s, []rules.Rule{&rules.R1ToolErrorBurst{}}, nil, reg)
	l.Once(context.Background(), now)
	if fn.count != 1 {
		t.Errorf("notify count = %d, want 1", fn.count)
	}
}

type fakeBrain struct{ d *Decision }

func (b *fakeBrain) Decide(ctx context.Context, in DeciderInput) (*Decision, error) {
	return b.d, nil
}

func TestOnceUsesBrainDecision(t *testing.T) {
	s, _ := state.Open(t.TempDir() + "/db")
	defer s.Close()
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		ev := ingest.Event{
			EventID:        "x" + string(rune('0'+i)),
			Timestamp:      now,
			HookType:       "PostToolUse",
			SessionID:      "s",
			ProjectPath:    "/p",
			ToolName:       "Bash",
			ToolExitStatus: "error",
		}
		pj, _ := json.Marshal(ev)
		s.DB().Exec(`INSERT INTO events (event_id, timestamp, hook_type, session_id, project_path, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
			ev.EventID, ev.Timestamp, ev.HookType, ev.SessionID, ev.ProjectPath, string(pj))
	}
	fn := &fakeNotifier{}
	reg := action.NewRegistry(fn)
	br := &fakeBrain{d: &Decision{Decision: "notify", Rationale: "from brain", Params: map[string]any{"title": "X"}}}
	l := New(s, []rules.Rule{&rules.R1ToolErrorBurst{}}, br, reg)
	l.Once(context.Background(), now)
	if fn.count != 1 {
		t.Errorf("notify count = %d, want 1", fn.count)
	}
}
