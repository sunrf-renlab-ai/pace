package ingest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

func setupTestState(t *testing.T) *state.State {
	t.Helper()
	s, err := state.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestIngestPostStoresEvent(t *testing.T) {
	s := setupTestState(t)
	h := NewHandler(s)
	srv := httptest.NewServer(h)
	defer srv.Close()

	ev := Event{
		EventID:     "test-evt-1",
		Timestamp:   time.Now().UTC(),
		HookType:    "PostToolUse",
		SessionID:   "sess-1",
		ProjectPath: "/Users/x/project/foo",
		ToolName:    "Bash",
	}
	body, _ := json.Marshal(ev)
	resp, err := http.Post(srv.URL+"/event", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var count int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM events WHERE event_id = ?", "test-evt-1").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("events = %d, want 1", count)
	}
}

func TestIngestRejectsBadSchema(t *testing.T) {
	s := setupTestState(t)
	h := NewHandler(s)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/event", "application/json", bytes.NewReader([]byte("{bad")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestIngestUpsertsProject(t *testing.T) {
	s := setupTestState(t)
	h := NewHandler(s)
	srv := httptest.NewServer(h)
	defer srv.Close()

	ev := Event{
		EventID:     "evt-2",
		Timestamp:   time.Now().UTC(),
		HookType:    "UserPromptSubmit",
		SessionID:   "sess-2",
		ProjectPath: "/Users/x/project/bar",
	}
	body, _ := json.Marshal(ev)
	http.Post(srv.URL+"/event", "application/json", bytes.NewReader(body))

	var count int
	s.DB().QueryRow("SELECT COUNT(*) FROM projects WHERE project_path = ?", "/Users/x/project/bar").Scan(&count)
	if count != 1 {
		t.Errorf("projects = %d, want 1", count)
	}
}
