package rules

import (
	"context"
	"sync"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type R8PeriodicOverview struct {
	mu       sync.Mutex
	lastFire time.Time
}

func (*R8PeriodicOverview) Name() string { return "R8.periodic_overview" }

func (r *R8PeriodicOverview) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.lastFire.IsZero() && now.Sub(r.lastFire) < 30*time.Minute {
		return nil, nil
	}
	r.lastFire = now
	return []Trigger{{
		RuleName: "R8.periodic_overview",
		Reason:   "30-min sweep",
		Now:      now,
	}}, nil
}
