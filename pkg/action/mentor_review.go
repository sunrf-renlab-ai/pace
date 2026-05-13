package action

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sunrf-renlab-ai/pace/pkg/mentor"
	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

// MentorReviewExec turns a brain decision of "mentor_review" into a durable
// Opinion record. The brain is expected to fill params with:
//
//   params.opinions = [
//     {
//       "topic":          "<short headline>",
//       "observation":    "<what was seen, with evidence>",
//       "concern":        "<why it matters>",
//       "recommendation": "<what to do>",
//       "confidence":     "high|medium|low",
//       "evidence":       ["abc123", "pkg/foo/bar.go:42", ...]
//     },
//     ...
//   ]
//
// Each opinion is saved separately so the user can ack/dismiss individually.
// A single notification is fired summarizing the count + top headline.
type MentorReviewExec struct {
	N        Notifier
	Notifier Notifier // alias kept for legacy
}

func (e MentorReviewExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	notifier := e.N
	if notifier == nil {
		notifier = e.Notifier
	}

	rawOps, ok := a.Params["opinions"].([]any)
	if !ok || len(rawOps) == 0 {
		// Brain returned mentor_review with no opinions — treat as a noop
		// rather than error (sometimes the adversarial pass kills all
		// concerns, which is the correct outcome).
		s.DB().Exec(`UPDATE actions SET result_summary=? WHERE action_id=?`,
			"no opinions survived adversarial review", a.ActionID)
		return nil
	}

	saved := 0
	var topics []string
	for _, raw := range rawOps {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		op := mentor.Opinion{
			Trigger:        firstNonEmpty(strFrom(m, "trigger"), "rule"),
			ProjectPath:    a.ProjectPath,
			Topic:          strFrom(m, "topic"),
			Observation:    strFrom(m, "observation"),
			Concern:        strFrom(m, "concern"),
			Recommendation: strFrom(m, "recommendation"),
			Confidence:     mentor.Confidence(firstNonEmpty(strFrom(m, "confidence"), "medium")),
			Evidence:       stringsFrom(m, "evidence"),
		}
		if op.Topic == "" || op.Observation == "" {
			continue
		}
		if _, err := mentor.Save(s, op); err != nil {
			continue
		}
		saved++
		topics = append(topics, op.Topic)
	}

	if saved == 0 {
		return fmt.Errorf("mentor_review: no valid opinions in params")
	}

	if notifier != nil {
		title := fmt.Sprintf("Pace mentor: %d opinion%s", saved, pluralize(saved))
		body := strings.Join(topics, " • ")
		if len(body) > 140 {
			body = body[:140] + "…"
		}
		body += " — `pace mentor` to review"
		_ = notifier.Notify(title, body)
	}

	s.DB().Exec(`UPDATE actions SET result_summary=? WHERE action_id=?`,
		fmt.Sprintf("%d opinion(s) saved", saved), a.ActionID)
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func strFrom(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func stringsFrom(m map[string]any, k string) []string {
	raw, ok := m[k].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if s, ok := r.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func pluralize(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// jsonEvidence is exported helper for callers that need to inspect raw evidence
// from the actions table. Not used by Execute.
func jsonEvidence(o mentor.Opinion) string {
	b, _ := json.Marshal(o.Evidence)
	return string(b)
}
