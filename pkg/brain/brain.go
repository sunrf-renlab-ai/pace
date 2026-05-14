// Package brain runs the LLM via `claude` CLI subprocess.
//
// v0.8: switched from one-shot `-p "<prompt>" --output-format json` to
// bidirectional streaming `-p --input-format stream-json --output-format
// stream-json --verbose --permission-mode bypassPermissions`. This is the
// same protocol Multica uses (see /Users/blink/project/multica/server/pkg/
// agent/claude.go).
//
// Why bidirectional streaming:
//   - Brain can use Claude's built-in tools (Read, Glob, Bash) when given
//     `--add-dir <path>` — required for `pace review` / `pace consult` to
//     actually read code.
//   - Token usage is reported per-call and stored in Decision.Usage.
//   - Tool invocations are observable; we record names in Decision.ToolsUsed
//     so the daemon's audit trail shows what brain actually inspected.
//   - control_request from claude (asking permission for a tool) is
//     auto-approved by writing back a control_response — daemon mode is
//     fully autonomous.
package brain

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/loop"
)

//go:embed prompt.tmpl
var promptTmpl string

type Brain struct {
	ClaudePath string
	ExtraEnv   map[string]string
	Timeout    time.Duration
}

func New(claudePath string, extra map[string]string) *Brain {
	if claudePath == "" {
		claudePath = "claude"
	}
	return &Brain{ClaudePath: claudePath, ExtraEnv: extra, Timeout: 5 * time.Minute}
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
	prompt := buf.String()

	if b.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.Timeout)
		defer cancel()
	}

	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
	}
	cmd := exec.CommandContext(ctx, b.ClaudePath, args...)
	cmd.Env = mergedEnv(b.ExtraEnv)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude start: %w", err)
	}

	// Send the prompt as a single user message (NDJSON), then close stdin so
	// claude knows there will be no follow-up. control_responses can still be
	// written via a separate stream — but our protocol is "one user message,
	// brain runs to completion" so we don't need a persistent writer.
	//
	// Note: Multica keeps stdin open for control_response auto-approve. We do
	// the same here with --permission-mode=bypassPermissions baked into the
	// flags, which makes claude skip the control_request roundtrip entirely
	// for built-in tools. So closing stdin after the user message is safe.
	if err := writeUserMessage(stdin, prompt); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return nil, fmt.Errorf("write user message: %w (stderr: %s)", err, truncate(stderrBuf.String(), 500))
	}
	_ = stdin.Close()

	d, err := parseStream(stdout)
	// Close stdout to send SIGPIPE to claude on its next write — necessary
	// because --input-format stream-json keeps claude alive waiting for more
	// user messages even after emitting the final result envelope.
	_ = stdout.Close()
	// Kill the subprocess if it's still around so cmd.Wait() returns promptly.
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if waitErr := cmd.Wait(); waitErr != nil && err == nil {
		// Wait will report exit status from the kill — ignore that specific
		// error if we already got a clean Decision; otherwise surface it.
		if d == nil {
			err = fmt.Errorf("claude wait: %w (stderr: %s)", waitErr, truncate(stderrBuf.String(), 500))
		}
	}
	return d, err
}

// writeUserMessage emits the NDJSON envelope claude expects on stdin.
func writeUserMessage(w io.Writer, prompt string) error {
	payload := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]string{
				{"type": "text", "text": prompt},
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// parseStream reads claude's NDJSON stream until the final `result` envelope.
// Accumulates all assistant text, tracks tool names brain used, captures token
// usage. Returns the Decision parsed out of the final assistant text.
//
// Important: claude in --input-format stream-json mode keeps stdout open
// even after emitting the final `result` event (it's still waiting on stdin
// for potential follow-up user messages). bufio.Scanner.Scan() blocks until
// EOF, so we MUST break out of the loop the moment we see "result" rather
// than keep scanning. The caller is responsible for cmd.Wait() afterward,
// which sends SIGPIPE to claude and lets it shut down cleanly.
func parseStream(r io.Reader) (*loop.Decision, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var assistantText strings.Builder
	var toolsUsed []string
	usage := loop.TokenUsage{}
	var resultText string
	var sawResult bool

scan:
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg streamMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "assistant":
			handleAssistant(msg, &assistantText, &toolsUsed, &usage)
		case "result":
			sawResult = true
			if msg.Subtype == "error_max_turns" || msg.Subtype == "error" || msg.IsError {
				return nil, fmt.Errorf("claude returned error result: subtype=%s text=%s",
					msg.Subtype, truncate(msg.ResultText, 200))
			}
			if msg.ResultText != "" {
				resultText = msg.ResultText
			}
			break scan
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}
	if !sawResult {
		return nil, fmt.Errorf("claude stream ended without result envelope; assistant text: %s",
			truncate(assistantText.String(), 200))
	}

	source := resultText
	if source == "" {
		source = assistantText.String()
	}
	source = extractJSONObject(source)
	var d loop.Decision
	if err := json.Unmarshal([]byte(source), &d); err != nil {
		return nil, fmt.Errorf("parse decision: %w; source: %s", err, truncate(source, 500))
	}
	if d.Decision == "" {
		return nil, fmt.Errorf("decision missing in output: %s", truncate(source, 500))
	}
	d.ToolsUsed = toolsUsed
	d.Usage = &usage
	return &d, nil
}

// handleAssistant accumulates text content, records tool names invoked, and
// sums per-call token usage from a single assistant stream message.
func handleAssistant(msg streamMessage, text *strings.Builder, tools *[]string, usage *loop.TokenUsage) {
	var content assistantMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return
	}
	if content.Usage != nil {
		usage.InputTokens += content.Usage.InputTokens
		usage.OutputTokens += content.Usage.OutputTokens
		usage.CacheReadTokens += content.Usage.CacheReadInputTokens
		usage.CacheWriteTokens += content.Usage.CacheCreationInputTokens
	}
	for _, b := range content.Content {
		switch b.Type {
		case "text":
			if b.Text != "" {
				text.WriteString(b.Text)
			}
		case "tool_use":
			if b.Name != "" {
				*tools = append(*tools, b.Name)
			}
		}
	}
}

// extractJSONObject finds the first { and the last } to recover JSON wrapped
// in markdown fences or surrounded by prose. Lenient on purpose.
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

// ── Claude SDK NDJSON types (subset; see Multica server/pkg/agent/claude.go for full schema) ──

type streamMessage struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype,omitempty"`
	Message    json.RawMessage `json:"message,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	ResultText string          `json:"result,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
}

type assistantMessageContent struct {
	Role    string                  `json:"role"`
	Content []assistantContentBlock `json:"content"`
	Model   string                  `json:"model,omitempty"`
	Usage   *streamUsage            `json:"usage,omitempty"`
}

type assistantContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	ID    string          `json:"id,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type streamUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}
