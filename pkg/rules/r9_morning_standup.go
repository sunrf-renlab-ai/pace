package rules

import (
	"context"
	"sync"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

// R9MorningStandup fires once per local-day, at or after the configured hour
// (default 9 AM local). It produces one Trigger that the brain turns into a
// generate_plan action. Idempotent within a day via in-memory lastFireDay.
type R9MorningStandup struct {
	mu          sync.Mutex
	lastFireDay string // YYYY-MM-DD in local TZ
	HourLocal   int    // 0-23, default 9
}

func (*R9MorningStandup) Name() string { return "R9.morning_standup" }

func (r *R9MorningStandup) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hour := r.HourLocal
	if hour == 0 {
		hour = 9
	}
	local := now.Local()
	day := local.Format("2006-01-02")
	if r.lastFireDay == day {
		return nil, nil
	}
	if local.Hour() < hour {
		return nil, nil
	}
	r.lastFireDay = day
	return []Trigger{{
		RuleName: "R9.morning_standup",
		Reason:   "morning standup — generate today's plan",
		Now:      now,
	}}, nil
}
