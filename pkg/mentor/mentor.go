// Package mentor holds the v0.4 mentor layer: structured opinions produced
// by the brain in mentor mode (two-pass adversarial review). Opinions are
// the durable record of "what the senior engineer noticed and recommended."
package mentor

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

type Status string

const (
	StatusOpen         Status = "open"
	StatusAcknowledged Status = "acknowledged"
	StatusDismissed    Status = "dismissed"
)

type Opinion struct {
	OpinionID      string     `json:"opinion_id"`
	CreatedAt      time.Time  `json:"created_at"`
	Trigger        string     `json:"trigger"`
	ProjectPath    string     `json:"project_path,omitempty"`
	Topic          string     `json:"topic"`
	Observation    string     `json:"observation"`
	Concern        string     `json:"concern,omitempty"`
	Recommendation string     `json:"recommendation,omitempty"`
	Confidence     Confidence `json:"confidence"`
	Evidence       []string   `json:"evidence,omitempty"`
	Status         Status     `json:"status"`
}

func validate(o Opinion) error {
	if o.Topic == "" {
		return errors.New("topic required")
	}
	if o.Observation == "" {
		return errors.New("observation required")
	}
	if o.Trigger == "" {
		return errors.New("trigger required")
	}
	switch o.Confidence {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
	case "":
		// default to medium if unspecified
	default:
		return fmt.Errorf("invalid confidence: %s", o.Confidence)
	}
	return nil
}

func Save(s *state.State, o Opinion) (string, error) {
	if err := validate(o); err != nil {
		return "", err
	}
	if o.OpinionID == "" {
		o.OpinionID = uuid.New().String()
	}
	if o.CreatedAt.IsZero() {
		o.CreatedAt = time.Now().UTC()
	}
	if o.Confidence == "" {
		o.Confidence = ConfidenceMedium
	}
	if o.Status == "" {
		o.Status = StatusOpen
	}
	var evJSON sql.NullString
	if len(o.Evidence) > 0 {
		b, _ := json.Marshal(o.Evidence)
		evJSON = sql.NullString{String: string(b), Valid: true}
	}
	_, err := s.DB().Exec(`INSERT INTO mentor_opinions
		(opinion_id, created_at, trigger, project_path, topic, observation, concern, recommendation, confidence, evidence_json, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.OpinionID, o.CreatedAt, o.Trigger, o.ProjectPath, o.Topic, o.Observation, o.Concern, o.Recommendation, string(o.Confidence), evJSON, string(o.Status))
	return o.OpinionID, err
}

func Get(s *state.State, id string) (*Opinion, error) {
	row := s.DB().QueryRow(`SELECT opinion_id, created_at, trigger, COALESCE(project_path, ''), topic, observation, COALESCE(concern, ''), COALESCE(recommendation, ''), confidence, COALESCE(evidence_json, ''), status FROM mentor_opinions WHERE opinion_id = ?`, id)
	return scanOpinion(row.Scan)
}

func ListOpen(s *state.State, limit int) ([]Opinion, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB().Query(`SELECT opinion_id, created_at, trigger, COALESCE(project_path, ''), topic, observation, COALESCE(concern, ''), COALESCE(recommendation, ''), confidence, COALESCE(evidence_json, ''), status FROM mentor_opinions WHERE status = 'open' ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOpinions(rows)
}

func ListAll(s *state.State, limit int) ([]Opinion, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB().Query(`SELECT opinion_id, created_at, trigger, COALESCE(project_path, ''), topic, observation, COALESCE(concern, ''), COALESCE(recommendation, ''), confidence, COALESCE(evidence_json, ''), status FROM mentor_opinions ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOpinions(rows)
}

func Acknowledge(s *state.State, id string) error {
	_, err := s.DB().Exec(`UPDATE mentor_opinions SET status = ? WHERE opinion_id = ?`, string(StatusAcknowledged), id)
	return err
}

func Dismiss(s *state.State, id string) error {
	_, err := s.DB().Exec(`UPDATE mentor_opinions SET status = ? WHERE opinion_id = ?`, string(StatusDismissed), id)
	return err
}

// OpinionsJSON returns recent OPEN opinions for the brain prompt — so the
// brain knows what it's already said and doesn't repeat itself.
func OpinionsJSON(s *state.State, limit int) string {
	ops, err := ListOpen(s, limit)
	if err != nil || ops == nil {
		return "[]"
	}
	b, _ := json.Marshal(ops)
	return string(b)
}

// ─── helpers ────────────────────────────────────────────────────────────

type scanFn func(dest ...any) error

func scanOpinion(fn scanFn) (*Opinion, error) {
	var o Opinion
	var conf, statusStr, evJSON string
	if err := fn(&o.OpinionID, &o.CreatedAt, &o.Trigger, &o.ProjectPath, &o.Topic, &o.Observation, &o.Concern, &o.Recommendation, &conf, &evJSON, &statusStr); err != nil {
		return nil, err
	}
	o.Confidence = Confidence(conf)
	o.Status = Status(statusStr)
	if evJSON != "" {
		_ = json.Unmarshal([]byte(evJSON), &o.Evidence)
	}
	return &o, nil
}

func scanOpinions(rows *sql.Rows) ([]Opinion, error) {
	var out []Opinion
	for rows.Next() {
		o, err := scanOpinion(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}
