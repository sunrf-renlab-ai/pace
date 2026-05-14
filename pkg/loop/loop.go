// Package loop is the brain coordinator. v0.6 is event-driven with debouncing:
//
//   - Hook events arriving at the daemon call Loop.Notify(), which signals
//     a debouncer goroutine.
//   - Debouncer waits for QuietDebounce of silence after the latest signal,
//     OR up to MaxWait since the first signal in a window — whichever first.
//   - When the window closes, brain runs Once on every event since the
//     previous Once.
//   - In parallel, a much-longer Strategic ticker (default 30 min) fires
//     Once regardless of events, so brain can do periodic reflection (morning
//     plan generation, focus drift across hours, etc.) even on quiet days.
//
// Manual triggers (chat, ask, review, consult) bypass both paths — they call
// Brain.Decide synchronously with their own TriggerReason.
package loop

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/action"
	"github.com/sunrf-renlab-ai/pace/pkg/mentor"
	"github.com/sunrf-renlab-ai/pace/pkg/pm"
	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

// Decider abstracts the LLM brain. nil = no LLM (loop becomes a no-op).
type Decider interface {
	Decide(ctx context.Context, in DeciderInput) (*Decision, error)
}

type DeciderInput struct {
	Now               string
	ProjectsJSON      string
	GoalsJSON         string
	FocusJSON         string
	PlansJSON         string
	OpinionsJSON      string
	PrefsJSON         string
	RecentActionsJSON string
	TriggerReason     string
	EventsJSON        string
}

// Decision is what brain emits. For multi-action ticks brain emits
// Decision{Decision:"batch"} and the actual list lives in Params["actions"]
// as a slice of {decision, rationale, params} maps.
//
// v0.8 adds Usage and ToolsUsed — populated by the brain after running its
// claude subprocess so the daemon can audit-log what brain actually saw.
type Decision struct {
	Decision  string         `json:"decision"`
	Rationale string         `json:"rationale"`
	Params    map[string]any `json:"params"`

	// Telemetry from the brain run (not part of the wire JSON the LLM emits).
	Usage     *TokenUsage `json:"-"`
	ToolsUsed []string    `json:"-"`
}

// TokenUsage is per-call token telemetry summed across all assistant messages.
type TokenUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

type Loop struct {
	State   *state.State
	Brain   Decider
	Actions *action.Registry

	// QuietDebounce is how long the debouncer waits for "silence" after the
	// latest event signal before firing brain. Default 5s.
	QuietDebounce time.Duration
	// MaxWait caps the total time the debouncer will wait from the first
	// signal in a window before forcing a brain run, even if events keep
	// arriving. Default 30s.
	MaxWait time.Duration
	// Strategic is the long-interval safety tick that fires brain regardless
	// of events. Default 30 min.
	Strategic time.Duration

	mu       sync.Mutex
	lastTick time.Time

	notifyCh chan struct{}
	stop     chan struct{}
}

// New constructs an event-driven loop. The lastTick baseline is set to
// startup so the first run only sees events ingested AFTER pace started.
func New(s *state.State, b Decider, ar *action.Registry) *Loop {
	return &Loop{
		State:         s,
		Brain:         b,
		Actions:       ar,
		QuietDebounce: 5 * time.Second,
		MaxWait:       30 * time.Second,
		Strategic:     30 * time.Minute,
		lastTick:      time.Now().UTC(),
		// Buffered 1 so a signal sent while the consumer is mid-run isn't lost
		// (Notify is non-blocking; if the buffer is full we know there's
		// already a pending signal — coalesce it).
		notifyCh: make(chan struct{}, 1),
		stop:     make(chan struct{}),
	}
}

// Notify signals that one or more new events have been ingested. Non-blocking;
// safe to call from the HTTP ingest handler. Multiple notifies during a single
// debounce window collapse to one brain run.
func (l *Loop) Notify() {
	select {
	case l.notifyCh <- struct{}{}:
	default:
		// Already queued; the consumer will pick up everything it needs from SQLite.
	}
}

func (l *Loop) Start(ctx context.Context) {
	go l.runDebouncer(ctx)
	go l.runStrategicTick(ctx)
}

// runDebouncer is the primary path: react to events, but coalesce bursts.
func (l *Loop) runDebouncer(ctx context.Context) {
	for {
		// Block until either the first signal of a new window arrives, or stop.
		select {
		case <-ctx.Done():
			return
		case <-l.stop:
			return
		case <-l.notifyCh:
		}

		// Window opened. Track:
		//   quietTimer — resets every time another signal arrives
		//   maxTimer   — fixed deadline from window start
		windowStart := time.Now()
		quietTimer := time.NewTimer(l.QuietDebounce)
		maxTimer := time.NewTimer(l.MaxWait)

	wait:
		for {
			select {
			case <-ctx.Done():
				quietTimer.Stop()
				maxTimer.Stop()
				return
			case <-l.stop:
				quietTimer.Stop()
				maxTimer.Stop()
				return
			case <-l.notifyCh:
				if !quietTimer.Stop() {
					select {
					case <-quietTimer.C:
					default:
					}
				}
				quietTimer.Reset(l.QuietDebounce)
			case <-quietTimer.C:
				maxTimer.Stop()
				break wait
			case <-maxTimer.C:
				quietTimer.Stop()
				log.Printf("loop: max-wait %v elapsed since first event in window, firing brain", time.Since(windowStart))
				break wait
			}
		}

		l.Once(ctx, time.Now())
	}
}

// runStrategicTick is the long-interval safety net so brain reflects even if
// no events flow (morning standup, deadline approaching, idle drift detection).
func (l *Loop) runStrategicTick(ctx context.Context) {
	t := time.NewTicker(l.Strategic)
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
}

func (l *Loop) Stop() {
	select {
	case <-l.stop:
	default:
		close(l.stop)
	}
}

// Once runs one tick: pull events since lastTick, ask brain, execute decisions.
func (l *Loop) Once(ctx context.Context, now time.Time) {
	now = now.UTC()
	l.mu.Lock()
	since := l.lastTick
	l.lastTick = now
	l.mu.Unlock()

	if l.Brain == nil {
		return // no brain → no-op (no rule fallback in v0.5)
	}

	events := l.eventsSince(since)
	in := l.buildTickInput(now, since, events)
	d, err := l.Brain.Decide(ctx, in)
	if err != nil {
		log.Printf("brain tick: %v", err)
		return
	}
	l.executeDecision(ctx, d, "")
}

// executeDecision runs a single brain Decision. If it's "batch", expand into
// sub-actions and run each. Project hint is the project_path to associate
// with actions that don't carry their own.
func (l *Loop) executeDecision(ctx context.Context, d *Decision, projectHint string) {
	if d == nil {
		return
	}
	switch d.Decision {
	case "", "ignore":
		return
	case "batch":
		raw, _ := d.Params["actions"].([]any)
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			sub := &Decision{
				Decision:  asStr(m["decision"]),
				Rationale: asStr(m["rationale"]),
				Params:    asMap(m["params"]),
			}
			l.executeDecision(ctx, sub, projectHint)
		}
	default:
		project := projectHint
		if p, ok := d.Params["project_path"].(string); ok && p != "" {
			project = p
		}
		l.Actions.Run(ctx, l.State, &action.Action{
			Type:        d.Decision,
			ProjectPath: project,
			Rationale:   d.Rationale,
			Params:      d.Params,
		})
	}
}

func asStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func (l *Loop) eventsSince(since time.Time) []map[string]any {
	rows, err := l.State.DB().Query(`SELECT payload_json FROM events WHERE timestamp > ? ORDER BY timestamp ASC LIMIT 200`, since.UTC())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var pj string
		if err := rows.Scan(&pj); err != nil {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(pj), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

func (l *Loop) buildTickInput(now, since time.Time, events []map[string]any) DeciderInput {
	body := map[string]any{
		"window_start": since.Format(time.RFC3339),
		"window_end":   now.Format(time.RFC3339),
		"event_count":  len(events),
		"events":       events,
	}
	evJSON, _ := json.Marshal(body)
	// TriggerReason hints brain about why it woke up: "events" if there are
	// new events (debouncer path), "strategic" if not (long-interval safety
	// tick — brain should consider time-of-day plan generation, drift checks).
	reason := "events"
	if len(events) == 0 {
		reason = "strategic"
	}
	return DeciderInput{
		Now:               now.Format(time.RFC3339),
		ProjectsJSON:      jsonProjects(l.State),
		GoalsJSON:         pm.GoalsJSON(l.State),
		FocusJSON:         pm.FocusJSON(l.State),
		PlansJSON:         pm.PlansJSON(l.State, 5),
		OpinionsJSON:      mentor.OpinionsJSON(l.State, 20),
		PrefsJSON:         jsonPrefs(l.State),
		RecentActionsJSON: jsonRecentActions(l.State),
		TriggerReason:     reason,
		EventsJSON:        string(evJSON),
	}
}

// BuildChatPromptInput is used by daemon IPC handlers (chat, ask, review,
// consult) to invoke brain synchronously outside the tick loop. The
// TriggerReason is set by the caller (e.g. "user_chat", "cli:ask", "cli:review").
func (l *Loop) BuildChatPromptInput(message string) DeciderInput {
	b, _ := json.Marshal(map[string]string{"user_message": message})
	return DeciderInput{
		Now:               time.Now().UTC().Format(time.RFC3339),
		ProjectsJSON:      jsonProjects(l.State),
		GoalsJSON:         pm.GoalsJSON(l.State),
		FocusJSON:         pm.FocusJSON(l.State),
		PlansJSON:         pm.PlansJSON(l.State, 5),
		OpinionsJSON:      mentor.OpinionsJSON(l.State, 20),
		PrefsJSON:         jsonPrefs(l.State),
		RecentActionsJSON: jsonRecentActions(l.State),
		TriggerReason:     "user_chat",
		EventsJSON:        string(b),
	}
}

// ─── helpers exposed to daemon ──────────────────────────────────────────

// ExecuteDecision runs a brain Decision through the action registry. Daemon
// uses this from chat/ask/review/consult handlers (which call brain
// synchronously, then need to expand any batch).
func (l *Loop) ExecuteDecision(ctx context.Context, d *Decision, projectHint string) {
	l.executeDecision(ctx, d, projectHint)
}

// ─── state-to-JSON helpers ──────────────────────────────────────────────

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
	if out == nil {
		return "[]"
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
	if out == nil {
		return "[]"
	}
	b, _ := json.Marshal(out)
	return string(b)
}
