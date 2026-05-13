package rules

import (
	"context"
	"regexp"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/ingest"
	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

var deployCmdPattern = regexp.MustCompile(`(?i)\b(vercel deploy|render deploys|fly deploy|netlify deploy|gh workflow run|kubectl apply|terraform apply)\b`)

type R3DeployFail struct{}

func (*R3DeployFail) Name() string { return "R3.deploy_fail" }

func (*R3DeployFail) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
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
		if deployCmdPattern.MatchString(ev.ToolInputSummary) {
			out = append(out, Trigger{
				RuleName:    "R3.deploy_fail",
				ProjectPath: ev.ProjectPath,
				Reason:      "deploy command failed",
				Events:      []ingest.Event{ev},
				Now:         now,
			})
		}
	}
	return out, nil
}
