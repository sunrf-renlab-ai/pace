package rules

import (
	"context"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/ingest"
	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

type R1ToolErrorBurst struct{}

func (*R1ToolErrorBurst) Name() string { return "R1.tool_error_burst" }

func (*R1ToolErrorBurst) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
	since := now.Add(-5 * time.Minute)
	evs, err := recentEvents(s, since)
	if err != nil {
		return nil, err
	}
	bySession := map[string]int{}
	bySessionEvents := map[string][]ingest.Event{}
	bySessionProject := map[string]string{}
	for _, ev := range evs {
		if ev.HookType == "PostToolUse" && ev.ToolExitStatus == "error" {
			bySession[ev.SessionID]++
			bySessionEvents[ev.SessionID] = append(bySessionEvents[ev.SessionID], ev)
			bySessionProject[ev.SessionID] = ev.ProjectPath
		}
	}
	var out []Trigger
	for sess, cnt := range bySession {
		if cnt >= 2 {
			out = append(out, Trigger{
				RuleName:    "R1.tool_error_burst",
				ProjectPath: bySessionProject[sess],
				Reason:      "2+ tool errors in same session within 5 min",
				Events:      bySessionEvents[sess],
				Now:         now,
			})
		}
	}
	return out, nil
}
