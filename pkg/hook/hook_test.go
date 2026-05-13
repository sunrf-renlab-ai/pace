package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCreatesSettingsAndScript(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}

	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks key missing: %v", s)
	}
	for _, key := range []string{"UserPromptSubmit", "PostToolUse", "Stop"} {
		if _, ok := hooks[key]; !ok {
			t.Errorf("hooks.%s missing", key)
		}
	}

	scriptPath := filepath.Join(dir, ".config", "pace", "hook.sh")
	st, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("stat script: %v", err)
	}
	if st.Mode().Perm()&0o111 == 0 {
		t.Errorf("script not executable: %v", st.Mode())
	}
}

func TestInstallPreservesExistingHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	existing := map[string]any{
		"hooks": map[string]any{
			"UserPromptSubmit": []any{
				map[string]any{"hooks": []any{
					map[string]any{"type": "command", "command": "echo hi"},
				}},
			},
		},
		"otherKey": "preserved",
	}
	b, _ := json.Marshal(existing)
	os.WriteFile(settingsPath, b, 0o644)

	if err := Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, _ := os.ReadFile(settingsPath)
	var s map[string]any
	json.Unmarshal(data, &s)
	if s["otherKey"] != "preserved" {
		t.Errorf("otherKey not preserved")
	}
	hooks := s["hooks"].(map[string]any)
	prompts := hooks["UserPromptSubmit"].([]any)
	if len(prompts) < 2 {
		t.Errorf("expected existing + pace hooks, got %d entries", len(prompts))
	}
}

func TestInstallIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := Install(); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	if err := Install(); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	data, _ := os.ReadFile(settingsPath)
	var s map[string]any
	json.Unmarshal(data, &s)
	hooks := s["hooks"].(map[string]any)
	for _, key := range []string{"UserPromptSubmit", "PostToolUse", "Stop"} {
		entries, _ := hooks[key].([]any)
		count := 0
		for _, e := range entries {
			em := e.(map[string]any)
			inner := em["hooks"].([]any)
			for _, h := range inner {
				hm := h.(map[string]any)
				if cmd, _ := hm["command"].(string); strings.Contains(cmd, marker) {
					count++
				}
			}
		}
		if count != 1 {
			t.Errorf("%s pace hook count = %d, want 1 (idempotency broken)", key, count)
		}
	}
}

func TestUninstallRemovesOnlyPaceHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	existing := map[string]any{
		"hooks": map[string]any{
			"PostToolUse": []any{
				map[string]any{"hooks": []any{
					map[string]any{"type": "command", "command": "echo other"},
				}},
			},
		},
	}
	b, _ := json.Marshal(existing)
	os.WriteFile(settingsPath, b, 0o644)

	Install()
	if err := Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	data, _ := os.ReadFile(settingsPath)
	var s map[string]any
	json.Unmarshal(data, &s)
	str := string(data)
	if strings.Contains(str, marker) {
		t.Errorf("marker still present after uninstall: %s", str)
	}
	hooks := s["hooks"].(map[string]any)
	pt := hooks["PostToolUse"].([]any)
	if len(pt) != 1 {
		t.Errorf("non-pace PostToolUse entries = %d, want 1", len(pt))
	}
}
