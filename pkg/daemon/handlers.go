package daemon

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/sunrf-renlab-ai/pace/pkg/action"
	"github.com/sunrf-renlab-ai/pace/pkg/ipc"
	"github.com/sunrf-renlab-ai/pace/pkg/pm"
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
		reply := "(Pace offline — brain not configured. v0.1 runs rules-only with direct notifications. " +
			"To enable LLM-driven decisions, wire OAuth + brain in a future build.)"
		r.d.State.DB().Exec(`INSERT INTO chat_log (message_id, timestamp, role, content) VALUES (?, ?, 'pace', ?)`,
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
	r.d.State.DB().Exec(`INSERT INTO chat_log (message_id, timestamp, role, content) VALUES (?, ?, 'pace', ?)`,
		uuid.New().String(), time.Now().UTC(), reply)
	return ipc.Response{OK: true, Result: map[string]any{"reply": reply}}
}

// ─── PM handlers (v0.3) ────────────────────────────────────────────────

func (r *rpc) goalSet(req ipc.Request) ipc.Response {
	project, _ := req.Params["project_path"].(string)
	desc, _ := req.Params["description"].(string)
	if project == "" || desc == "" {
		return ipc.Response{OK: false, Error: "project_path and description required"}
	}
	g := pm.Goal{ProjectPath: project, Description: desc, Source: "cli"}
	if d, _ := req.Params["deadline"].(string); d != "" {
		dt, err := pm.ParseDeadline(d)
		if err != nil {
			return ipc.Response{OK: false, Error: err.Error()}
		}
		g.Deadline = dt
	}
	if err := pm.SetGoal(r.d.State, g); err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	return ipc.Response{OK: true}
}

func (r *rpc) goalGet(req ipc.Request) ipc.Response {
	project, _ := req.Params["project_path"].(string)
	if project == "" {
		// no path → list all
		gs, err := pm.ListGoals(r.d.State)
		if err != nil {
			return ipc.Response{OK: false, Error: err.Error()}
		}
		return ipc.Response{OK: true, Result: gs}
	}
	g, err := pm.GetGoal(r.d.State, project)
	if err != nil {
		return ipc.Response{OK: false, Error: "no goal for " + project}
	}
	return ipc.Response{OK: true, Result: g}
}

func (r *rpc) goalDelete(req ipc.Request) ipc.Response {
	project, _ := req.Params["project_path"].(string)
	if project == "" {
		return ipc.Response{OK: false, Error: "project_path required"}
	}
	if err := pm.DeleteGoal(r.d.State, project); err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	return ipc.Response{OK: true}
}

func (r *rpc) focusSet(req ipc.Request) ipc.Response {
	project, _ := req.Params["project_path"].(string)
	if project == "" {
		return ipc.Response{OK: false, Error: "project_path required"}
	}
	f := pm.Focus{ProjectPath: project}
	if reason, _ := req.Params["reason"].(string); reason != "" {
		f.Reason = reason
	}
	if u, _ := req.Params["until"].(string); u != "" {
		dt, err := pm.ParseDeadline(u)
		if err != nil {
			return ipc.Response{OK: false, Error: err.Error()}
		}
		f.Until = dt
	}
	if err := pm.SetFocus(r.d.State, f); err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	return ipc.Response{OK: true}
}

func (r *rpc) focusGet(req ipc.Request) ipc.Response {
	f, err := pm.GetFocus(r.d.State)
	if err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	if f == nil {
		return ipc.Response{OK: true, Result: nil}
	}
	return ipc.Response{OK: true, Result: f}
}

func (r *rpc) focusClear(req ipc.Request) ipc.Response {
	if err := pm.ClearFocus(r.d.State); err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	return ipc.Response{OK: true}
}

// planShow returns the latest plan (today scope by default) without regenerating.
func (r *rpc) planShow(req ipc.Request) ipc.Response {
	scope, _ := req.Params["scope"].(string)
	if scope == "" {
		scope = "today"
	}
	p, err := pm.LatestPlan(r.d.State, scope)
	if err != nil {
		return ipc.Response{OK: true, Result: map[string]any{"plan": nil, "message": "no plan yet — run `pace plan generate` to create one"}}
	}
	return ipc.Response{OK: true, Result: p}
}

// planGenerate forces a brain-driven plan generation by synthesizing a
// fake R9.morning_standup trigger and routing it through the loop.
// This lets the user say `pace plan` and get a fresh plan without waiting
// for tomorrow morning.
func (r *rpc) planGenerate(req ipc.Request) ipc.Response {
	if r.d.brain == nil {
		return ipc.Response{OK: false, Error: "brain offline — install `claude` CLI or run `pace login`"}
	}
	scope, _ := req.Params["scope"].(string)
	if scope == "" {
		scope = "today"
	}
	in := r.d.loop.BuildChatPromptInput("Generate a " + scope + " plan now. Respond with decision='generate_plan' and a thorough markdown body in params.content_md.")
	in.TriggerReason = "manual_plan_request"
	d, err := r.d.brain.Decide(context.Background(), in)
	if err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	if d.Decision != "generate_plan" {
		// brain refused; return its reasoning
		return ipc.Response{OK: false, Error: "brain returned decision=" + d.Decision + ": " + d.Rationale}
	}
	// Force the scope param to what the user asked for
	if d.Params == nil {
		d.Params = map[string]any{}
	}
	d.Params["scope"] = scope
	a := &action.Action{Type: "generate_plan", Rationale: d.Rationale, Params: d.Params}
	if err := r.d.actions.Run(context.Background(), r.d.State, a); err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	p, _ := pm.LatestPlan(r.d.State, scope)
	return ipc.Response{OK: true, Result: p}
}
