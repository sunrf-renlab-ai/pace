package action

import (
	"context"
	"os/exec"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type SpawnExec struct {
	ClaudePath string
	AuthEnv    map[string]string
}

func (e *SpawnExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	project, _ := a.Params["project_path"].(string)
	prompt, _ := a.Params["prompt"].(string)
	if project == "" || prompt == "" {
		return errInvalidParams
	}
	cmd := exec.CommandContext(ctx, e.ClaudePath, "-p", prompt, "--add-dir", project)
	if e.AuthEnv != nil {
		cmd.Env = mergeEnv(e.AuthEnv)
	}
	out, err := cmd.CombinedOutput()
	s.DB().Exec(`UPDATE actions SET result_summary=? WHERE action_id=?`, truncate(string(out), 1000), a.ActionID)
	return err
}
