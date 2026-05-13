package rules

import (
	"context"
	"regexp"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/ingest"
	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

var testCmdPattern = regexp.MustCompile(`(?i)\b(go test|npm test|bun test|pnpm test|yarn test|pytest|jest|vitest|cargo test|mix test)\b`)

type R2TestFail struct{}

func (*R2TestFail) Name() string { return "R2.test_fail" }

func (*R2TestFail) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
	since := now.Add(-2 * time.Minute)
	evs, err := recentEvents(s, since)
	if err != nil {
		return nil, err
	}
	var out []Trigger
	for _, ev := range evs {
		if ev.HookType != "PostToolUse" || ev.ToolExitStatus != "error" {
			continue
		}
		if testCmdPattern.MatchString(ev.ToolInputSummary) {
			out = append(out, Trigger{
				RuleName:    "R2.test_fail",
				ProjectPath: ev.ProjectPath,
				Reason:      "test command failed",
				Events:      []ingest.Event{ev},
				Now:         now,
			})
		}
	}
	return out, nil
}
