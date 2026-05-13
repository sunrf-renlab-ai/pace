package loop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/action"
	"github.com/sunrf-renlab-ai/pace/pkg/ingest"
	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

type fakeNotifier struct{ count int }

func (f *fakeNotifier) Notify(t, b string) error { f.count++; return nil }

type fakeBrain struct {
	d         *Decision
	lastInput DeciderInput
	calls     int
}

func (b *fakeBrain) Decide(ctx context.Context, in DeciderInput) (*Decision, error) {
	b.lastInput = in
	b.calls++
	return b.d, nil
}

func newState(t *testing.T) *state.State {
	t.Helper()
	s, err := state.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func insertEvent(t *testing.T, s *state.State, ev ingest.Event) {
	t.Helper()
	pj, _ := json.Marshal(ev)
	if _, err := s.DB().Exec(`INSERT INTO events (event_id, timestamp, hook_type, session_id, project_path, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
		ev.EventID, ev.Timestamp.UTC(), ev.HookType, ev.SessionID, ev.ProjectPath, string(pj)); err != nil {
		t.Fatalf("insert event: %v", err)
	}
}

// Tick passes events since last tick to brain and runs whatever brain returns.
func TestTickPassesNewEventsToBrain(t *testing.T) {
	s := newState(t)
	now := time.Now().UTC()

	insertEvent(t, s, ingest.Event{
		EventID:        "evt-pass-1",
		Timestamp:      now,
		HookType:       "PostToolUse",
		SessionID:      "s",
		ProjectPath:    "/p",
		ToolExitStatus: "error",
	})

	br := &fakeBrain{d: &Decision{Decision: "ignore"}}
	l := New(s, br, action.NewRegistry(&fakeNotifier{}))
	// Force lastTick into the past so the synthetic event is picked up.
	l.lastTick = now.Add(-time.Hour)

	l.Once(context.Background(), now.Add(time.Minute))

	if br.calls != 1 {
		t.Fatalf("brain calls = %d, want 1", br.calls)
	}
	if !strings.Contains(br.lastInput.EventsJSON, "evt-pass-1") {
		t.Errorf("brain didn't see event; got %s", br.lastInput.EventsJSON)
	}
}

// Brain decision = "ignore" → no actions executed.
func TestTickIgnoreDoesNothing(t *testing.T) {
	s := newState(t)
	br := &fakeBrain{d: &Decision{Decision: "ignore", Rationale: "noise"}}
	fn := &fakeNotifier{}
	l := New(s, br, action.NewRegistry(fn))
	l.Once(context.Background(), time.Now().UTC())
	if fn.count != 0 {
		t.Errorf("ignore fired %d notifies, want 0", fn.count)
	}
}

// Brain decision = "notify" → one notify executed.
func TestTickNotifyRuns(t *testing.T) {
	s := newState(t)
	br := &fakeBrain{d: &Decision{
		Decision:  "notify",
		Rationale: "test fail",
		Params:    map[string]any{"title": "T", "body": "B"},
	}}
	fn := &fakeNotifier{}
	l := New(s, br, action.NewRegistry(fn))
	l.Once(context.Background(), time.Now().UTC())
	if fn.count != 1 {
		t.Errorf("notify count = %d, want 1", fn.count)
	}
}

// Brain decision = "batch" → each sub-action executed.
func TestTickBatchExpands(t *testing.T) {
	s := newState(t)
	br := &fakeBrain{d: &Decision{
		Decision:  "batch",
		Rationale: "two things",
		Params: map[string]any{
			"actions": []any{
				map[string]any{
					"decision":  "notify",
					"rationale": "first",
					"params":    map[string]any{"title": "A", "body": "1"},
				},
				map[string]any{
					"decision":  "notify",
					"rationale": "second",
					"params":    map[string]any{"title": "B", "body": "2"},
				},
			},
		},
	}}
	fn := &fakeNotifier{}
	l := New(s, br, action.NewRegistry(fn))
	l.Once(context.Background(), time.Now().UTC())
	if fn.count != 2 {
		t.Errorf("batch notify count = %d, want 2", fn.count)
	}
}

// nil brain → tick is a no-op (no rule fallback in v0.5).
func TestTickNilBrainIsNoop(t *testing.T) {
	s := newState(t)
	insertEvent(t, s, ingest.Event{
		EventID: "noop", Timestamp: time.Now().UTC(),
		HookType: "Stop", SessionID: "s", ProjectPath: "/p",
	})
	fn := &fakeNotifier{}
	l := New(s, nil, action.NewRegistry(fn))
	l.Once(context.Background(), time.Now().UTC())
	if fn.count != 0 {
		t.Errorf("nil-brain tick fired %d notifies, want 0", fn.count)
	}
}

func TestTickAdvancesLastTick(t *testing.T) {
	s := newState(t)
	br := &fakeBrain{d: &Decision{Decision: "ignore"}}
	l := New(s, br, action.NewRegistry(&fakeNotifier{}))
	t1 := time.Now().UTC().Add(-2 * time.Minute)
	t2 := time.Now().UTC().Add(-1 * time.Minute)
	l.Once(context.Background(), t1)
	first := l.lastTick
	l.Once(context.Background(), t2)
	if !l.lastTick.After(first) {
		t.Errorf("lastTick did not advance: first=%v second=%v", first, l.lastTick)
	}
}
