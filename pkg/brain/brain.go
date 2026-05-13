package brain

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/loop"
)

//go:embed prompt.tmpl
var promptTmpl string

// Brain spawns `claude -p` to get a Decision JSON for a given input.
// It implements loop.Decider.
type Brain struct {
	ClaudePath string            // path to `claude` binary; default "claude" (found via PATH)
	ExtraEnv   map[string]string // additional env vars (typically auth from oauth.LoadAuthEnv)
	Timeout    time.Duration     // per-call timeout; default 60s
}

func New(claudePath string, extra map[string]string) *Brain {
	if claudePath == "" {
		claudePath = "claude"
	}
	return &Brain{ClaudePath: claudePath, ExtraEnv: extra, Timeout: 60 * time.Second}
}

func (b *Brain) Decide(ctx context.Context, in loop.DeciderInput) (*loop.Decision, error) {
	tmpl, err := template.New("p").Parse(promptTmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, in); err != nil {
		return nil, err
	}
	if b.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, b.ClaudePath, "-p", buf.String(), "--output-format", "json")
	cmd.Env = mergedEnv(b.ExtraEnv)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude exec: %w", err)
	}
	return parseDecision(out)
}

func parseDecision(out []byte) (*loop.Decision, error) {
	// claude -p --output-format json wraps the assistant content in an envelope:
	//   {"result": "<assistant string>", "session_id": "...", "type":"result", ...}
	// We try the envelope first; fall back to raw JSON if it's not wrapped.
	var envelope struct {
		Result string `json:"result"`
	}
	raw := out
	if err := json.Unmarshal(out, &envelope); err == nil && envelope.Result != "" {
		raw = []byte(extractJSONObject(envelope.Result))
	}
	var d loop.Decision
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("parse decision: %w; raw output: %s", err, truncate(string(out), 500))
	}
	if d.Decision == "" {
		return nil, fmt.Errorf("decision missing in output: %s", truncate(string(out), 500))
	}
	return &d, nil
}

// extractJSONObject finds the first { and the last } to recover JSON wrapped in
// markdown fences or surrounded by prose. Lenient on purpose.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end <= start {
		return s
	}
	return s[start : end+1]
}

func mergedEnv(extra map[string]string) []string {
	env := append([]string{}, os.Environ()...)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
