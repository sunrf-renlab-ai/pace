package rules

import (
	"context"
	"sync"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

// R15MentorPulse fires every 2 hours to give brain a chance to do strategic
// review across all projects: are recent activities aligned with goals? Has
// focus drifted at the day-scale? Is anything stalling silently?
//
// Distinct from R8 (event-summary every 30 min, reactive) and R9 (one-shot
// morning plan generation).
type R15MentorPulse struct {
	mu       sync.Mutex
	lastFire time.Time
}

func (*R15MentorPulse) Name() string { return "R15.mentor_pulse" }

func (r *R15MentorPulse) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.lastFire.IsZero() && now.Sub(r.lastFire) < 2*time.Hour {
		return nil, nil
	}
	r.lastFire = now
	return []Trigger{{
		RuleName: "R15.mentor_pulse",
		Reason:   "2-hour mentor pulse — strategic review across active projects",
		Now:      now,
	}}, nil
}
