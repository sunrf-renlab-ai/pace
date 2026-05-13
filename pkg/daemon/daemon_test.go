package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartWritesPortFileAndAcceptsEvents(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	d, err := Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Stop()

	portFile := filepath.Join(dir, ".config", "mentor", "port")
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
		t.Fatalf("port file not written")
	}

	resp, err := http.Post("http://127.0.0.1:"+port+"/event", "application/json",
		strings.NewReader(`{"event_id":"a","timestamp":"2026-01-01T00:00:00Z","hook_type":"Stop","session_id":"s","project_path":"/tmp/x"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestStartHealthz(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	d, err := Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Stop()

	portFile := filepath.Join(dir, ".config", "mentor", "port")
	var port string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(portFile); err == nil {
			port = strings.TrimSpace(string(b))
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	resp, err := http.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		t.Fatalf("GET healthz: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}
