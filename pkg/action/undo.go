package action

import (
	"context"
	"database/sql"
	"strings"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

// UndoLast reverses the last N done actions, newest first.
// Returns count actually reverted.
func UndoLast(ctx context.Context, s *state.State, n int) (int, error) {
	rows, err := s.DB().Query(`SELECT action_id, action_type, COALESCE(undo_payload, '') FROM actions WHERE status='done' ORDER BY timestamp DESC LIMIT ?`, n)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type row struct{ id, typ, payload string }
	var rs []row
	for rows.Next() {
		var r row
		rows.Scan(&r.id, &r.typ, &r.payload)
		rs = append(rs, r)
	}
	rows.Close()

	count := 0
	for _, r := range rs {
		switch r.typ {
		case "set_pref":
			parts := strings.SplitN(r.payload, "=", 2)
			if len(parts) == 2 {
				if parts[1] == "" {
					s.DB().Exec(`DELETE FROM user_prefs WHERE key=?`, parts[0])
				} else {
					s.DB().Exec(`UPDATE user_prefs SET value=? WHERE key=?`, parts[1], parts[0])
				}
			}
		case "pause_project":
			parts := strings.SplitN(r.payload, "|", 2)
			if len(parts) == 2 {
				if parts[1] == "" {
					s.DB().Exec(`UPDATE projects SET paused_until=NULL WHERE project_path=?`, parts[0])
				} else {
					var v sql.NullTime
					if err := v.Scan(parts[1]); err == nil {
						s.DB().Exec(`UPDATE projects SET paused_until=? WHERE project_path=?`, v.Time, parts[0])
					} else {
						s.DB().Exec(`UPDATE projects SET paused_until=NULL WHERE project_path=?`, parts[0])
					}
				}
			}
		case "sync_files", "notify", "spawn_session":
			// passive or non-trivially-undoable; we still mark undone.
		}
		s.DB().Exec(`UPDATE actions SET status='undone' WHERE action_id=?`, r.id)
		count++
	}
	return count, nil
}
