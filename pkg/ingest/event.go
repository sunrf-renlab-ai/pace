package ingest

import "time"

// Event is the wire format hook scripts POST to /event. v0.7: tool/prompt
// content fields hold FULL payloads, not summaries — there is no truncation
// in the hook script anymore. Brain decisions improve with full context.
type Event struct {
	EventID        string    `json:"event_id"`
	Timestamp      time.Time `json:"timestamp"`
	HookType       string    `json:"hook_type"`
	SessionID      string    `json:"session_id"`
	ProjectPath    string    `json:"project_path"`
	GitBranch      string    `json:"git_branch,omitempty"`
	GitHeadSHA     string    `json:"git_head_sha,omitempty"`
	ToolName       string    `json:"tool_name,omitempty"`
	ToolInput      string    `json:"tool_input,omitempty"`
	ToolResult     string    `json:"tool_result,omitempty"`
	ToolExitStatus string    `json:"tool_exit_status,omitempty"`
	UserPrompt     string    `json:"user_prompt,omitempty"`
	StopReason     string    `json:"stop_reason,omitempty"`
}

func (e *Event) Validate() error {
	if e.EventID == "" {
		return ErrMissing("event_id")
	}
	if e.HookType == "" {
		return ErrMissing("hook_type")
	}
	if e.SessionID == "" {
		return ErrMissing("session_id")
	}
	if e.ProjectPath == "" {
		return ErrMissing("project_path")
	}
	if e.Timestamp.IsZero() {
		return ErrMissing("timestamp")
	}
	return nil
}

type ErrMissing string

func (e ErrMissing) Error() string { return "missing field: " + string(e) }
