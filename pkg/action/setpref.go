package action

import (
	"context"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type SetPrefExec struct{}

func (SetPrefExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	key, _ := a.Params["key"].(string)
	value, _ := a.Params["value"].(string)
	if key == "" {
		return errInvalidParams
	}
	var prev string
	s.DB().QueryRow(`SELECT value FROM user_prefs WHERE key=?`, key).Scan(&prev)
	s.DB().Exec(`UPDATE actions SET undo_payload=? WHERE action_id=?`, key+"="+prev, a.ActionID)
	_, err := s.DB().Exec(`INSERT INTO user_prefs (key, value, set_at, source) VALUES (?, ?, ?, 'chat')
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, set_at=excluded.set_at, source=excluded.source`,
		key, value, time.Now().UTC())
	return err
}
