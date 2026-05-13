package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/pm"
	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

// GeneratePlanExec is the action the brain emits to write a plan markdown
// document to disk and persist it in the plans table. The brain provides:
//   params.scope        — "today" | "week" | "manual"
//   params.content_md   — full markdown body
//   params.title        — optional notification title
type GeneratePlanExec struct{}

func (GeneratePlanExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	scope, _ := a.Params["scope"].(string)
	content, _ := a.Params["content_md"].(string)
	if scope == "" {
		scope = "today"
	}
	if content == "" {
		return errInvalidParams
	}

	id, err := pm.SavePlan(s, pm.Plan{
		Scope:     scope,
		ContentMD: content,
		Rationale: a.Rationale,
		Source:    "rule:" + scope,
	})
	if err != nil {
		return err
	}

	// Also write to ~/.config/pace/plans/<date>-<scope>.md so the user can
	// open the file directly.
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "pace", "plans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fname := time.Now().UTC().Format("2006-01-02") + "-" + scope + ".md"
	path := filepath.Join(dir, fname)
	header := fmt.Sprintf("<!-- plan_id: %s -->\n<!-- generated: %s -->\n\n",
		id, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(header+content), 0o644); err != nil {
		return err
	}

	s.DB().Exec(`UPDATE actions SET result_summary=? WHERE action_id=?`,
		"plan saved: "+path, a.ActionID)
	return nil
}
