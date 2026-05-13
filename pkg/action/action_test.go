package action

import (
	"context"
	"testing"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type fakeNotifier struct {
	calls []struct{ Title, Body string }
}

func (f *fakeNotifier) Notify(title, body string) error {
	f.calls = append(f.calls, struct{ Title, Body string }{title, body})
	return nil
}

func newTestState(t *testing.T) *state.State {
	t.Helper()
	s, err := state.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRegistryRunsNotify(t *testing.T) {
	s := newTestState(t)
	fn := &fakeNotifier{}
	r := NewRegistry(fn)
	a := &Action{Type: "notify", Rationale: "test", Params: map[string]any{"title": "T", "body": "B"}}
	if err := r.Run(context.Background(), s, a); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fn.calls) != 1 {
		t.Fatalf("notify calls = %d, want 1", len(fn.calls))
	}
	if fn.calls[0].Title != "T" || fn.calls[0].Body != "B" {
		t.Errorf("got %+v", fn.calls[0])
	}
	var status string
	s.DB().QueryRow("SELECT status FROM actions WHERE action_id=?", a.ActionID).Scan(&status)
	if status != "done" {
		t.Errorf("status = %q, want done", status)
	}
}

func TestRegistryUnknownTypeMarksFailed(t *testing.T) {
	s := newTestState(t)
	r := NewRegistry(&fakeNotifier{})
	a := &Action{Type: "nonexistent"}
	r.Run(context.Background(), s, a)
	var status string
	s.DB().QueryRow("SELECT status FROM actions WHERE action_id=?", a.ActionID).Scan(&status)
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
}

func TestSetPrefStoresValue(t *testing.T) {
	s := newTestState(t)
	r := NewRegistry(&fakeNotifier{})
	a := &Action{Type: "set_pref", Params: map[string]any{"key": "priority.foo", "value": "high"}}
	if err := r.Run(context.Background(), s, a); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var v string
	s.DB().QueryRow(`SELECT value FROM user_prefs WHERE key=?`, "priority.foo").Scan(&v)
	if v != "high" {
		t.Errorf("value = %q, want high", v)
	}
}

func TestUndoSetPref(t *testing.T) {
	s := newTestState(t)
	r := NewRegistry(&fakeNotifier{})
	r.Run(context.Background(), s, &Action{Type: "set_pref", Params: map[string]any{"key": "k", "value": "v1"}})
	r.Run(context.Background(), s, &Action{Type: "set_pref", Params: map[string]any{"key": "k", "value": "v2"}})
	n, err := UndoLast(context.Background(), s, 1)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if n != 1 {
		t.Errorf("undone = %d, want 1", n)
	}
	var v string
	s.DB().QueryRow(`SELECT value FROM user_prefs WHERE key=?`, "k").Scan(&v)
	if v != "v1" {
		t.Errorf("value after undo = %q, want v1", v)
	}
}
