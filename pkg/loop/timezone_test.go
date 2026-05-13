package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/action"
	"github.com/sunrf-renlab-ai/pace/pkg/ingest"
)

// Regression test: SQLite stores time.Time in TZ-suffixed text. Local-tz `now`
// vs UTC-stored event timestamps caused lexicographic comparison failures and
// silently dropped events from the tick window. Loop.Once must normalize to UTC.
func TestTickEventQueryUTCNormalized(t *testing.T) {
	s := newState(t)

	utcNow := time.Now().UTC()
	insertEvent(t, s, ingest.Event{
		EventID:        "tz-1",
		Timestamp:      utcNow,
		HookType:       "PostToolUse",
		SessionID:      "tz-test",
		ProjectPath:    "/p",
		ToolExitStatus: "error",
	})

	br := &fakeBrain{d: &Decision{Decision: "ignore"}}
	l := New(s, br, action.NewRegistry(&fakeNotifier{}))
	// Force lastTick into the past in *local* TZ. If eventsSince doesn't
	// normalize, the SQL comparison would compare local-tz strings against
	// UTC-stored strings and miss the event.
	beijing, _ := time.LoadLocation("Asia/Shanghai")
	l.lastTick = utcNow.Add(-time.Hour).In(beijing)

	l.Once(context.Background(), utcNow.Add(time.Minute))

	if !strings.Contains(br.lastInput.EventsJSON, "tz-1") {
		t.Errorf("brain didn't see event tz-1 across TZ boundary; got %s", br.lastInput.EventsJSON)
	}
}
