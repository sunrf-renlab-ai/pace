package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/daemon"
)

func main() {
	tmp, _ := os.MkdirTemp("", "pace-e2e-")
	defer os.RemoveAll(tmp)
	os.Setenv("HOME", tmp)

	d, err := daemon.Start()
	if err != nil {
		die("start: %v", err)
	}
	defer d.Stop()

	portFile := filepath.Join(tmp, ".config", "pace", "port")
	deadline := time.Now().Add(2 * time.Second)
	var port string
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(portFile); err == nil {
			port = strings.TrimSpace(string(b))
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if port == "" {
		die("no port file")
	}

	ev := map[string]any{
		"event_id":         "e2e-1",
		"timestamp":        time.Now().UTC().Format(time.RFC3339),
		"hook_type":        "PostToolUse",
		"session_id":       "s-e2e",
		"project_path":     "/tmp/foo",
		"tool_name":        "Bash",
		"tool_exit_status": "ok",
	}
	body, _ := json.Marshal(ev)
	resp, err := http.Post("http://127.0.0.1:"+port+"/event", "application/json", bytes.NewReader(body))
	if err != nil {
		die("POST: %v", err)
	}
	if resp.StatusCode != 200 {
		die("status = %d", resp.StatusCode)
	}

	var count int
	d.State.DB().QueryRow("SELECT COUNT(*) FROM events").Scan(&count)
	if count != 1 {
		die("events = %d", count)
	}
	var projects int
	d.State.DB().QueryRow("SELECT COUNT(*) FROM projects").Scan(&projects)
	if projects != 1 {
		die("projects = %d", projects)
	}
	fmt.Println("e2e ok — daemon ingested 1 event into 1 project row")
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+f+"\n", a...)
	os.Exit(1)
}
