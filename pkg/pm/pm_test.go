package pm

import (
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

func newState(t *testing.T) *state.State {
	t.Helper()
	s, err := state.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSetAndGetGoal(t *testing.T) {
	s := newState(t)
	deadline := time.Date(2026, 6, 13, 23, 59, 59, 0, time.UTC)
	g := Goal{
		ProjectPath: "/p/foo",
		Description: "ship v1",
		Deadline:    &deadline,
		Milestones:  []Milestone{{Name: "MVP", Done: true}, {Name: "polish", Done: false}},
		Source:      "cli",
	}
	if err := SetGoal(s, g); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	got, err := GetGoal(s, "/p/foo")
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if got.Description != "ship v1" {
		t.Errorf("description = %q", got.Description)
	}
	if got.Deadline == nil || !got.Deadline.Equal(deadline) {
		t.Errorf("deadline = %v, want %v", got.Deadline, deadline)
	}
	if len(got.Milestones) != 2 || !got.Milestones[0].Done || got.Milestones[1].Done {
		t.Errorf("milestones = %+v", got.Milestones)
	}
}

func TestSetGoalUpserts(t *testing.T) {
	s := newState(t)
	SetGoal(s, Goal{ProjectPath: "/p", Description: "v1"})
	SetGoal(s, Goal{ProjectPath: "/p", Description: "v2"})
	g, _ := GetGoal(s, "/p")
	if g.Description != "v2" {
		t.Errorf("description = %q, want v2", g.Description)
	}
}

func TestListGoals(t *testing.T) {
	s := newState(t)
	SetGoal(s, Goal{ProjectPath: "/p1", Description: "g1"})
	SetGoal(s, Goal{ProjectPath: "/p2", Description: "g2"})
	gs, err := ListGoals(s)
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(gs) != 2 {
		t.Errorf("len = %d, want 2", len(gs))
	}
}

func TestDeleteGoal(t *testing.T) {
	s := newState(t)
	SetGoal(s, Goal{ProjectPath: "/p", Description: "x"})
	DeleteGoal(s, "/p")
	if _, err := GetGoal(s, "/p"); err == nil {
		t.Errorf("expected error after delete")
	}
}

func TestSetAndGetFocus(t *testing.T) {
	s := newState(t)
	until := time.Now().Add(24 * time.Hour).UTC()
	f := Focus{ProjectPath: "/p/focus", Reason: "ship friday", Until: &until}
	if err := SetFocus(s, f); err != nil {
		t.Fatalf("SetFocus: %v", err)
	}
	got, err := GetFocus(s)
	if err != nil {
		t.Fatalf("GetFocus: %v", err)
	}
	if got == nil {
		t.Fatal("got nil focus")
	}
	if got.ProjectPath != "/p/focus" {
		t.Errorf("project = %q", got.ProjectPath)
	}
	if got.Reason != "ship friday" {
		t.Errorf("reason = %q", got.Reason)
	}
	if got.Until == nil || got.Until.Sub(until) > time.Second {
		t.Errorf("until = %v, want %v", got.Until, until)
	}
}

func TestFocusAutoExpires(t *testing.T) {
	s := newState(t)
	past := time.Now().Add(-1 * time.Hour).UTC()
	SetFocus(s, Focus{ProjectPath: "/p", Until: &past})
	got, err := GetFocus(s)
	if err != nil {
		t.Fatalf("GetFocus: %v", err)
	}
	if got != nil {
		t.Errorf("expected expired focus to return nil, got %+v", got)
	}
}

func TestClearFocus(t *testing.T) {
	s := newState(t)
	SetFocus(s, Focus{ProjectPath: "/p"})
	ClearFocus(s)
	got, _ := GetFocus(s)
	if got != nil {
		t.Errorf("expected nil after clear")
	}
}

func TestSavePlanAndLatest(t *testing.T) {
	s := newState(t)
	id1, err := SavePlan(s, Plan{Scope: "today", ContentMD: "# Today\n- A\n- B", Source: "cli"})
	if err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	if id1 == "" {
		t.Errorf("plan_id empty")
	}
	time.Sleep(10 * time.Millisecond) // ensure ordering
	id2, _ := SavePlan(s, Plan{Scope: "today", ContentMD: "# Today\n- C", Source: "cli"})

	got, err := LatestPlan(s, "today")
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if got.PlanID != id2 {
		t.Errorf("latest = %q, want %q", got.PlanID, id2)
	}
}

func TestParseDeadline(t *testing.T) {
	cases := []struct {
		in       string
		wantNil  bool
		wantYear int
	}{
		{"", true, 0},
		{"2026-06-13", false, 2026},
		{"2026-06-13T12:00:00Z", false, 2026},
	}
	for _, c := range cases {
		got, err := ParseDeadline(c.in)
		if err != nil {
			t.Errorf("ParseDeadline(%q): %v", c.in, err)
			continue
		}
		if c.wantNil && got != nil {
			t.Errorf("ParseDeadline(%q) = %v, want nil", c.in, got)
		}
		if !c.wantNil && (got == nil || got.Year() != c.wantYear) {
			t.Errorf("ParseDeadline(%q) year = %d, want %d", c.in, got.Year(), c.wantYear)
		}
	}
	if _, err := ParseDeadline("garbage"); err == nil {
		t.Errorf("expected error on garbage deadline")
	}
}

func TestGoalsJSONFocusJSON(t *testing.T) {
	s := newState(t)
	SetGoal(s, Goal{ProjectPath: "/p", Description: "x"})
	SetFocus(s, Focus{ProjectPath: "/p", Reason: "now"})
	if GoalsJSON(s) == "[]" {
		t.Errorf("GoalsJSON should not be []")
	}
	if FocusJSON(s) == "null" {
		t.Errorf("FocusJSON should not be null")
	}
}
