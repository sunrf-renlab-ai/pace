package action

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

type SyncFilesExec struct{}

func (SyncFilesExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	topic, _ := a.Params["note_topic"].(string)
	body, _ := a.Params["body"].(string)
	if topic == "" || body == "" {
		return errInvalidParams
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "pace", "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, topic+".md")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := "\n## " + time.Now().UTC().Format(time.RFC3339) + "\n\n"
	_, err = f.WriteString(prefix + body + "\n")
	if err == nil {
		s.DB().Exec(`UPDATE actions SET undo_payload=? WHERE action_id=?`, path+"|"+prefix+body+"\n", a.ActionID)
	}
	return err
}
