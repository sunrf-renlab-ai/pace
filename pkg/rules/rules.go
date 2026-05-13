package rules

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/ingest"
	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

type Trigger struct {
	RuleName    string
	ProjectPath string
	Reason      string
	Events      []ingest.Event
	Now         time.Time
}

type Rule interface {
	Name() string
	Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error)
}

func All() []Rule {
	return []Rule{
		&R1ToolErrorBurst{},
		&R2TestFail{},
		&R3DeployFail{},
		&R8PeriodicOverview{},
	}
}

func recentEvents(s *state.State, since time.Time) ([]ingest.Event, error) {
	rows, err := s.DB().Query(`SELECT payload_json FROM events WHERE timestamp >= ? ORDER BY timestamp ASC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ingest.Event
	for rows.Next() {
		var pj string
		if err := rows.Scan(&pj); err != nil {
			return nil, err
		}
		var ev ingest.Event
		if err := json.Unmarshal([]byte(pj), &ev); err == nil {
			out = append(out, ev)
		}
	}
	return out, rows.Err()
}
