package rules

import (
	"context"
	"sync"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

// R10FocusDrift watches for the user spending substantial time on a project
// other than the one they declared as focus. Cooldown so we don't nag.
type R10FocusDrift struct {
	mu       sync.Mutex
	lastFire time.Time
}

func (*R10FocusDrift) Name() string { return "R10.focus_drift" }

func (r *R10FocusDrift) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Cooldown: at most once per hour.
	if !r.lastFire.IsZero() && now.Sub(r.lastFire) < time.Hour {
		return nil, nil
	}

	// Read focus from user_prefs directly (avoids importing pm and creating a cycle).
	var focusProject string
	s.DB().QueryRow(`SELECT value FROM user_prefs WHERE key = 'focus.project_path'`).Scan(&focusProject)
	if focusProject == "" {
		return nil, nil
	}

	// Look at the past hour's events: which non-focus project saw the most activity?
	since := now.Add(-1 * time.Hour)
	rows, err := s.DB().Query(`SELECT project_path, COUNT(*) FROM events WHERE timestamp >= ? GROUP BY project_path`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topOther string
	var topOtherCnt, focusCnt int
	for rows.Next() {
		var p string
		var c int
		rows.Scan(&p, &c)
		if p == focusProject {
			focusCnt = c
		} else if c > topOtherCnt {
			topOther = p
			topOtherCnt = c
		}
	}

	// Drift if some other project has at least 10 events AND ≥3x focus activity.
	if topOther == "" || topOtherCnt < 10 {
		return nil, nil
	}
	if focusCnt > 0 && topOtherCnt < focusCnt*3 {
		return nil, nil
	}

	r.lastFire = now
	return []Trigger{{
		RuleName:    "R10.focus_drift",
		ProjectPath: topOther,
		Reason:      "user is drifting from declared focus (" + focusProject + ") toward " + topOther,
		Now:         now,
	}}, nil
}
