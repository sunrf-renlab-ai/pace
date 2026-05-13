package hook

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed script.sh.tmpl
var scriptTemplate string

const marker = "# mentor-managed-hook"

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mentor"), nil
}

func settingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// Install writes the hook script and merges hook entries into ~/.claude/settings.json.
// Idempotent.
func Install() error {
	cfg, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		return err
	}
	scriptPath := filepath.Join(cfg, "hook.sh")
	if err := os.WriteFile(scriptPath, []byte(scriptTemplate), 0o755); err != nil {
		return err
	}

	sp, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(sp), 0o755); err != nil {
		return err
	}

	settings := map[string]any{}
	if data, err := os.ReadFile(sp); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	for _, key := range []string{"UserPromptSubmit", "PostToolUse", "Stop"} {
		entries, _ := hooks[key].([]any)
		entries = removeMentorEntries(entries)
		entries = append(entries, map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": fmt.Sprintf("%s %s %s", scriptPath, key, marker),
			}},
		})
		hooks[key] = entries
	}
	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	tmp := sp + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, sp)
}

// Uninstall removes only entries whose command contains the marker.
func Uninstall() error {
	sp, err := settingsPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(sp)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	for _, key := range []string{"UserPromptSubmit", "PostToolUse", "Stop"} {
		if entries, ok := hooks[key].([]any); ok {
			cleaned := removeMentorEntries(entries)
			if len(cleaned) == 0 {
				delete(hooks, key)
			} else {
				hooks[key] = cleaned
			}
		}
	}
	settings["hooks"] = hooks
	out, _ := json.MarshalIndent(settings, "", "  ")
	tmp := sp + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, sp)
}

func IsInstalled() (bool, error) {
	sp, err := settingsPath()
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(sp)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return strings.Contains(string(data), marker), nil
}

func removeMentorEntries(entries []any) []any {
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		em, ok := e.(map[string]any)
		if !ok {
			out = append(out, e)
			continue
		}
		inner, _ := em["hooks"].([]any)
		isMentor := false
		for _, h := range inner {
			if hm, ok := h.(map[string]any); ok {
				if cmd, _ := hm["command"].(string); strings.Contains(cmd, marker) {
					isMentor = true
					break
				}
			}
		}
		if !isMentor {
			out = append(out, e)
		}
	}
	return out
}
