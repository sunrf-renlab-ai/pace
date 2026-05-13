// fakeclaude is a stand-in for the real `claude` CLI used by brain tests.
// It honors a few env vars to drive deterministic behavior:
//   FAKE_CLAUDE_RESPONSE       — full stdout to print (verbatim)
//   FAKE_CLAUDE_EXIT           — exit code (default 0)
package main

import (
	"fmt"
	"os"
	"strconv"
)

func main() {
	resp := os.Getenv("FAKE_CLAUDE_RESPONSE")
	if resp == "" {
		resp = `{"result":"{\"decision\":\"ignore\",\"rationale\":\"default fake response\",\"params\":{}}"}`
	}
	fmt.Print(resp)

	if code := os.Getenv("FAKE_CLAUDE_EXIT"); code != "" {
		if n, err := strconv.Atoi(code); err == nil {
			os.Exit(n)
		}
	}
}
