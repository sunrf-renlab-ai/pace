package rules

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/ingest"
)

func TestR9FiresOnceAfterHourThreshold(t *testing.T) {
	s := helperState(t)
	r := &R9MorningStandup{HourLocal: 9}

	// 8 AM local — too early
	morning := time.Date(2026, 5, 13, 8, 0, 0, 0, time.Local).UTC()
	tr, _ := r.Evaluate(context.Background(), s, morning)
	if len(tr) != 0 {
		t.Errorf("8am triggers = %d, want 0", len(tr))
	}

	// 9:30 AM local — should fire once
	t930 := time.Date(2026, 5, 13, 9, 30, 0, 0, time.Local).UTC()
	tr, _ = r.Evaluate(context.Background(), s, t930)
	if len(tr) != 1 {
		t.Errorf("9:30am triggers = %d, want 1", len(tr))
	}

	// 10 AM same day — should not refire
	t10 := time.Date(2026, 5, 13, 10, 0, 0, 0, time.Local).UTC()
	tr, _ = r.Evaluate(context.Background(), s, t10)
	if len(tr) != 0 {
		t.Errorf("same-day refire triggers = %d, want 0", len(tr))
	}

	// Next day 10 AM — should fire again
	t11next := time.Date(2026, 5, 14, 10, 0, 0, 0, time.Local).UTC()
	tr, _ = r.Evaluate(context.Background(), s, t11next)
	if len(tr) != 1 {
		t.Errorf("next-day triggers = %d, want 1", len(tr))
	}
}

func TestR10NoFocusNoTrigger(t *testing.T) {
	s := helperState(t)
	r := &R10FocusDrift{}
	tr, _ := r.Evaluate(context.Background(), s, time.Now().UTC())
	if len(tr) != 0 {
		t.Errorf("no focus → triggers = %d, want 0", len(tr))
	}
}

func TestR10FiresOnDrift(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	// Set focus on /p/A
	s.DB().Exec(`INSERT INTO user_prefs (key, value, set_at, source) VALUES ('focus.project_path', '/p/A', ?, 'pm')`, now)

	// Insert 12 events on /p/B in the last hour, 0 on /p/A
	for i := 0; i < 12; i++ {
		ev := ingest.Event{
			EventID:     "drift-" + string(rune('a'+i)),
			Timestamp:   now.Add(-time.Duration(i) * time.Minute),
			HookType:    "PostToolUse",
			SessionID:   "sess",
			ProjectPath: "/p/B",
			ToolName:    "Edit",
		}
		pj, _ := json.Marshal(ev)
		s.DB().Exec(`INSERT INTO events (event_id, timestamp, hook_type, session_id, project_path, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
			ev.EventID, ev.Timestamp, ev.HookType, ev.SessionID, ev.ProjectPath, string(pj))
	}

	r := &R10FocusDrift{}
	tr, err := r.Evaluate(context.Background(), s, now)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(tr) != 1 {
		t.Fatalf("triggers = %d, want 1", len(tr))
	}
	if tr[0].ProjectPath != "/p/B" {
		t.Errorf("project = %q, want /p/B", tr[0].ProjectPath)
	}
}

func TestR10HasCooldown(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	s.DB().Exec(`INSERT INTO user_prefs (key, value, set_at, source) VALUES ('focus.project_path', '/p/A', ?, 'pm')`, now)
	for i := 0; i < 12; i++ {
		ev := ingest.Event{
			EventID:     "cool-" + string(rune('a'+i)),
			Timestamp:   now.Add(-time.Duration(i) * time.Minute),
			HookType:    "PostToolUse",
			SessionID:   "s",
			ProjectPath: "/p/B",
			ToolName:    "Edit",
		}
		pj, _ := json.Marshal(ev)
		s.DB().Exec(`INSERT INTO events (event_id, timestamp, hook_type, session_id, project_path, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
			ev.EventID, ev.Timestamp, ev.HookType, ev.SessionID, ev.ProjectPath, string(pj))
	}
	r := &R10FocusDrift{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 1 {
		t.Fatalf("first triggers = %d, want 1", len(tr))
	}
	tr2, _ := r.Evaluate(context.Background(), s, now.Add(30*time.Minute))
	if len(tr2) != 0 {
		t.Errorf("cooldown 30min triggers = %d, want 0", len(tr2))
	}
}

// All-count assertion lives in r11_r15_test.go now (8 rules in v0.4).
