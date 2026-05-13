package action

import (
	"context"
	"database/sql"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type PauseProjectExec struct{}

func (PauseProjectExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	project, _ := a.Params["project_path"].(string)
	untilStr, _ := a.Params["until"].(string)
	if project == "" {
		return errInvalidParams
	}
	var until time.Time
	if untilStr != "" {
		t, err := time.Parse(time.RFC3339, untilStr)
		if err != nil {
			return err
		}
		until = t
	} else {
		until = time.Now().AddDate(100, 0, 0)
	}

	var prev sql.NullTime
	s.DB().QueryRow(`SELECT paused_until FROM projects WHERE project_path=?`, project).Scan(&prev)
	prevStr := ""
	if prev.Valid {
		prevStr = prev.Time.Format(time.RFC3339)
	}
	s.DB().Exec(`UPDATE actions SET undo_payload=? WHERE action_id=?`, project+"|"+prevStr, a.ActionID)
	// Use upsert in case the project hasn't been seen yet.
	now := time.Now().UTC()
	_, err := s.DB().Exec(`INSERT INTO projects (project_path, display_name, first_seen, last_active, paused_until) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(project_path) DO UPDATE SET paused_until=excluded.paused_until`,
		project, lastSegment(project), now, now, until)
	return err
}

func lastSegment(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
