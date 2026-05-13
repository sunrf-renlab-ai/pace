package brain

import (
	"context"
	"os/exec"
	"path/filepath"
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
		"FAKE_CLAUDE_RESPONSE": `{"result":"{\"decision\":\"notify\",\"rationale\":\"test\",\"params\":{\"title\":\"hi\"}}"}`,
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
		"FAKE_CLAUDE_RESPONSE": `{"result":"{\"decision\":\"ignore\",\"rationale\":\"noise\",\"params\":{}}"}`,
	})
	d, err := b.Decide(context.Background(), loop.DeciderInput{TriggerReason: "test"})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Decision != "ignore" {
		t.Errorf("decision = %q, want ignore", d.Decision)
	}
}

func TestParseDecisionDirectJSON(t *testing.T) {
	out := []byte(`{"decision":"ignore","rationale":"x","params":{}}`)
	d, err := parseDecision(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Decision != "ignore" {
		t.Errorf("got %q", d.Decision)
	}
}

func TestParseDecisionFromMarkdownFence(t *testing.T) {
	out := []byte("{\"result\":\"Here you go:\\n\\n```json\\n{\\\"decision\\\":\\\"notify\\\",\\\"rationale\\\":\\\"y\\\",\\\"params\\\":{}}\\n```\\n\"}")
	d, err := parseDecision(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Decision != "notify" {
		t.Errorf("got %q (raw=%s)", d.Decision, out)
	}
}

func TestParseDecisionMissingDecisionField(t *testing.T) {
	out := []byte(`{"rationale":"y","params":{}}`)
	_, err := parseDecision(out)
	if err == nil {
		t.Errorf("expected error on missing decision field")
	}
}

func TestExitNonZeroBubblesUp(t *testing.T) {
	bin := buildFakeClaude(t)
	b := New(bin, map[string]string{
		"FAKE_CLAUDE_RESPONSE": `garbage`,
		"FAKE_CLAUDE_EXIT":     "2",
	})
	_, err := b.Decide(context.Background(), loop.DeciderInput{})
	if err == nil {
		t.Errorf("expected error on non-zero exit")
	}
}
