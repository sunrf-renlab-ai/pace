package mentor

import (
	"testing"

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

func TestSaveAndGet(t *testing.T) {
	s := newState(t)
	o := Opinion{
		Trigger:        "R11.commit_review",
		ProjectPath:    "/p",
		Topic:          "Big commit conflates layers",
		Observation:    "commit abc123 changes 14 files across pkg/db, pkg/http, and cmd/foo",
		Concern:        "single commit mixes data-layer + transport changes — hard to revert one without other",
		Recommendation: "split into two commits",
		Confidence:     ConfidenceHigh,
		Evidence:       []string{"abc123", "pkg/db/users.go:42", "pkg/http/router.go:88"},
	}
	id, err := Save(s, o)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == "" {
		t.Fatal("empty id")
	}
	got, err := Get(s, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Topic != o.Topic {
		t.Errorf("topic = %q", got.Topic)
	}
	if len(got.Evidence) != 3 {
		t.Errorf("evidence = %v", got.Evidence)
	}
	if got.Confidence != ConfidenceHigh {
		t.Errorf("confidence = %q", got.Confidence)
	}
	if got.Status != StatusOpen {
		t.Errorf("status = %q, want open", got.Status)
	}
}

func TestSaveValidation(t *testing.T) {
	s := newState(t)
	cases := []Opinion{
		{Trigger: "x", Observation: "y"}, // missing topic
		{Trigger: "x", Topic: "y"},       // missing observation
		{Topic: "y", Observation: "z"},   // missing trigger
		{Trigger: "x", Topic: "y", Observation: "z", Confidence: "wat"}, // bad confidence
	}
	for i, o := range cases {
		if _, err := Save(s, o); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestListOpenExcludesDismissed(t *testing.T) {
	s := newState(t)
	id1, _ := Save(s, Opinion{Trigger: "t", Topic: "a", Observation: "o"})
	id2, _ := Save(s, Opinion{Trigger: "t", Topic: "b", Observation: "o"})
	Save(s, Opinion{Trigger: "t", Topic: "c", Observation: "o"})

	if err := Dismiss(s, id1); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if err := Acknowledge(s, id2); err != nil {
		t.Fatalf("ack: %v", err)
	}

	open, err := ListOpen(s, 10)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(open) != 1 {
		t.Errorf("open count = %d, want 1", len(open))
	}
	if len(open) > 0 && open[0].Topic != "c" {
		t.Errorf("open[0].Topic = %q, want c", open[0].Topic)
	}

	all, err := ListAll(s, 10)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all count = %d, want 3", len(all))
	}
}

func TestOpinionsJSON(t *testing.T) {
	s := newState(t)
	if OpinionsJSON(s, 5) != "[]" {
		t.Errorf("expected [] when no opinions")
	}
	Save(s, Opinion{Trigger: "t", Topic: "x", Observation: "y"})
	if OpinionsJSON(s, 5) == "[]" {
		t.Errorf("expected non-empty JSON")
	}
}

func TestDefaultConfidenceIsMedium(t *testing.T) {
	s := newState(t)
	id, _ := Save(s, Opinion{Trigger: "t", Topic: "a", Observation: "b"})
	o, _ := Get(s, id)
	if o.Confidence != ConfidenceMedium {
		t.Errorf("default confidence = %q, want medium", o.Confidence)
	}
}
