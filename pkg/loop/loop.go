package loop

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/action"
	"github.com/sunrf-renlab-ai/mentor/pkg/rules"
	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

// Decider abstracts the LLM brain. nil = no LLM (degrade to direct notify).
type Decider interface {
	Decide(ctx context.Context, in DeciderInput) (*Decision, error)
}

type DeciderInput struct {
	Now               string
	ProjectsJSON      string
	PrefsJSON         string
	RecentActionsJSON string
	TriggerReason     string
	EventsJSON        string
}

type Decision struct {
	Decision  string         `json:"decision"`
	Rationale string         `json:"rationale"`
	Params    map[string]any `json:"params"`
}

type Loop struct {
	State   *state.State
	Rules   []rules.Rule
	Brain   Decider
	Actions *action.Registry
	Tick    time.Duration
	stop    chan struct{}
}

func New(s *state.State, rs []rules.Rule, b Decider, ar *action.Registry) *Loop {
	return &Loop{State: s, Rules: rs, Brain: b, Actions: ar, Tick: 30 * time.Second, stop: make(chan struct{})}
}

func (l *Loop) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(l.Tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-l.stop:
				return
			case now := <-t.C:
				l.Once(ctx, now)
			}
		}
	}()
}

func (l *Loop) Stop() {
	select {
	case <-l.stop:
	default:
		close(l.stop)
	}
}

func (l *Loop) Once(ctx context.Context, now time.Time) {
	now = now.UTC()
	for _, r := range l.Rules {
		triggers, err := r.Evaluate(ctx, l.State, now)
		if err != nil {
			log.Printf("rule %s: %v", r.Name(), err)
			continue
		}
		for _, t := range triggers {
			l.handleTrigger(ctx, t)
		}
	}
}

func (l *Loop) handleTrigger(ctx context.Context, t rules.Trigger) {
	if l.Brain == nil {
		l.Actions.Run(ctx, l.State, &action.Action{
			Type:        "notify",
			ProjectPath: t.ProjectPath,
			Rationale:   t.Reason,
			Params:      map[string]any{"title": "Mentor: " + t.RuleName, "body": t.Reason},
		})
		return
	}
	in := l.buildPromptInput(t)
	d, err := l.Brain.Decide(ctx, in)
	if err != nil {
		log.Printf("brain: %v", err)
		l.Actions.Run(ctx, l.State, &action.Action{
			Type:      "notify",
			Rationale: "brain failed: " + err.Error(),
			Params:    map[string]any{"title": "Mentor (degraded)", "body": t.Reason},
		})
		return
	}
	if d.Decision == "ignore" || d.Decision == "" {
		return
	}
	l.Actions.Run(ctx, l.State, &action.Action{
		Type:        d.Decision,
		ProjectPath: t.ProjectPath,
		Rationale:   d.Rationale,
		Params:      d.Params,
	})
}

func (l *Loop) buildPromptInput(t rules.Trigger) DeciderInput {
	evJSON, _ := json.Marshal(t.Events)
	return DeciderInput{
		Now:               t.Now.UTC().Format(time.RFC3339),
		ProjectsJSON:      jsonProjects(l.State),
		PrefsJSON:         jsonPrefs(l.State),
		RecentActionsJSON: jsonRecentActions(l.State),
		TriggerReason:     t.RuleName + ": " + t.Reason,
		EventsJSON:        string(evJSON),
	}
}

func (l *Loop) BuildChatPromptInput(message string) DeciderInput {
	b, _ := json.Marshal(map[string]string{"user_message": message})
	return DeciderInput{
		Now:               time.Now().UTC().Format(time.RFC3339),
		ProjectsJSON:      jsonProjects(l.State),
		PrefsJSON:         jsonPrefs(l.State),
		RecentActionsJSON: jsonRecentActions(l.State),
		TriggerReason:     "user_chat",
		EventsJSON:        string(b),
	}
}

func jsonProjects(s *state.State) string {
	rows, err := s.DB().Query(`SELECT project_path, COALESCE(display_name, ''), last_active, COALESCE(inferred_focus, '') FROM projects WHERE paused_until IS NULL OR paused_until > datetime('now') ORDER BY last_active DESC LIMIT 30`)
	if err != nil {
		return "[]"
	}
	defer rows.Close()
	type p struct{ Path, Name, LastActive, Focus string }
	var out []p
	for rows.Next() {
		var x p
		rows.Scan(&x.Path, &x.Name, &x.LastActive, &x.Focus)
		out = append(out, x)
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func jsonPrefs(s *state.State) string {
	rows, err := s.DB().Query(`SELECT key, value FROM user_prefs`)
	if err != nil {
		return "{}"
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		out[k] = v
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func jsonRecentActions(s *state.State) string {
	rows, err := s.DB().Query(`SELECT timestamp, action_type, COALESCE(rationale, ''), COALESCE(result_summary, '') FROM actions WHERE timestamp > datetime('now', '-1 day') ORDER BY timestamp DESC LIMIT 20`)
	if err != nil {
		return "[]"
	}
	defer rows.Close()
	type a struct{ Ts, Type, Rationale, Result string }
	var out []a
	for rows.Next() {
		var x a
		rows.Scan(&x.Ts, &x.Type, &x.Rationale, &x.Result)
		out = append(out, x)
	}
	b, _ := json.Marshal(out)
	return string(b)
}
