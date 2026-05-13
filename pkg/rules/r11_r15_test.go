package rules

import (
	"context"
	"testing"
	"time"
)

func TestR15FiresEvery2Hours(t *testing.T) {
	s := helperState(t)
	r := &R15MentorPulse{}
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 1 {
		t.Fatalf("first eval triggers = %d, want 1", len(tr))
	}
	tr2, _ := r.Evaluate(context.Background(), s, now.Add(time.Hour))
	if len(tr2) != 0 {
		t.Errorf("1h later triggers = %d, want 0", len(tr2))
	}
	tr3, _ := r.Evaluate(context.Background(), s, now.Add(2*time.Hour+time.Minute))
	if len(tr3) != 1 {
		t.Errorf("2h1m later triggers = %d, want 1", len(tr3))
	}
}

// R11 needs a real git repo to be meaningful. Test the helpers + the
// "no-project" path; full integration tested by daemon e2e.
func TestR11NoProjectsNoTriggers(t *testing.T) {
	s := helperState(t)
	r := &R11CommitReview{}
	tr, err := r.Evaluate(context.Background(), s, time.Now().UTC())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(tr) != 0 {
		t.Errorf("triggers = %d, want 0", len(tr))
	}
}

func TestShortSHA(t *testing.T) {
	if got := shortSHA("abc1234567890"); got != "abc1234" {
		t.Errorf("shortSHA = %q, want abc1234", got)
	}
	if got := shortSHA("abc"); got != "abc" {
		t.Errorf("shortSHA short = %q", got)
	}
}

func TestAllReturnsEightRules(t *testing.T) {
	if got := len(All()); got != 8 {
		t.Errorf("All() = %d rules, want 8", got)
	}
}
