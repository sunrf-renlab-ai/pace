package brain

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunrf-renlab-ai/pace/pkg/loop"
)

func buildFakeClaude(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "fakeclaude")
	cmd := exec.Command("go", "build", "-o", binPath, "../../testdata/fakeclaude")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakeclaude: %v\n%s", err, out)
	}
	return binPath
}

func TestDecideHappyPath(t *testing.T) {
	bin := buildFakeClaude(t)
	b := New(bin, map[string]string{
		"FAKE_CLAUDE_DECISION_JSON": `{"decision":"notify","rationale":"test","params":{"title":"hi"}}`,
	})
	d, err := b.Decide(context.Background(), loop.DeciderInput{TriggerReason: "test"})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Decision != "notify" {
		t.Errorf("decision = %q, want notify", d.Decision)
	}
	if d.Params["title"] != "hi" {
		t.Errorf("params.title = %v, want 'hi'", d.Params["title"])
	}
}

func TestDecideIgnore(t *testing.T) {
	bin := buildFakeClaude(t)
	b := New(bin, map[string]string{
		"FAKE_CLAUDE_DECISION_JSON": `{"decision":"ignore","rationale":"noise","params":{}}`,
	})
	d, err := b.Decide(context.Background(), loop.DeciderInput{TriggerReason: "test"})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Decision != "ignore" {
		t.Errorf("decision = %q, want ignore", d.Decision)
	}
}

// Brain must extract the decision JSON even when wrapped in prose / markdown.
func TestDecideExtractsJSONFromMarkdownFence(t *testing.T) {
	bin := buildFakeClaude(t)
	b := New(bin, map[string]string{
		"FAKE_CLAUDE_PROSE_PREFIX":  "Here you go:\n\n```json\n",
		"FAKE_CLAUDE_DECISION_JSON": `{"decision":"notify","rationale":"y","params":{}}`,
	})
	d, err := b.Decide(context.Background(), loop.DeciderInput{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Decision != "notify" {
		t.Errorf("got %q", d.Decision)
	}
}

// Token usage from assistant messages flows into Decision.Usage.
func TestDecideCapturesTokenUsage(t *testing.T) {
	bin := buildFakeClaude(t)
	b := New(bin, map[string]string{
		"FAKE_CLAUDE_DECISION_JSON": `{"decision":"ignore","rationale":"x","params":{}}`,
		"FAKE_CLAUDE_USAGE_INPUT":   "1234",
		"FAKE_CLAUDE_USAGE_OUTPUT":  "567",
	})
	d, err := b.Decide(context.Background(), loop.DeciderInput{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if d.Usage.InputTokens != 1234 {
		t.Errorf("input tokens = %d, want 1234", d.Usage.InputTokens)
	}
	if d.Usage.OutputTokens != 567 {
		t.Errorf("output tokens = %d, want 567", d.Usage.OutputTokens)
	}
}

// tool_use blocks in assistant messages get recorded in Decision.ToolsUsed.
func TestDecideTracksTools(t *testing.T) {
	bin := buildFakeClaude(t)
	b := New(bin, map[string]string{
		"FAKE_CLAUDE_DECISION_JSON": `{"decision":"ignore","rationale":"x","params":{}}`,
		"FAKE_CLAUDE_TOOL_NAME":     "Read",
	})
	d, err := b.Decide(context.Background(), loop.DeciderInput{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if len(d.ToolsUsed) != 1 || d.ToolsUsed[0] != "Read" {
		t.Errorf("ToolsUsed = %v, want [Read]", d.ToolsUsed)
	}
}

// If claude exits without ever sending a result envelope, brain must error
// rather than return a partial Decision.
func TestDecideStreamWithoutResultErrors(t *testing.T) {
	bin := buildFakeClaude(t)
	b := New(bin, map[string]string{
		"FAKE_CLAUDE_DECISION_JSON": `{"decision":"ignore","rationale":"x","params":{}}`,
		"FAKE_CLAUDE_OMIT_RESULT":   "1",
	})
	_, err := b.Decide(context.Background(), loop.DeciderInput{})
	if err == nil {
		t.Errorf("expected error when result envelope is missing")
	}
	if !strings.Contains(err.Error(), "without result") {
		t.Errorf("error should mention missing result; got: %v", err)
	}
}

// Result envelope with is_error must surface as error.
func TestDecideResultErrorPath(t *testing.T) {
	bin := buildFakeClaude(t)
	b := New(bin, map[string]string{
		"FAKE_CLAUDE_RESULT_ERROR": "1",
	})
	_, err := b.Decide(context.Background(), loop.DeciderInput{})
	if err == nil {
		t.Errorf("expected error when result is_error=true")
	}
}

// Non-zero exit from claude bubbles up as an error.
func TestExitNonZeroBubblesUp(t *testing.T) {
	bin := buildFakeClaude(t)
	b := New(bin, map[string]string{
		"FAKE_CLAUDE_OMIT_RESULT": "1",
		"FAKE_CLAUDE_EXIT":        "2",
	})
	_, err := b.Decide(context.Background(), loop.DeciderInput{})
	if err == nil {
		t.Errorf("expected error on non-zero exit")
	}
}

// extractJSONObject pulls the first {...} from arbitrary prose.
func TestExtractJSONObject(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`{"decision":"ignore"}`, `{"decision":"ignore"}`},
		{"prose...{\"a\":1}...trailing", `{"a":1}`},
		{"```json\n{\"x\":2}\n```", `{"x":2}`},
		{"no json here", "no json here"},
	}
	for _, c := range cases {
		got := extractJSONObject(c.in)
		if got != c.want {
			t.Errorf("extractJSONObject(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
