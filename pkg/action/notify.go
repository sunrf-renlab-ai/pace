package action

import (
	"context"
	"fmt"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type notifyExec struct{ n Notifier }

func (e *notifyExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	title, _ := a.Params["title"].(string)
	body, _ := a.Params["body"].(string)
	if title == "" {
		title = "Mentor"
	}
	if body == "" {
		body = a.Rationale
	}
	if err := e.n.Notify(title, body); err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	return nil
}
