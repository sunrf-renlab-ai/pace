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

// Regression test: SQLite stores time.Time in TZ-suffixed text. Local-tz `now`
// vs UTC-stored event timestamps caused lexicographic comparison failures and
// silently dropped events from rule windows. Loop.Once must normalize to UTC.
func TestOnceUTCNormalizesNow(t *testing.T) {
	s, _ := state.Open(t.TempDir() + "/db")
	defer s.Close()

	utcNow := time.Now().UTC()
	for i := 0; i < 2; i++ {
		ev := ingest.Event{
			EventID:        "tz-" + string(rune('0'+i)),
			Timestamp:      utcNow,
			HookType:       "PostToolUse",
			SessionID:      "tz-test",
			ProjectPath:    "/p",
			ToolExitStatus: "error",
		}
		pj, _ := json.Marshal(ev)
		s.DB().Exec(`INSERT INTO events (event_id, timestamp, hook_type, session_id, project_path, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
			ev.EventID, ev.Timestamp.UTC(), ev.HookType, ev.SessionID, ev.ProjectPath, string(pj))
	}

	fn := &fakeNotifier{}
	reg := action.NewRegistry(fn)
	l := New(s, []rules.Rule{&rules.R1ToolErrorBurst{}}, nil, reg)

	// Pass a *local-tz* now. If Once doesn't normalize, the rule's `since` will
	// be in local TZ and the events won't compare correctly.
	beijing, _ := time.LoadLocation("Asia/Shanghai")
	localNow := utcNow.In(beijing)
	l.Once(context.Background(), localNow)

	if fn.count != 1 {
		t.Errorf("notify count = %d, want 1 (R1 should fire after UTC normalization)", fn.count)
	}
}
