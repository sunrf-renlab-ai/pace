package ingest

import "time"

type Event struct {
	EventID           string    `json:"event_id"`
	Timestamp         time.Time `json:"timestamp"`
	HookType          string    `json:"hook_type"`
	SessionID         string    `json:"session_id"`
	ProjectPath       string    `json:"project_path"`
	GitBranch         string    `json:"git_branch,omitempty"`
	GitHeadSHA        string    `json:"git_head_sha,omitempty"`
	ToolName          string    `json:"tool_name,omitempty"`
	ToolInputSummary  string    `json:"tool_input_summary,omitempty"`
	ToolResultSummary string    `json:"tool_result_summary,omitempty"`
	ToolExitStatus    string    `json:"tool_exit_status,omitempty"`
	UserPromptSummary string    `json:"user_prompt_summary,omitempty"`
	StopReason        string    `json:"stop_reason,omitempty"`
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
