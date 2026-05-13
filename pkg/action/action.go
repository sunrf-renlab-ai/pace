package action

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

type Action struct {
	ActionID       string
	Type           string
	ProjectPath    string
	TriggerEventID string
	Rationale      string
	Params         map[string]any
}

type Executor interface {
	Execute(ctx context.Context, s *state.State, a *Action) error
}

type Notifier interface {
	Notify(title, body string) error
}

type Registry struct {
	executors map[string]Executor
	notifier  Notifier
}

func NewRegistry(n Notifier) *Registry {
	r := &Registry{executors: map[string]Executor{}, notifier: n}
	r.executors["notify"] = &notifyExec{n: n}
	r.executors["sync_files"] = SyncFilesExec{}
	r.executors["pause_project"] = PauseProjectExec{}
	r.executors["set_pref"] = SetPrefExec{}
	r.executors["generate_plan"] = GeneratePlanExec{}
	r.executors["mentor_review"] = MentorReviewExec{N: n}
	return r
}

func (r *Registry) Register(typ string, e Executor) { r.executors[typ] = e }

func (r *Registry) Run(ctx context.Context, s *state.State, a *Action) error {
	if a.ActionID == "" {
		a.ActionID = uuid.New().String()
	}
	pj, _ := json.Marshal(a.Params)
	if _, err := s.DB().Exec(`INSERT INTO actions (action_id, timestamp, action_type, project_path, trigger_event_id, rationale, parameters_json, status) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')`,
		a.ActionID, time.Now().UTC(), a.Type, a.ProjectPath, a.TriggerEventID, a.Rationale, string(pj)); err != nil {
		return err
	}

	exec, ok := r.executors[a.Type]
	if !ok {
		s.DB().Exec(`UPDATE actions SET status='failed', result_summary=? WHERE action_id=?`, "no executor for type "+a.Type, a.ActionID)
		return nil
	}
	if err := exec.Execute(ctx, s, a); err != nil {
		s.DB().Exec(`UPDATE actions SET status='failed', result_summary=? WHERE action_id=?`, err.Error(), a.ActionID)
		return err
	}
	s.DB().Exec(`UPDATE actions SET status='done' WHERE action_id=?`, a.ActionID)
	return nil
}

var errInvalidParams = errors.New("invalid params")

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func mergeEnv(extra map[string]string) []string {
	env := append([]string{}, os.Environ()...)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
