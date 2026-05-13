package ingest

import (
	"encoding/json"
	"net/http"

	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

type Handler struct {
	state   *state.State
	onEvent func() // optional callback fired after a successful event store
}

func NewHandler(s *state.State) *Handler { return &Handler{state: s} }

// SetOnEvent registers a callback to be fired (non-blocking; in-handler) after
// each event is successfully persisted. The daemon uses this to nudge the
// loop's debouncer so brain reacts to new events instead of polling.
func (h *Handler) SetOnEvent(fn func()) { h.onEvent = fn }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/event" {
		http.NotFound(w, r)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var ev Event
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := ev.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.store(&ev); err != nil {
		http.Error(w, "store: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if h.onEvent != nil {
		h.onEvent()
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) store(ev *Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	tx, err := h.state.DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tsUTC := ev.Timestamp.UTC()
	_, err = tx.Exec(`INSERT OR IGNORE INTO events
		(event_id, timestamp, hook_type, session_id, project_path, payload_json)
		VALUES (?, ?, ?, ?, ?, ?)`,
		ev.EventID, tsUTC, ev.HookType, ev.SessionID, ev.ProjectPath, string(payload))
	if err != nil {
		return err
	}

	_, err = tx.Exec(`INSERT INTO projects (project_path, display_name, first_seen, last_active)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(project_path) DO UPDATE SET last_active = excluded.last_active`,
		ev.ProjectPath, displayNameFor(ev.ProjectPath), ev.Timestamp, ev.Timestamp)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func displayNameFor(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
