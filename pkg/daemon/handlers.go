package daemon

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/sunrf-renlab-ai/mentor/pkg/action"
	"github.com/sunrf-renlab-ai/mentor/pkg/ipc"
)

type rpc struct {
	d *Daemon
}

func (r *rpc) status(req ipc.Request) ipc.Response {
	var n int
	r.d.State.DB().QueryRow(`SELECT COUNT(*) FROM projects WHERE paused_until IS NULL OR paused_until > datetime('now')`).Scan(&n)
	var todayActions int
	r.d.State.DB().QueryRow(`SELECT COUNT(*) FROM actions WHERE timestamp > datetime('now', '-1 day')`).Scan(&todayActions)
	var events24h int
	r.d.State.DB().QueryRow(`SELECT COUNT(*) FROM events WHERE timestamp > datetime('now', '-1 day')`).Scan(&events24h)
	return ipc.Response{OK: true, Result: map[string]any{
		"active_projects": n,
		"events_24h":      events24h,
		"actions_24h":     todayActions,
		"brain":           r.d.brain != nil,
	}}
}

func (r *rpc) pause(req ipc.Request) ipc.Response {
	project, _ := req.Params["project_path"].(string)
	if project == "" {
		return ipc.Response{OK: false, Error: "project_path required"}
	}
	a := &action.Action{Type: "pause_project", Params: req.Params, Rationale: "user pause via CLI"}
	if err := r.d.actions.Run(context.Background(), r.d.State, a); err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	return ipc.Response{OK: true}
}

func (r *rpc) undo(req ipc.Request) ipc.Response {
	n := 1
	if v, ok := req.Params["n"].(float64); ok {
		n = int(v)
	}
	count, err := action.UndoLast(context.Background(), r.d.State, n)
	if err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	return ipc.Response{OK: true, Result: map[string]any{"undone": count}}
}

func (r *rpc) actions(req ipc.Request) ipc.Response {
	rows, err := r.d.State.DB().Query(`SELECT action_id, timestamp, action_type, COALESCE(project_path, ''), rationale, status FROM actions ORDER BY timestamp DESC LIMIT 50`)
	if err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	defer rows.Close()
	type row struct {
		ID, Ts, Type, Project, Rationale, Status string
	}
	var out []row
	for rows.Next() {
		var x row
		rows.Scan(&x.ID, &x.Ts, &x.Type, &x.Project, &x.Rationale, &x.Status)
		out = append(out, x)
	}
	return ipc.Response{OK: true, Result: out}
}

func (r *rpc) chat(req ipc.Request) ipc.Response {
	msg, _ := req.Params["message"].(string)
	if msg == "" {
		return ipc.Response{OK: false, Error: "message required"}
	}
	r.d.State.DB().Exec(`INSERT INTO chat_log (message_id, timestamp, role, content) VALUES (?, ?, 'user', ?)`,
		uuid.New().String(), time.Now().UTC(), msg)

	if r.d.brain == nil {
		reply := "(Mentor offline — brain not configured. v0.1 runs rules-only with direct notifications. " +
			"To enable LLM-driven decisions, wire OAuth + brain in a future build.)"
		r.d.State.DB().Exec(`INSERT INTO chat_log (message_id, timestamp, role, content) VALUES (?, ?, 'mentor', ?)`,
			uuid.New().String(), time.Now().UTC(), reply)
		return ipc.Response{OK: true, Result: map[string]any{"reply": reply}}
	}

	in := r.d.loop.BuildChatPromptInput(msg)
	d, err := r.d.brain.Decide(context.Background(), in)
	if err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	reply := d.Rationale
	if d.Decision != "ignore" && d.Decision != "" {
		a := &action.Action{Type: d.Decision, Rationale: d.Rationale, Params: d.Params}
		r.d.actions.Run(context.Background(), r.d.State, a)
		reply += " (action: " + d.Decision + ")"
	}
	r.d.State.DB().Exec(`INSERT INTO chat_log (message_id, timestamp, role, content) VALUES (?, ?, 'mentor', ?)`,
		uuid.New().String(), time.Now().UTC(), reply)
	return ipc.Response{OK: true, Result: map[string]any{"reply": reply}}
}
