// fakeclaude is a stand-in for the real `claude` CLI used by brain tests.
// v0.8: emits stream-json NDJSON to match the protocol Pace's brain now
// consumes. Drains stdin (test harness sends a user-message envelope but
// the response is fully canned).
//
// Honored env vars:
//
//	FAKE_CLAUDE_DECISION_JSON  — JSON object brain should parse out of the
//	                             assistant text. Default: ignore decision.
//	FAKE_CLAUDE_PROSE_PREFIX   — text emitted before the JSON in the
//	                             assistant text (simulates LLM adding
//	                             markdown fence / explanation prose).
//	FAKE_CLAUDE_OMIT_RESULT    — if set, skip the final result envelope to
//	                             test the "stream ended without result" path.
//	FAKE_CLAUDE_RESULT_ERROR   — if set, emit result with is_error=true.
//	FAKE_CLAUDE_TOOL_NAME      — if set, also emit a tool_use block with
//	                             this name to test tool tracking.
//	FAKE_CLAUDE_USAGE_INPUT    — input_tokens to report (default 100)
//	FAKE_CLAUDE_USAGE_OUTPUT   — output_tokens to report (default 50)
//	FAKE_CLAUDE_EXIT           — process exit code (default 0)
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
)

func main() {
	go io.Copy(io.Discard, os.Stdin)

	decisionJSON := os.Getenv("FAKE_CLAUDE_DECISION_JSON")
	if decisionJSON == "" {
		decisionJSON = `{"decision":"ignore","rationale":"default fake","params":{}}`
	}
	prosePrefix := os.Getenv("FAKE_CLAUDE_PROSE_PREFIX")
	assistantText := prosePrefix + decisionJSON

	inputTokens := atoi(os.Getenv("FAKE_CLAUDE_USAGE_INPUT"), 100)
	outputTokens := atoi(os.Getenv("FAKE_CLAUDE_USAGE_OUTPUT"), 50)

	contentBlocks := []map[string]any{
		{"type": "text", "text": assistantText},
	}
	if tn := os.Getenv("FAKE_CLAUDE_TOOL_NAME"); tn != "" {
		contentBlocks = append(contentBlocks, map[string]any{
			"type":  "tool_use",
			"name":  tn,
			"id":    "toolu_fake_1",
			"input": map[string]any{"path": "/fake"},
		})
	}
	emit(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":    "assistant",
			"content": contentBlocks,
			"model":   "claude-fake",
			"usage": map[string]any{
				"input_tokens":                inputTokens,
				"output_tokens":               outputTokens,
				"cache_read_input_tokens":     0,
				"cache_creation_input_tokens": 0,
			},
		},
	})

	if os.Getenv("FAKE_CLAUDE_OMIT_RESULT") == "" {
		result := map[string]any{
			"type":       "result",
			"subtype":    "success",
			"result":     assistantText,
			"is_error":   false,
			"session_id": "fake-session",
		}
		if os.Getenv("FAKE_CLAUDE_RESULT_ERROR") != "" {
			result["is_error"] = true
			result["subtype"] = "error"
		}
		emit(result)
	}

	if code := os.Getenv("FAKE_CLAUDE_EXIT"); code != "" {
		if n, err := strconv.Atoi(code); err == nil {
			os.Exit(n)
		}
	}
}

func emit(m map[string]any) {
	b, _ := json.Marshal(m)
	fmt.Println(string(b))
}

func atoi(s string, def int64) int64 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return n
}
