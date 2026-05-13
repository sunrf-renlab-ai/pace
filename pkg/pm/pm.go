// Package pm holds the v0.3 project-management layer: per-project goals,
// the user's declared focus, and generated plans. All persistence goes
// through *state.State; nothing here writes the DB schema (migrations live
// in pkg/state/migrations/).
package pm

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

// ─── Goals ──────────────────────────────────────────────────────────────

type Goal struct {
	ProjectPath string     `json:"project_path"`
	Description string     `json:"description"`
	Deadline    *time.Time `json:"deadline,omitempty"`
	Milestones  []Milestone `json:"milestones,omitempty"`
	SetAt       time.Time  `json:"set_at"`
	Source      string     `json:"source"`
}

type Milestone struct {
	Name string `json:"name"`
	Done bool   `json:"done"`
}

// SetGoal upserts a goal. source is "cli" or "chat".
func SetGoal(s *state.State, g Goal) error {
	if g.ProjectPath == "" || g.Description == "" {
		return errors.New("project_path and description required")
	}
	if g.SetAt.IsZero() {
		g.SetAt = time.Now().UTC()
	}
	if g.Source == "" {
		g.Source = "cli"
	}
	var msJSON sql.NullString
	if len(g.Milestones) > 0 {
		b, _ := json.Marshal(g.Milestones)
		msJSON = sql.NullString{String: string(b), Valid: true}
	}
	var deadline sql.NullTime
	if g.Deadline != nil {
		deadline = sql.NullTime{Time: g.Deadline.UTC(), Valid: true}
	}
	_, err := s.DB().Exec(`INSERT INTO goals (project_path, description, deadline, milestones_json, set_at, source)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_path) DO UPDATE SET
			description = excluded.description,
			deadline = excluded.deadline,
			milestones_json = excluded.milestones_json,
			set_at = excluded.set_at,
			source = excluded.source`,
		g.ProjectPath, g.Description, deadline, msJSON, g.SetAt, g.Source)
	return err
}

func GetGoal(s *state.State, projectPath string) (*Goal, error) {
	row := s.DB().QueryRow(`SELECT project_path, description, deadline, COALESCE(milestones_json, ''), set_at, source FROM goals WHERE project_path = ?`, projectPath)
	var g Goal
	var deadline sql.NullTime
	var msJSON string
	if err := row.Scan(&g.ProjectPath, &g.Description, &deadline, &msJSON, &g.SetAt, &g.Source); err != nil {
		return nil, err
	}
	if deadline.Valid {
		t := deadline.Time
		g.Deadline = &t
	}
	if msJSON != "" {
		_ = json.Unmarshal([]byte(msJSON), &g.Milestones)
	}
	return &g, nil
}

func ListGoals(s *state.State) ([]Goal, error) {
	rows, err := s.DB().Query(`SELECT project_path, description, deadline, COALESCE(milestones_json, ''), set_at, source FROM goals ORDER BY set_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Goal
	for rows.Next() {
		var g Goal
		var deadline sql.NullTime
		var msJSON string
		if err := rows.Scan(&g.ProjectPath, &g.Description, &deadline, &msJSON, &g.SetAt, &g.Source); err != nil {
			return nil, err
		}
		if deadline.Valid {
			t := deadline.Time
			g.Deadline = &t
		}
		if msJSON != "" {
			_ = json.Unmarshal([]byte(msJSON), &g.Milestones)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func DeleteGoal(s *state.State, projectPath string) error {
	_, err := s.DB().Exec(`DELETE FROM goals WHERE project_path = ?`, projectPath)
	return err
}

// ─── Focus (stored as user_prefs) ───────────────────────────────────────
//
// Keys:
//   focus.project_path
//   focus.reason
//   focus.until         (RFC3339, optional)
//   focus.set_at        (RFC3339)
//
// We keep focus in user_prefs (not its own table) so the brain prompt's
// existing PrefsJSON automatically surfaces it.

type Focus struct {
	ProjectPath string     `json:"project_path"`
	Reason      string     `json:"reason,omitempty"`
	Until       *time.Time `json:"until,omitempty"`
	SetAt       time.Time  `json:"set_at"`
}

func SetFocus(s *state.State, f Focus) error {
	if f.ProjectPath == "" {
		return errors.New("project_path required")
	}
	if f.SetAt.IsZero() {
		f.SetAt = time.Now().UTC()
	}
	now := time.Now().UTC()
	prefs := map[string]string{
		"focus.project_path": f.ProjectPath,
		"focus.reason":       f.Reason,
		"focus.set_at":       f.SetAt.UTC().Format(time.RFC3339),
	}
	if f.Until != nil {
		prefs["focus.until"] = f.Until.UTC().Format(time.RFC3339)
	} else {
		prefs["focus.until"] = ""
	}
	tx, err := s.DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range prefs {
		if v == "" {
			tx.Exec(`DELETE FROM user_prefs WHERE key = ?`, k)
			continue
		}
		_, err = tx.Exec(`INSERT INTO user_prefs (key, value, set_at, source) VALUES (?, ?, ?, 'pm')
			ON CONFLICT(key) DO UPDATE SET value=excluded.value, set_at=excluded.set_at, source=excluded.source`,
			k, v, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func GetFocus(s *state.State) (*Focus, error) {
	rows, err := s.DB().Query(`SELECT key, value FROM user_prefs WHERE key LIKE 'focus.%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		m[k] = v
	}
	if m["focus.project_path"] == "" {
		return nil, nil
	}
	f := &Focus{
		ProjectPath: m["focus.project_path"],
		Reason:      m["focus.reason"],
	}
	if v := m["focus.set_at"]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.SetAt = t
		}
	}
	if v := m["focus.until"]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = &t
		}
	}
	// Auto-expire focus past its until.
	if f.Until != nil && time.Now().After(*f.Until) {
		ClearFocus(s)
		return nil, nil
	}
	return f, nil
}

func ClearFocus(s *state.State) error {
	_, err := s.DB().Exec(`DELETE FROM user_prefs WHERE key LIKE 'focus.%'`)
	return err
}

// ─── Plans ──────────────────────────────────────────────────────────────

type Plan struct {
	PlanID      string    `json:"plan_id"`
	GeneratedAt time.Time `json:"generated_at"`
	Scope       string    `json:"scope"` // "today" | "week" | "manual"
	ContentMD   string    `json:"content_md"`
	Rationale   string    `json:"rationale,omitempty"`
	Source      string    `json:"source"`
}

func SavePlan(s *state.State, p Plan) (string, error) {
	if p.PlanID == "" {
		p.PlanID = uuid.New().String()
	}
	if p.GeneratedAt.IsZero() {
		p.GeneratedAt = time.Now().UTC()
	}
	if p.Scope == "" {
		p.Scope = "today"
	}
	if p.Source == "" {
		p.Source = "cli"
	}
	_, err := s.DB().Exec(`INSERT INTO plans (plan_id, generated_at, scope, content_md, rationale, source) VALUES (?, ?, ?, ?, ?, ?)`,
		p.PlanID, p.GeneratedAt, p.Scope, p.ContentMD, p.Rationale, p.Source)
	return p.PlanID, err
}

// LatestPlan returns the most recent plan of the given scope (or any if scope is "").
func LatestPlan(s *state.State, scope string) (*Plan, error) {
	var (
		row *sql.Row
	)
	if scope == "" {
		row = s.DB().QueryRow(`SELECT plan_id, generated_at, scope, content_md, COALESCE(rationale, ''), source FROM plans ORDER BY generated_at DESC LIMIT 1`)
	} else {
		row = s.DB().QueryRow(`SELECT plan_id, generated_at, scope, content_md, COALESCE(rationale, ''), source FROM plans WHERE scope = ? ORDER BY generated_at DESC LIMIT 1`, scope)
	}
	var p Plan
	if err := row.Scan(&p.PlanID, &p.GeneratedAt, &p.Scope, &p.ContentMD, &p.Rationale, &p.Source); err != nil {
		return nil, err
	}
	return &p, nil
}

// LatestPlanToday returns the most recent "today"-scope plan generated in the
// last 24h, or nil if none.
func LatestPlanToday(s *state.State) (*Plan, error) {
	row := s.DB().QueryRow(`SELECT plan_id, generated_at, scope, content_md, COALESCE(rationale, ''), source FROM plans WHERE scope='today' AND generated_at > datetime('now', '-24 hours') ORDER BY generated_at DESC LIMIT 1`)
	var p Plan
	if err := row.Scan(&p.PlanID, &p.GeneratedAt, &p.Scope, &p.ContentMD, &p.Rationale, &p.Source); err != nil {
		return nil, err
	}
	return &p, nil
}

// PlansJSON returns recent plans as JSON for the brain prompt.
func PlansJSON(s *state.State, limit int) string {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.DB().Query(`SELECT scope, generated_at, content_md FROM plans ORDER BY generated_at DESC LIMIT ?`, limit)
	if err != nil {
		return "[]"
	}
	defer rows.Close()
	type item struct{ Scope, At, Body string }
	var out []item
	for rows.Next() {
		var x item
		rows.Scan(&x.Scope, &x.At, &x.Body)
		out = append(out, x)
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// GoalsJSON for the brain prompt.
func GoalsJSON(s *state.State) string {
	gs, err := ListGoals(s)
	if err != nil {
		return "[]"
	}
	b, _ := json.Marshal(gs)
	return string(b)
}

// FocusJSON for the brain prompt.
func FocusJSON(s *state.State) string {
	f, err := GetFocus(s)
	if err != nil || f == nil {
		return "null"
	}
	b, _ := json.Marshal(f)
	return string(b)
}

// ParseDeadline accepts "2026-06-13" or full RFC3339; returns nil for empty.
func ParseDeadline(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	if len(s) == 10 {
		// YYYY-MM-DD → end-of-day UTC
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return nil, err
		}
		t = t.Add(24*time.Hour - time.Second).UTC()
		return &t, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, fmt.Errorf("deadline must be YYYY-MM-DD or RFC3339: %w", err)
	}
	return &t, nil
}
