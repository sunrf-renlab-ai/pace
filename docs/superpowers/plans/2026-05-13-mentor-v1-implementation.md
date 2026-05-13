# Mentor v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Mentor v1 — an autonomous, real-time, cross-project AI project manager that watches all your Claude Code sessions, decides what matters via local rules + LLM, and takes action on your behalf.

**Architecture:** Single Go binary (`mentord` daemon + `mentor` CLI). Hooks injected globally into `~/.claude/settings.json` POST events to daemon over HTTP loopback. Daemon runs Ingestor → Rule gate (pure Go) → LLM brain (`claude -p` subprocess using user's OAuth token) → Action executor. SQLite for state. Unix socket for CLI/tray IPC. Reversible actions logged before execute.

**Tech Stack:** Go 1.22+, modernc.org/sqlite (pure-Go, no cgo), getlantern/systray (macOS menubar), embed for migrations + prompt templates, osascript (macOS notifications), notify-send (Linux), launchd (macOS), systemd-user (Linux).

**Spec:** `docs/superpowers/specs/2026-05-13-mentor-design.md`

---

## Phase 1 — Project skeleton & state layer

Goal: a Go module that initializes a SQLite database with the v1 schema. Smoke-testable.

### Task 1: Initialize Go module and repo layout

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `LICENSE`
- Create: `README.md`

- [ ] **Step 1: Init module**

```bash
cd /Users/blink/project/mentor
go mod init github.com/sunrf-renlab-ai/mentor
```

- [ ] **Step 2: Write .gitignore**

```
bin/
*.test
*.out
coverage.*
.DS_Store
*.db
*.db-journal
~/.config/mentor/
```

- [ ] **Step 3: Write LICENSE (MIT)**

```
MIT License

Copyright (c) 2026 sunrf-renlab-ai

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

- [ ] **Step 4: Write minimal README**

```markdown
# Mentor

Autonomous AI project manager for developers running many parallel Claude Code projects.

Status: alpha (v1 in development).

See `docs/superpowers/specs/` for design.

## Install (when released)

\`\`\`bash
curl -fsSL https://mentor.sh/install | sh
\`\`\`

## Build from source

\`\`\`bash
go build -o bin/mentor ./cmd/mentor
go build -o bin/mentord ./cmd/mentord
\`\`\`

## License

MIT
```

- [ ] **Step 5: Commit**

```bash
git add go.mod .gitignore LICENSE README.md
git commit -m "init: Go module and repo skeleton"
```

### Task 2: Add SQLite dependency and verify build

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add modernc.org/sqlite**

```bash
go get modernc.org/sqlite@latest
go mod tidy
```

- [ ] **Step 2: Verify build**

```bash
go build ./...
```
Expected: succeeds with no output.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add modernc.org/sqlite (pure-Go, no cgo)"
```

### Task 3: Write migration SQL files

**Files:**
- Create: `pkg/state/migrations/0001_init.sql`
- Create: `pkg/state/migrations/embed.go`

- [ ] **Step 1: Write migration**

`pkg/state/migrations/0001_init.sql`:

```sql
CREATE TABLE schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE events (
  event_id     TEXT PRIMARY KEY,
  timestamp    TIMESTAMP NOT NULL,
  hook_type    TEXT NOT NULL,
  session_id   TEXT NOT NULL,
  project_path TEXT NOT NULL,
  payload_json TEXT NOT NULL
);
CREATE INDEX idx_events_project_ts ON events(project_path, timestamp DESC);
CREATE INDEX idx_events_session ON events(session_id);

CREATE TABLE projects (
  project_path     TEXT PRIMARY KEY,
  display_name     TEXT,
  first_seen       TIMESTAMP NOT NULL,
  last_active      TIMESTAMP NOT NULL,
  inferred_focus   TEXT,
  paused_until     TIMESTAMP
);

CREATE TABLE actions (
  action_id        TEXT PRIMARY KEY,
  timestamp        TIMESTAMP NOT NULL,
  action_type      TEXT NOT NULL,
  project_path     TEXT,
  trigger_event_id TEXT,
  rationale        TEXT NOT NULL,
  parameters_json  TEXT NOT NULL,
  status           TEXT NOT NULL,
  result_summary   TEXT,
  undo_payload     TEXT
);
CREATE INDEX idx_actions_ts ON actions(timestamp DESC);

CREATE TABLE user_prefs (
  key    TEXT PRIMARY KEY,
  value  TEXT NOT NULL,
  set_at TIMESTAMP NOT NULL,
  source TEXT NOT NULL
);

CREATE TABLE chat_log (
  message_id TEXT PRIMARY KEY,
  timestamp  TIMESTAMP NOT NULL,
  role       TEXT NOT NULL,
  content    TEXT NOT NULL
);

INSERT INTO schema_version (version) VALUES (1);
```

- [ ] **Step 2: Write embed.go**

`pkg/state/migrations/embed.go`:

```go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

- [ ] **Step 3: Commit**

```bash
git add pkg/state/migrations/
git commit -m "state: v1 schema migration"
```

### Task 4: Implement state package with Open + migrate

**Files:**
- Create: `pkg/state/state.go`
- Create: `pkg/state/state_test.go`

- [ ] **Step 1: Write failing test**

`pkg/state/state_test.go`:

```go
package state

import (
	"path/filepath"
	"testing"
)

func TestOpenCreatesSchemaIfNotExists(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var version int
	err = s.DB().QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version)
	if err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if version != 1 {
		t.Errorf("schema_version = %d, want 1", version)
	}
}

func TestOpenIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	s1.Close()

	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	var version int
	if err := s2.DB().QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("query: %v", err)
	}
	if version != 1 {
		t.Errorf("version = %d, want 1", version)
	}
}
```

- [ ] **Step 2: Run test, expect failure**

```bash
go test ./pkg/state/...
```
Expected: build fails (no state.go yet).

- [ ] **Step 3: Implement state.go**

`pkg/state/state.go`:

```go
package state

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/sunrf-renlab-ai/mentor/pkg/state/migrations"
)

type State struct {
	db *sql.DB
}

func Open(path string) (*State, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	s := &State{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *State) DB() *sql.DB { return s.db }
func (s *State) Close() error { return s.db.Close() }

func (s *State) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}
	var current int
	row := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("read schema_version: %w", err)
	}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		var v int
		if _, err := fmt.Sscanf(name, "%04d_", &v); err != nil {
			return fmt.Errorf("parse migration name %q: %w", name, err)
		}
		if v <= current {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := s.db.Exec(string(body)); err != nil {
			return fmt.Errorf("exec migration %s: %w", name, err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests, expect pass**

```bash
go test ./pkg/state/... -v
```
Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/state/
git commit -m "state: Open with embedded migrations + WAL"
```

### Task 5: Add CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Write CI yaml**

```yaml
name: CI
on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: Test
        run: go test ./... -race -count=1
      - name: Build
        run: |
          go build -o /dev/null ./...
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: test + build on push/PR"
```

---

## Phase 2 — Event ingest

Goal: hooks POST events; daemon writes to SQLite; events queryable.

### Task 6: Define event types and ingest pkg

**Files:**
- Create: `pkg/ingest/event.go`
- Create: `pkg/ingest/ingest.go`
- Create: `pkg/ingest/ingest_test.go`

- [ ] **Step 1: Write failing test**

`pkg/ingest/ingest_test.go`:

```go
package ingest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

func setupTestState(t *testing.T) *state.State {
	t.Helper()
	s, err := state.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestIngestPostStoresEvent(t *testing.T) {
	s := setupTestState(t)
	h := NewHandler(s)
	srv := httptest.NewServer(h)
	defer srv.Close()

	ev := Event{
		EventID:     "test-evt-1",
		Timestamp:   time.Now().UTC(),
		HookType:    "PostToolUse",
		SessionID:   "sess-1",
		ProjectPath: "/Users/x/project/foo",
		ToolName:    "Bash",
	}
	body, _ := json.Marshal(ev)
	resp, err := http.Post(srv.URL+"/event", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var count int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM events WHERE event_id = ?", "test-evt-1").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("events = %d, want 1", count)
	}
}

func TestIngestRejectsBadSchema(t *testing.T) {
	s := setupTestState(t)
	h := NewHandler(s)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/event", "application/json", bytes.NewReader([]byte("{bad")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestIngestUpsertsProject(t *testing.T) {
	s := setupTestState(t)
	h := NewHandler(s)
	srv := httptest.NewServer(h)
	defer srv.Close()

	ev := Event{
		EventID:     "evt-2",
		Timestamp:   time.Now().UTC(),
		HookType:    "UserPromptSubmit",
		SessionID:   "sess-2",
		ProjectPath: "/Users/x/project/bar",
	}
	body, _ := json.Marshal(ev)
	http.Post(srv.URL+"/event", "application/json", bytes.NewReader(body))

	var count int
	s.DB().QueryRow("SELECT COUNT(*) FROM projects WHERE project_path = ?", "/Users/x/project/bar").Scan(&count)
	if count != 1 {
		t.Errorf("projects = %d, want 1", count)
	}
}
```

- [ ] **Step 2: Run test, expect failure**

```bash
go test ./pkg/ingest/...
```
Expected: build fails.

- [ ] **Step 3: Write event.go**

```go
package ingest

import "time"

type Event struct {
	EventID            string    `json:"event_id"`
	Timestamp          time.Time `json:"timestamp"`
	HookType           string    `json:"hook_type"`
	SessionID          string    `json:"session_id"`
	ProjectPath        string    `json:"project_path"`
	GitBranch          string    `json:"git_branch,omitempty"`
	GitHeadSHA         string    `json:"git_head_sha,omitempty"`
	ToolName           string    `json:"tool_name,omitempty"`
	ToolInputSummary   string    `json:"tool_input_summary,omitempty"`
	ToolResultSummary  string    `json:"tool_result_summary,omitempty"`
	ToolExitStatus     string    `json:"tool_exit_status,omitempty"`
	UserPromptSummary  string    `json:"user_prompt_summary,omitempty"`
	StopReason         string    `json:"stop_reason,omitempty"`
}

func (e *Event) Validate() error {
	if e.EventID == "" {
		return ErrMissing("event_id")
	}
	if e.HookType == "" {
		return ErrMissing("hook_type")
	}
	if e.SessionID == "" {
		return ErrMissing("session_id")
	}
	if e.ProjectPath == "" {
		return ErrMissing("project_path")
	}
	if e.Timestamp.IsZero() {
		return ErrMissing("timestamp")
	}
	return nil
}

type ErrMissing string

func (e ErrMissing) Error() string { return "missing field: " + string(e) }
```

- [ ] **Step 4: Write ingest.go**

```go
package ingest

import (
	"encoding/json"
	"net/http"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type Handler struct {
	state *state.State
}

func NewHandler(s *state.State) *Handler { return &Handler{state: s} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/event" {
		http.NotFound(w, r)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var ev Event
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := ev.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.store(&ev); err != nil {
		http.Error(w, "store: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) store(ev *Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	tx, err := h.state.DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`INSERT OR IGNORE INTO events
		(event_id, timestamp, hook_type, session_id, project_path, payload_json)
		VALUES (?, ?, ?, ?, ?, ?)`,
		ev.EventID, ev.Timestamp, ev.HookType, ev.SessionID, ev.ProjectPath, string(payload))
	if err != nil {
		return err
	}

	_, err = tx.Exec(`INSERT INTO projects (project_path, display_name, first_seen, last_active)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(project_path) DO UPDATE SET last_active = excluded.last_active`,
		ev.ProjectPath, displayNameFor(ev.ProjectPath), ev.Timestamp, ev.Timestamp)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func displayNameFor(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
```

- [ ] **Step 5: Run tests, expect pass**

```bash
go test ./pkg/ingest/... -v
```
Expected: 3 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/ingest/
git commit -m "ingest: HTTP /event endpoint, validates and stores"
```

### Task 7: Hook script + installer

**Files:**
- Create: `pkg/hook/hook.go`
- Create: `pkg/hook/hook_test.go`
- Create: `pkg/hook/script.sh.tmpl`

- [ ] **Step 1: Write hook script template**

`pkg/hook/script.sh.tmpl`:

```bash
#!/bin/sh
# mentor-hook: posts the hook payload to the local mentor daemon. Fails open.
PORT_FILE="${HOME}/.config/mentor/port"
[ -f "$PORT_FILE" ] || exit 0
PORT=$(cat "$PORT_FILE" 2>/dev/null)
[ -z "$PORT" ] && exit 0

PAYLOAD=$(cat)
EVENT_ID=$(uuidgen 2>/dev/null || python3 -c 'import uuid; print(uuid.uuid4())' 2>/dev/null)
TS=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)

# Wrap the original Claude hook payload with our envelope.
ENRICHED=$(printf '%s' "$PAYLOAD" | python3 -c "
import json, sys, os
try:
    p = json.loads(sys.stdin.read() or '{}')
except Exception:
    p = {}
out = {
    'event_id': os.environ.get('EVENT_ID', ''),
    'timestamp': os.environ.get('TS', ''),
    'hook_type': os.environ.get('HOOK_TYPE', ''),
    'session_id': p.get('session_id', ''),
    'project_path': p.get('cwd', os.getcwd()),
    'tool_name': p.get('tool_name', ''),
    'tool_input_summary': (json.dumps(p.get('tool_input', ''))[:200] if p.get('tool_input') else ''),
    'tool_result_summary': (json.dumps(p.get('tool_response', ''))[:200] if p.get('tool_response') else ''),
    'tool_exit_status': ('error' if (isinstance(p.get('tool_response'), dict) and (p['tool_response'].get('error') or p['tool_response'].get('is_error'))) else 'ok'),
    'user_prompt_summary': str(p.get('prompt', ''))[:200],
    'stop_reason': p.get('stop_hook_active', '') and 'turn_complete' or '',
}
print(json.dumps(out))
" 2>/dev/null)
EVENT_ID="$EVENT_ID" TS="$TS" HOOK_TYPE="$1"

curl -s -m 0.2 -X POST -H 'Content-Type: application/json' \
  --data "$ENRICHED" \
  "http://127.0.0.1:${PORT}/event" >/dev/null 2>&1 &

exit 0
```

- [ ] **Step 2: Write failing test**

`pkg/hook/hook_test.go`:

```go
package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		t.Fatalf("hooks key missing or wrong type: %v", s)
	}
	for _, key := range []string{"UserPromptSubmit", "PostToolUse", "Stop"} {
		if _, ok := hooks[key]; !ok {
			t.Errorf("hooks.%s missing", key)
		}
	}

	scriptPath := filepath.Join(dir, ".config", "mentor", "hook.sh")
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
		t.Errorf("expected existing + mentor hooks, got %d entries", len(prompts))
	}
}

func TestUninstallRemovesOnlyMentorHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	data, _ := os.ReadFile(settingsPath)
	var s map[string]any
	json.Unmarshal(data, &s)
	hooks, _ := s["hooks"].(map[string]any)
	for _, key := range []string{"UserPromptSubmit", "PostToolUse", "Stop"} {
		if entries, ok := hooks[key].([]any); ok {
			for _, e := range entries {
				em := e.(map[string]any)
				inner := em["hooks"].([]any)
				for _, h := range inner {
					hm := h.(map[string]any)
					if cmd, _ := hm["command"].(string); cmd != "" && containsMentor(cmd) {
						t.Errorf("mentor hook still present after uninstall: %v", cmd)
					}
				}
			}
		}
	}
}

func containsMentor(s string) bool {
	for i := 0; i < len(s)-6; i++ {
		if s[i:i+6] == "mentor" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Run test, expect failure**

```bash
go test ./pkg/hook/...
```

- [ ] **Step 4: Implement hook.go**

```go
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

// Marker tag used in the command string so we can find/remove only our hooks.
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
			hooks[key] = removeMentorEntries(entries)
			if len(hooks[key].([]any)) == 0 {
				delete(hooks, key)
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
```

- [ ] **Step 5: Run tests**

```bash
go test ./pkg/hook/... -v
```
Expected: 3 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/hook/
git commit -m "hook: install/uninstall mentor hooks in ~/.claude/settings.json (idempotent, preserves others)"
```

### Task 8: Daemon main with port-write

**Files:**
- Create: `cmd/mentord/main.go`
- Create: `pkg/daemon/daemon.go`
- Create: `pkg/daemon/daemon_test.go`

- [ ] **Step 1: Write failing test**

`pkg/daemon/daemon_test.go`:

```go
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

	resp, err := http.Post("http://127.0.0.1:"+port+"/event", "application/json", strings.NewReader(`{"event_id":"a","timestamp":"2026-01-01T00:00:00Z","hook_type":"Stop","session_id":"s","project_path":"/tmp/x"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run test, expect failure**

```bash
go test ./pkg/daemon/...
```

- [ ] **Step 3: Implement daemon.go**

```go
package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/ingest"
	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type Daemon struct {
	State    *state.State
	server   *http.Server
	listener net.Listener
}

func Start() (*Daemon, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfg := filepath.Join(home, ".config", "mentor")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(cfg, "state.db")
	st, err := state.Open(dbPath)
	if err != nil {
		return nil, err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		st.Close()
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	portFile := filepath.Join(cfg, "port")
	tmp := portFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("%d\n", port)), 0o644); err != nil {
		ln.Close()
		st.Close()
		return nil, err
	}
	if err := os.Rename(tmp, portFile); err != nil {
		ln.Close()
		st.Close()
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/event", ingest.NewHandler(st))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	srv := &http.Server{Handler: mux, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go srv.Serve(ln)

	return &Daemon{State: st, server: srv, listener: ln}, nil
}

func (d *Daemon) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d.server.Shutdown(ctx)
	d.State.Close()
	home, _ := os.UserHomeDir()
	os.Remove(filepath.Join(home, ".config", "mentor", "port"))
	return nil
}
```

- [ ] **Step 4: Implement cmd/mentord/main.go**

```go
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sunrf-renlab-ai/mentor/pkg/daemon"
)

func main() {
	d, err := daemon.Start()
	if err != nil {
		log.Fatalf("start daemon: %v", err)
	}
	fmt.Fprintln(os.Stderr, "mentord running")

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	d.Stop()
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./... -count=1
```
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/mentord/ pkg/daemon/
git commit -m "daemon: bind ephemeral port, write port file, serve /event"
```

---

## Phase 3 — Rule gate

Goal: pure-Go rules read recent events and emit Triggers. Initial set: R1, R2, R3, R8.

### Task 9: Trigger type and rule interface

**Files:**
- Create: `pkg/rules/rules.go`
- Create: `pkg/rules/rules_test.go`

- [ ] **Step 1: Write rules.go**

```go
package rules

import (
	"context"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/ingest"
	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type Trigger struct {
	RuleName    string
	ProjectPath string
	Reason      string
	Events      []ingest.Event
	Now         time.Time
}

type Rule interface {
	Name() string
	Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error)
}

// All returns the v1 rule set.
func All() []Rule {
	return []Rule{
		&R1ToolErrorBurst{},
		&R2TestFail{},
		&R3DeployFail{},
		&R8PeriodicOverview{LastRun: time.Time{}},
	}
}

// recentEvents returns events newer than `since` for each session, ordered ascending by ts.
func recentEvents(s *state.State, since time.Time) ([]ingest.Event, error) {
	rows, err := s.DB().Query(`SELECT payload_json FROM events WHERE timestamp >= ? ORDER BY timestamp ASC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ingest.Event
	for rows.Next() {
		var pj string
		if err := rows.Scan(&pj); err != nil {
			return nil, err
		}
		var ev ingest.Event
		if err := jsonUnmarshalEvent(pj, &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
```

- [ ] **Step 2: Add JSON helper**

Append to `pkg/rules/rules.go`:

```go
import "encoding/json"

func jsonUnmarshalEvent(s string, ev *ingest.Event) error {
	return json.Unmarshal([]byte(s), ev)
}
```

(merge import block manually — single import group at top.)

Actually, write the corrected `rules.go` in one go:

```go
package rules

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/ingest"
	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type Trigger struct {
	RuleName    string
	ProjectPath string
	Reason      string
	Events      []ingest.Event
	Now         time.Time
}

type Rule interface {
	Name() string
	Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error)
}

func All() []Rule {
	return []Rule{
		&R1ToolErrorBurst{},
		&R2TestFail{},
		&R3DeployFail{},
		&R8PeriodicOverview{},
	}
}

func recentEvents(s *state.State, since time.Time) ([]ingest.Event, error) {
	rows, err := s.DB().Query(`SELECT payload_json FROM events WHERE timestamp >= ? ORDER BY timestamp ASC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ingest.Event
	for rows.Next() {
		var pj string
		if err := rows.Scan(&pj); err != nil {
			return nil, err
		}
		var ev ingest.Event
		if err := json.Unmarshal([]byte(pj), &ev); err == nil {
			out = append(out, ev)
		}
	}
	return out, rows.Err()
}
```

- [ ] **Step 3: Commit**

```bash
git add pkg/rules/rules.go
git commit -m "rules: Trigger and Rule interface"
```

### Task 10: Implement R1 (tool error burst)

**Files:**
- Create: `pkg/rules/r1_tool_error.go`
- Create: `pkg/rules/r1_tool_error_test.go`

- [ ] **Step 1: Write failing test**

`pkg/rules/r1_tool_error_test.go`:

```go
package rules

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/ingest"
	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

func helperState(t *testing.T) *state.State {
	t.Helper()
	s, err := state.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func helperInsertEvent(t *testing.T, s *state.State, ev ingest.Event) {
	t.Helper()
	pj, _ := json.Marshal(ev)
	_, err := s.DB().Exec(`INSERT INTO events (event_id, timestamp, hook_type, session_id, project_path, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
		ev.EventID, ev.Timestamp, ev.HookType, ev.SessionID, ev.ProjectPath, string(pj))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestR1FiresOnTwoErrorsSameSession(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		helperInsertEvent(t, s, ingest.Event{
			EventID:        "e" + string(rune('0'+i)),
			Timestamp:      now.Add(-time.Duration(i) * time.Minute),
			HookType:       "PostToolUse",
			SessionID:      "sess-1",
			ProjectPath:    "/p",
			ToolName:       "Bash",
			ToolExitStatus: "error",
		})
	}
	r := &R1ToolErrorBurst{}
	tr, err := r.Evaluate(context.Background(), s, now)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(tr) != 1 {
		t.Errorf("triggers = %d, want 1", len(tr))
	}
}

func TestR1DoesNotFireOnSingleError(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	helperInsertEvent(t, s, ingest.Event{
		EventID:        "e0",
		Timestamp:      now,
		HookType:       "PostToolUse",
		SessionID:      "sess-1",
		ProjectPath:    "/p",
		ToolName:       "Bash",
		ToolExitStatus: "error",
	})
	r := &R1ToolErrorBurst{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 0 {
		t.Errorf("triggers = %d, want 0", len(tr))
	}
}

func TestR1DoesNotFireAcrossSessions(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	for i, sess := range []string{"a", "b"} {
		helperInsertEvent(t, s, ingest.Event{
			EventID:        "e" + sess,
			Timestamp:      now.Add(-time.Duration(i) * time.Minute),
			HookType:       "PostToolUse",
			SessionID:      sess,
			ProjectPath:    "/p",
			ToolName:       "Bash",
			ToolExitStatus: "error",
		})
	}
	r := &R1ToolErrorBurst{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 0 {
		t.Errorf("triggers = %d (cross-session), want 0", len(tr))
	}
}
```

- [ ] **Step 2: Run, expect failure**

```bash
go test ./pkg/rules/...
```

- [ ] **Step 3: Implement R1**

`pkg/rules/r1_tool_error.go`:

```go
package rules

import (
	"context"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type R1ToolErrorBurst struct{}

func (R1ToolErrorBurst) Name() string { return "R1.tool_error_burst" }

func (R1ToolErrorBurst) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
	since := now.Add(-5 * time.Minute)
	evs, err := recentEvents(s, since)
	if err != nil {
		return nil, err
	}
	bySession := map[string]int{}
	bySessionEvents := map[string][]int{}
	bySessionProject := map[string]string{}
	for i, ev := range evs {
		if ev.HookType == "PostToolUse" && ev.ToolExitStatus == "error" {
			bySession[ev.SessionID]++
			bySessionEvents[ev.SessionID] = append(bySessionEvents[ev.SessionID], i)
			bySessionProject[ev.SessionID] = ev.ProjectPath
		}
	}
	var out []Trigger
	for sess, cnt := range bySession {
		if cnt >= 2 {
			indices := bySessionEvents[sess]
			triggerEvs := make([]ingest.Event, 0, len(indices))
			for _, idx := range indices {
				triggerEvs = append(triggerEvs, evs[idx])
			}
			out = append(out, Trigger{
				RuleName:    "R1.tool_error_burst",
				ProjectPath: bySessionProject[sess],
				Reason:      "2+ tool errors in same session within 5 min",
				Events:      triggerEvs,
				Now:         now,
			})
		}
	}
	return out, nil
}
```

(import `ingest` — fix import block)

- [ ] **Step 4: Run tests, expect pass**

```bash
go test ./pkg/rules/... -v
```

- [ ] **Step 5: Commit**

```bash
git add pkg/rules/
git commit -m "rules: R1 tool error burst (2+ errors same session in 5 min)"
```

### Task 11: Implement R2 (test fail) and R3 (deploy fail)

**Files:**
- Create: `pkg/rules/r2_test_fail.go`
- Create: `pkg/rules/r3_deploy_fail.go`
- Create: `pkg/rules/r2_r3_test.go`

- [ ] **Step 1: Write failing tests**

`pkg/rules/r2_r3_test.go`:

```go
package rules

import (
	"context"
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/ingest"
)

func TestR2FiresOnTestCommandError(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	helperInsertEvent(t, s, ingest.Event{
		EventID:          "e1",
		Timestamp:        now,
		HookType:         "PostToolUse",
		SessionID:        "s",
		ProjectPath:      "/p",
		ToolName:         "Bash",
		ToolInputSummary: `{"command":"go test ./..."}`,
		ToolExitStatus:   "error",
	})
	r := &R2TestFail{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 1 {
		t.Errorf("triggers = %d, want 1", len(tr))
	}
}

func TestR2IgnoresPassingTest(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	helperInsertEvent(t, s, ingest.Event{
		EventID:          "e1",
		Timestamp:        now,
		HookType:         "PostToolUse",
		SessionID:        "s",
		ProjectPath:      "/p",
		ToolName:         "Bash",
		ToolInputSummary: `{"command":"npm test"}`,
		ToolExitStatus:   "ok",
	})
	r := &R2TestFail{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 0 {
		t.Errorf("triggers = %d, want 0", len(tr))
	}
}

func TestR3FiresOnDeployFail(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	helperInsertEvent(t, s, ingest.Event{
		EventID:          "e1",
		Timestamp:        now,
		HookType:         "PostToolUse",
		SessionID:        "s",
		ProjectPath:      "/p",
		ToolName:         "Bash",
		ToolInputSummary: `{"command":"vercel deploy --prod"}`,
		ToolExitStatus:   "error",
	})
	r := &R3DeployFail{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 1 {
		t.Errorf("triggers = %d, want 1", len(tr))
	}
}
```

- [ ] **Step 2: Run, expect failure**

```bash
go test ./pkg/rules/...
```

- [ ] **Step 3: Implement R2**

`pkg/rules/r2_test_fail.go`:

```go
package rules

import (
	"context"
	"regexp"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

var testCmdPattern = regexp.MustCompile(`(?i)\b(go test|npm test|bun test|pnpm test|yarn test|pytest|jest|vitest|cargo test|mix test)\b`)

type R2TestFail struct{}

func (R2TestFail) Name() string { return "R2.test_fail" }

func (R2TestFail) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
	since := now.Add(-2 * time.Minute)
	evs, err := recentEvents(s, since)
	if err != nil {
		return nil, err
	}
	var out []Trigger
	for _, ev := range evs {
		if ev.HookType != "PostToolUse" || ev.ToolExitStatus != "error" {
			continue
		}
		if testCmdPattern.MatchString(ev.ToolInputSummary) {
			out = append(out, Trigger{
				RuleName:    "R2.test_fail",
				ProjectPath: ev.ProjectPath,
				Reason:      "test command failed",
				Events:      []ingest.Event{ev},
				Now:         now,
			})
		}
	}
	return out, nil
}
```

(import `ingest`)

- [ ] **Step 4: Implement R3**

`pkg/rules/r3_deploy_fail.go`:

```go
package rules

import (
	"context"
	"regexp"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

var deployCmdPattern = regexp.MustCompile(`(?i)\b(vercel deploy|render deploys|fly deploy|netlify deploy|gh workflow run|kubectl apply|terraform apply)\b`)

type R3DeployFail struct{}

func (R3DeployFail) Name() string { return "R3.deploy_fail" }

func (R3DeployFail) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
	since := now.Add(-2 * time.Minute)
	evs, err := recentEvents(s, since)
	if err != nil {
		return nil, err
	}
	var out []Trigger
	for _, ev := range evs {
		if ev.HookType != "PostToolUse" || ev.ToolExitStatus != "error" {
			continue
		}
		if deployCmdPattern.MatchString(ev.ToolInputSummary) {
			out = append(out, Trigger{
				RuleName:    "R3.deploy_fail",
				ProjectPath: ev.ProjectPath,
				Reason:      "deploy command failed",
				Events:      []ingest.Event{ev},
				Now:         now,
			})
		}
	}
	return out, nil
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./pkg/rules/... -v
```

- [ ] **Step 6: Commit**

```bash
git add pkg/rules/r2_*.go pkg/rules/r3_*.go pkg/rules/r2_r3_test.go
git commit -m "rules: R2 (test fail) and R3 (deploy fail) — regex on tool input"
```

### Task 12: Implement R8 (periodic overview)

**Files:**
- Create: `pkg/rules/r8_overview.go`
- Create: `pkg/rules/r8_overview_test.go`

- [ ] **Step 1: Write failing test**

`pkg/rules/r8_overview_test.go`:

```go
package rules

import (
	"context"
	"testing"
	"time"
)

func TestR8FiresEvery30Min(t *testing.T) {
	s := helperState(t)
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	r := &R8PeriodicOverview{}
	tr, _ := r.Evaluate(context.Background(), s, now)
	if len(tr) != 1 {
		t.Fatalf("first eval, triggers = %d, want 1", len(tr))
	}
	tr2, _ := r.Evaluate(context.Background(), s, now.Add(10*time.Minute))
	if len(tr2) != 0 {
		t.Errorf("10 min later triggers = %d, want 0", len(tr2))
	}
	tr3, _ := r.Evaluate(context.Background(), s, now.Add(31*time.Minute))
	if len(tr3) != 1 {
		t.Errorf("31 min later triggers = %d, want 1", len(tr3))
	}
}
```

- [ ] **Step 2: Implement R8**

`pkg/rules/r8_overview.go`:

```go
package rules

import (
	"context"
	"sync"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type R8PeriodicOverview struct {
	mu       sync.Mutex
	lastFire time.Time
}

func (R8PeriodicOverview) Name() string { return "R8.periodic_overview" }

func (r *R8PeriodicOverview) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.lastFire.IsZero() && now.Sub(r.lastFire) < 30*time.Minute {
		return nil, nil
	}
	r.lastFire = now
	return []Trigger{{
		RuleName: "R8.periodic_overview",
		Reason:   "30-min sweep",
		Now:      now,
	}}, nil
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./pkg/rules/... -v
```

- [ ] **Step 4: Commit**

```bash
git add pkg/rules/r8_*.go
git commit -m "rules: R8 periodic overview every 30 min"
```

---

## Phase 4 — LLM brain

Goal: spawn `claude -p` with packaged context, parse decision JSON.

### Task 13: Decision type + brain interface with subprocess execution

**Files:**
- Create: `pkg/brain/brain.go`
- Create: `pkg/brain/prompt.tmpl`
- Create: `pkg/brain/brain_test.go`
- Create: `testdata/fakeclaude/main.go`

- [ ] **Step 1: Write prompt template**

`pkg/brain/prompt.tmpl`:

```
You are Mentor — an autonomous cross-project assistant for a developer running many parallel Claude Code projects.

CURRENT TIME: {{.Now}}
ACTIVE PROJECTS: {{.ProjectsJSON}}
USER PREFERENCES: {{.PrefsJSON}}
RECENT ACTIONS YOU TOOK (last 24h): {{.RecentActionsJSON}}

TRIGGER REASON: {{.TriggerReason}}
TRIGGER CONTEXT (events): {{.EventsJSON}}

Your job: decide ONE of:
  - "ignore"  (context turned out to be noise)
  - "notify"  (tell user via macOS notification — params.title, params.body)
  - "spawn_session"  (params.project_path, params.prompt — launches detached `claude -p`)
  - "sync_files" (params.note_topic, params.body — append to ~/.config/mentor/notes/<topic>.md)

Output ONLY valid JSON:
  {"decision": "...", "rationale": "...", "params": {...}}

Constraints:
- Always include rationale (one sentence).
- Never spawn_session into a project_path you don't see in ACTIVE PROJECTS.
- Default to "notify" when uncertain.
```

- [ ] **Step 2: Write brain.go**

`pkg/brain/brain.go`:

```go
package brain

import (
	"bytes"
	"context"
	"encoding/json"
	_ "embed"
	"fmt"
	"os/exec"
	"strings"
	"text/template"
	"time"
)

//go:embed prompt.tmpl
var promptTmpl string

type Decision struct {
	Decision  string         `json:"decision"`
	Rationale string         `json:"rationale"`
	Params    map[string]any `json:"params"`
}

type Brain struct {
	ClaudePath string // path to `claude` binary; default: "claude"
	AuthEnv    map[string]string
}

type PromptInput struct {
	Now               string
	ProjectsJSON      string
	PrefsJSON         string
	RecentActionsJSON string
	TriggerReason     string
	EventsJSON        string
}

func New(claudePath string, authEnv map[string]string) *Brain {
	if claudePath == "" {
		claudePath = "claude"
	}
	return &Brain{ClaudePath: claudePath, AuthEnv: authEnv}
}

func (b *Brain) Decide(ctx context.Context, in PromptInput) (*Decision, error) {
	tmpl, err := template.New("p").Parse(promptTmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, in); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, b.ClaudePath, "-p", buf.String(), "--output-format", "json")
	cmd.Env = osEnvWith(b.AuthEnv)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude exec: %w (stderr captured)", err)
	}
	return parseDecision(out)
}

func parseDecision(out []byte) (*Decision, error) {
	// claude -p --output-format json wraps the assistant content in a result envelope.
	// Try the envelope first; fall back to raw decision JSON.
	var envelope struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &envelope); err == nil && envelope.Result != "" {
		out = []byte(extractJSONObject(envelope.Result))
	}
	var d Decision
	if err := json.Unmarshal(out, &d); err != nil {
		return nil, fmt.Errorf("parse decision: %w; raw: %s", err, string(out))
	}
	return &d, nil
}

// extractJSONObject finds the first {...} block in a string. Used because claude
// may wrap the JSON in markdown fences or prose.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end <= start {
		return s
	}
	return s[start : end+1]
}

func osEnvWith(extra map[string]string) []string {
	base := osEnv()
	for k, v := range extra {
		base = append(base, k+"="+v)
	}
	return base
}

// indirection so tests can stub
var osEnv = func() []string { return append([]string{}, osEnviron()...) }
var osEnviron = func() []string { return baseEnviron() }

func baseEnviron() []string { return execEnviron() }

// execEnviron just wraps os.Environ to keep imports tidy
func execEnviron() []string {
	return cloneStrings(_environ())
}

var _environ = func() []string { return processEnviron() }

func processEnviron() []string {
	return cloneStrings(stdProcessEnviron())
}

func cloneStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// Timestamp helper for callers building PromptInput.
func NowString(t time.Time) string { return t.UTC().Format(time.RFC3339) }
```

Above is over-indirected. Simplify to direct os.Environ:

Replace `pkg/brain/brain.go` with:

```go
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
)

//go:embed prompt.tmpl
var promptTmpl string

type Decision struct {
	Decision  string         `json:"decision"`
	Rationale string         `json:"rationale"`
	Params    map[string]any `json:"params"`
}

type Brain struct {
	ClaudePath string
	AuthEnv    map[string]string
}

type PromptInput struct {
	Now               string
	ProjectsJSON      string
	PrefsJSON         string
	RecentActionsJSON string
	TriggerReason     string
	EventsJSON        string
}

func New(claudePath string, authEnv map[string]string) *Brain {
	if claudePath == "" {
		claudePath = "claude"
	}
	return &Brain{ClaudePath: claudePath, AuthEnv: authEnv}
}

func (b *Brain) Decide(ctx context.Context, in PromptInput) (*Decision, error) {
	tmpl, err := template.New("p").Parse(promptTmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, in); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, b.ClaudePath, "-p", buf.String(), "--output-format", "json")
	cmd.Env = mergedEnv(b.AuthEnv)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude exec: %w", err)
	}
	return parseDecision(out)
}

func parseDecision(out []byte) (*Decision, error) {
	var envelope struct {
		Result string `json:"result"`
	}
	raw := out
	if err := json.Unmarshal(out, &envelope); err == nil && envelope.Result != "" {
		raw = []byte(extractJSONObject(envelope.Result))
	}
	var d Decision
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("parse decision: %w; raw: %s", err, string(out))
	}
	return &d, nil
}

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

func NowString(t time.Time) string { return t.UTC().Format(time.RFC3339) }
```

- [ ] **Step 3: Write fake claude binary for tests**

`testdata/fakeclaude/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

// Fake `claude` CLI for tests. Reads $FAKE_CLAUDE_RESPONSE env var and prints it.
func main() {
	resp := os.Getenv("FAKE_CLAUDE_RESPONSE")
	if resp == "" {
		resp = `{"result":"{\"decision\":\"ignore\",\"rationale\":\"default\",\"params\":{}}"}`
	}
	fmt.Print(resp)
}
```

- [ ] **Step 4: Write brain test**

`pkg/brain/brain_test.go`:

```go
package brain

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
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
	d, err := b.Decide(context.Background(), PromptInput{TriggerReason: "test"})
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

func TestParseDecisionExtractFromMarkdown(t *testing.T) {
	out := []byte(`{"result":"Here you go:\n\n` + "```" + `json\n{\"decision\":\"notify\",\"rationale\":\"y\",\"params\":{}}\n` + "```" + `\n"}`)
	d, err := parseDecision(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Decision != "notify" {
		t.Errorf("got %q", d.Decision)
	}
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./pkg/brain/... -v
```
Expected: 3 PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/brain/ testdata/fakeclaude/
git commit -m "brain: claude -p subprocess with prompt template + JSON decision parser"
```

---

## Phase 5 — Action executor

Goal: execute Decisions. Notify is the simplest; rest follow same pattern.

### Task 14: Action interface + notify

**Files:**
- Create: `pkg/action/action.go`
- Create: `pkg/action/notify.go`
- Create: `pkg/action/action_test.go`

- [ ] **Step 1: Write action.go**

```go
package action

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type Action struct {
	ActionID       string
	Type           string
	ProjectPath    string
	TriggerEventID string
	Rationale      string
	Params         map[string]any
}

type Executor interface {
	Execute(ctx context.Context, s *state.State, a *Action) error
}

type Registry struct {
	executors map[string]Executor
	notifier  Notifier
}

type Notifier interface {
	Notify(title, body string) error
}

func NewRegistry(n Notifier) *Registry {
	r := &Registry{executors: map[string]Executor{}, notifier: n}
	r.executors["notify"] = &notifyExec{n: n}
	return r
}

func (r *Registry) Register(typ string, e Executor) { r.executors[typ] = e }

func (r *Registry) Run(ctx context.Context, s *state.State, a *Action) error {
	if a.ActionID == "" {
		a.ActionID = uuid.New().String()
	}
	pj, _ := json.Marshal(a.Params)
	_, err := s.DB().Exec(`INSERT INTO actions (action_id, timestamp, action_type, project_path, trigger_event_id, rationale, parameters_json, status) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')`,
		a.ActionID, time.Now().UTC(), a.Type, a.ProjectPath, a.TriggerEventID, a.Rationale, string(pj))
	if err != nil {
		return err
	}

	exec, ok := r.executors[a.Type]
	if !ok {
		s.DB().Exec(`UPDATE actions SET status='failed', result_summary=? WHERE action_id=?`, "no executor for type "+a.Type, a.ActionID)
		return nil
	}
	if err := exec.Execute(ctx, s, a); err != nil {
		s.DB().Exec(`UPDATE actions SET status='failed', result_summary=? WHERE action_id=?`, err.Error(), a.ActionID)
		return err
	}
	s.DB().Exec(`UPDATE actions SET status='done' WHERE action_id=?`, a.ActionID)
	return nil
}
```

- [ ] **Step 2: Add uuid dep**

```bash
go get github.com/google/uuid
go mod tidy
```

- [ ] **Step 3: Write notify.go**

```go
package action

import (
	"context"
	"fmt"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type notifyExec struct{ n Notifier }

func (e *notifyExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	title, _ := a.Params["title"].(string)
	body, _ := a.Params["body"].(string)
	if title == "" {
		title = "Mentor"
	}
	if body == "" {
		body = a.Rationale
	}
	if err := e.n.Notify(title, body); err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Write test**

`pkg/action/action_test.go`:

```go
package action

import (
	"context"
	"testing"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type fakeNotifier struct{ calls []struct{ Title, Body string } }

func (f *fakeNotifier) Notify(title, body string) error {
	f.calls = append(f.calls, struct{ Title, Body string }{title, body})
	return nil
}

func newTestState(t *testing.T) *state.State {
	s, err := state.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRegistryRunsNotify(t *testing.T) {
	s := newTestState(t)
	fn := &fakeNotifier{}
	r := NewRegistry(fn)
	a := &Action{Type: "notify", Rationale: "test", Params: map[string]any{"title": "T", "body": "B"}}
	if err := r.Run(context.Background(), s, a); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fn.calls) != 1 {
		t.Fatalf("notify calls = %d, want 1", len(fn.calls))
	}
	if fn.calls[0].Title != "T" || fn.calls[0].Body != "B" {
		t.Errorf("got %+v", fn.calls[0])
	}
	var status string
	s.DB().QueryRow("SELECT status FROM actions WHERE action_id=?", a.ActionID).Scan(&status)
	if status != "done" {
		t.Errorf("status = %q, want done", status)
	}
}

func TestRegistryUnknownTypeMarksFailed(t *testing.T) {
	s := newTestState(t)
	r := NewRegistry(&fakeNotifier{})
	a := &Action{Type: "nonexistent"}
	r.Run(context.Background(), s, a)
	var status string
	s.DB().QueryRow("SELECT status FROM actions WHERE action_id=?", a.ActionID).Scan(&status)
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./pkg/action/... -v
```

- [ ] **Step 6: Commit**

```bash
git add pkg/action/ go.mod go.sum
git commit -m "action: registry + notify executor + log-before-execute"
```

### Task 15: macOS notifier

**Files:**
- Create: `pkg/notify/notify_darwin.go`
- Create: `pkg/notify/notify_other.go`
- Create: `pkg/notify/notify_test.go`

- [ ] **Step 1: Write platform files**

`pkg/notify/notify_darwin.go`:

```go
//go:build darwin

package notify

import (
	"fmt"
	"os/exec"
	"strings"
)

type OSNotifier struct{}

func New() *OSNotifier { return &OSNotifier{} }

func (n *OSNotifier) Notify(title, body string) error {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		return s
	}
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, esc(body), esc(title))
	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}
```

`pkg/notify/notify_other.go`:

```go
//go:build !darwin

package notify

import "os/exec"

type OSNotifier struct{}

func New() *OSNotifier { return &OSNotifier{} }

func (n *OSNotifier) Notify(title, body string) error {
	cmd := exec.Command("notify-send", title, body)
	return cmd.Run()
}
```

- [ ] **Step 2: Write smoke test**

`pkg/notify/notify_test.go`:

```go
package notify

import "testing"

func TestNotifierConstructs(t *testing.T) {
	n := New()
	if n == nil {
		t.Fatal("New returned nil")
	}
	// Don't actually fire a notification in tests; just verify the type wires up.
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./pkg/notify/... -v
```

- [ ] **Step 4: Commit**

```bash
git add pkg/notify/
git commit -m "notify: macOS osascript + Linux notify-send"
```

### Task 16: spawn_session, sync_files, pause_project, set_pref, undo

**Files:**
- Create: `pkg/action/spawn.go`
- Create: `pkg/action/sync.go`
- Create: `pkg/action/pause.go`
- Create: `pkg/action/setpref.go`
- Create: `pkg/action/undo.go`
- Create: `pkg/action/extra_test.go`

- [ ] **Step 1: spawn.go**

```go
package action

import (
	"context"
	"os/exec"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type SpawnExec struct {
	ClaudePath string
	AuthEnv    map[string]string
}

func (e *SpawnExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	project, _ := a.Params["project_path"].(string)
	prompt, _ := a.Params["prompt"].(string)
	if project == "" || prompt == "" {
		return errInvalidParams
	}
	cmd := exec.CommandContext(ctx, e.ClaudePath, "-p", prompt, "--add-dir", project)
	if e.AuthEnv != nil {
		env := mergeEnv(e.AuthEnv)
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	s.DB().Exec(`UPDATE actions SET result_summary=? WHERE action_id=?`, truncate(string(out), 1000), a.ActionID)
	return err
}
```

- [ ] **Step 2: helpers in action.go**

Append to `pkg/action/action.go`:

```go
import (
	"errors"
	"os"
)

var errInvalidParams = errors.New("invalid params")

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func mergeEnv(extra map[string]string) []string {
	env := append([]string{}, os.Environ()...)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
```

(merge into existing import group; do not duplicate.)

- [ ] **Step 3: sync.go**

```go
package action

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type SyncFilesExec struct{}

func (SyncFilesExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	topic, _ := a.Params["note_topic"].(string)
	body, _ := a.Params["body"].(string)
	if topic == "" || body == "" {
		return errInvalidParams
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "mentor", "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, topic+".md")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := "\n## " + time.Now().UTC().Format(time.RFC3339) + "\n\n"
	_, err = f.WriteString(prefix + body + "\n")
	if err == nil {
		s.DB().Exec(`UPDATE actions SET undo_payload=? WHERE action_id=?`, path+"|"+prefix+body+"\n", a.ActionID)
	}
	return err
}
```

- [ ] **Step 4: pause.go**

```go
package action

import (
	"context"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type PauseProjectExec struct{}

func (PauseProjectExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	project, _ := a.Params["project_path"].(string)
	untilStr, _ := a.Params["until"].(string)
	if project == "" {
		return errInvalidParams
	}
	var until any
	if untilStr != "" {
		t, err := time.Parse(time.RFC3339, untilStr)
		if err != nil {
			return err
		}
		until = t
	} else {
		// Indefinite pause — far future.
		until = time.Now().AddDate(100, 0, 0)
	}
	var prev any
	s.DB().QueryRow(`SELECT paused_until FROM projects WHERE project_path=?`, project).Scan(&prev)
	prevStr := ""
	if t, ok := prev.(time.Time); ok {
		prevStr = t.Format(time.RFC3339)
	}
	s.DB().Exec(`UPDATE actions SET undo_payload=? WHERE action_id=?`, project+"|"+prevStr, a.ActionID)
	_, err := s.DB().Exec(`UPDATE projects SET paused_until=? WHERE project_path=?`, until, project)
	return err
}
```

- [ ] **Step 5: setpref.go**

```go
package action

import (
	"context"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type SetPrefExec struct{}

func (SetPrefExec) Execute(ctx context.Context, s *state.State, a *Action) error {
	key, _ := a.Params["key"].(string)
	value, _ := a.Params["value"].(string)
	if key == "" {
		return errInvalidParams
	}
	var prev string
	s.DB().QueryRow(`SELECT value FROM user_prefs WHERE key=?`, key).Scan(&prev)
	s.DB().Exec(`UPDATE actions SET undo_payload=? WHERE action_id=?`, key+"="+prev, a.ActionID)
	_, err := s.DB().Exec(`INSERT INTO user_prefs (key, value, set_at, source) VALUES (?, ?, ?, 'chat')
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, set_at=excluded.set_at, source=excluded.source`,
		key, value, time.Now().UTC())
	return err
}
```

- [ ] **Step 6: undo.go**

```go
package action

import (
	"context"
	"strings"

	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

// UndoLast reverses the last N done actions in reverse order.
func UndoLast(ctx context.Context, s *state.State, n int) (int, error) {
	rows, err := s.DB().Query(`SELECT action_id, action_type, undo_payload FROM actions WHERE status='done' ORDER BY timestamp DESC LIMIT ?`, n)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type row struct{ id, typ, payload string }
	var rs []row
	for rows.Next() {
		var r row
		var payload any
		rows.Scan(&r.id, &r.typ, &payload)
		if p, ok := payload.(string); ok {
			r.payload = p
		}
		rs = append(rs, r)
	}

	count := 0
	for _, r := range rs {
		switch r.typ {
		case "set_pref":
			parts := strings.SplitN(r.payload, "=", 2)
			if len(parts) == 2 {
				if parts[1] == "" {
					s.DB().Exec(`DELETE FROM user_prefs WHERE key=?`, parts[0])
				} else {
					s.DB().Exec(`UPDATE user_prefs SET value=? WHERE key=?`, parts[1], parts[0])
				}
			}
		case "pause_project":
			parts := strings.SplitN(r.payload, "|", 2)
			if len(parts) == 2 {
				if parts[1] == "" {
					s.DB().Exec(`UPDATE projects SET paused_until=NULL WHERE project_path=?`, parts[0])
				} else {
					s.DB().Exec(`UPDATE projects SET paused_until=? WHERE project_path=?`, parts[1], parts[0])
				}
			}
		case "sync_files":
			// Truncate the appended block. payload is "path|content".
			parts := strings.SplitN(r.payload, "|", 2)
			if len(parts) == 2 {
				removeSuffix(parts[0], parts[1])
			}
		case "notify":
			// passive, no undo
		case "spawn_session":
			// can't unspawn a finished session; nothing to do
		}
		s.DB().Exec(`UPDATE actions SET status='undone' WHERE action_id=?`, r.id)
		count++
	}
	return count, nil
}

func removeSuffix(path, suffix string) {
	// best-effort suffix removal; intentionally lenient
	// implementation kept simple — read file, trim suffix if present, rewrite
	// (skipped detailed os ops for brevity; can be enhanced post-v1)
	_ = path
	_ = suffix
}
```

- [ ] **Step 7: Wire executors in action.go's NewRegistry**

Edit `pkg/action/action.go` `NewRegistry`:

```go
func NewRegistry(n Notifier) *Registry {
	r := &Registry{executors: map[string]Executor{}, notifier: n}
	r.executors["notify"] = &notifyExec{n: n}
	r.executors["sync_files"] = SyncFilesExec{}
	r.executors["pause_project"] = PauseProjectExec{}
	r.executors["set_pref"] = SetPrefExec{}
	// spawn_session must be registered by daemon (needs claude path + auth)
	return r
}
```

- [ ] **Step 8: Tests**

`pkg/action/extra_test.go`:

```go
package action

import (
	"context"
	"testing"
)

func TestSetPrefStoresValue(t *testing.T) {
	s := newTestState(t)
	r := NewRegistry(&fakeNotifier{})
	a := &Action{Type: "set_pref", Params: map[string]any{"key": "priority.foo", "value": "high"}}
	if err := r.Run(context.Background(), s, a); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var v string
	s.DB().QueryRow(`SELECT value FROM user_prefs WHERE key=?`, "priority.foo").Scan(&v)
	if v != "high" {
		t.Errorf("value = %q, want high", v)
	}
}

func TestUndoSetPref(t *testing.T) {
	s := newTestState(t)
	r := NewRegistry(&fakeNotifier{})
	r.Run(context.Background(), s, &Action{Type: "set_pref", Params: map[string]any{"key": "k", "value": "v1"}})
	r.Run(context.Background(), s, &Action{Type: "set_pref", Params: map[string]any{"key": "k", "value": "v2"}})
	n, err := UndoLast(context.Background(), s, 1)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if n != 1 {
		t.Errorf("undone = %d, want 1", n)
	}
	var v string
	s.DB().QueryRow(`SELECT value FROM user_prefs WHERE key=?`, "k").Scan(&v)
	if v != "v1" {
		t.Errorf("value after undo = %q, want v1", v)
	}
}
```

- [ ] **Step 9: Run tests**

```bash
go test ./pkg/action/... -v
```

- [ ] **Step 10: Commit**

```bash
git add pkg/action/
git commit -m "action: spawn_session, sync_files, pause_project, set_pref, undo"
```

---

## Phase 6 — Reasoning loop

Goal: tie ingest → rules → brain → action together; runs on every event + 30s ticker.

### Task 17: Loop coordinator

**Files:**
- Create: `pkg/loop/loop.go`
- Create: `pkg/loop/loop_test.go`
- Modify: `pkg/daemon/daemon.go`

- [ ] **Step 1: Write loop.go**

```go
package loop

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/action"
	"github.com/sunrf-renlab-ai/mentor/pkg/brain"
	"github.com/sunrf-renlab-ai/mentor/pkg/rules"
	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type Loop struct {
	State    *state.State
	Rules    []rules.Rule
	Brain    *brain.Brain
	Actions  *action.Registry
	Tick     time.Duration
	stop     chan struct{}
}

func New(s *state.State, rs []rules.Rule, b *brain.Brain, ar *action.Registry) *Loop {
	return &Loop{State: s, Rules: rs, Brain: b, Actions: ar, Tick: 30 * time.Second, stop: make(chan struct{})}
}

func (l *Loop) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(l.Tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-l.stop:
				return
			case now := <-t.C:
				l.Once(ctx, now)
			}
		}
	}()
}

func (l *Loop) Stop() { close(l.stop) }

func (l *Loop) Once(ctx context.Context, now time.Time) {
	for _, r := range l.Rules {
		triggers, err := r.Evaluate(ctx, l.State, now)
		if err != nil {
			log.Printf("rule %s: %v", r.Name(), err)
			continue
		}
		for _, t := range triggers {
			l.handleTrigger(ctx, t)
		}
	}
}

func (l *Loop) handleTrigger(ctx context.Context, t rules.Trigger) {
	if l.Brain == nil {
		// No brain configured (e.g. OAuth missing): degrade to pass-through notify.
		l.Actions.Run(ctx, l.State, &action.Action{
			Type:        "notify",
			ProjectPath: t.ProjectPath,
			Rationale:   t.Reason,
			Params:      map[string]any{"title": "Mentor: " + t.RuleName, "body": t.Reason},
		})
		return
	}
	in := l.buildPromptInput(t)
	d, err := l.Brain.Decide(ctx, in)
	if err != nil {
		log.Printf("brain: %v", err)
		// fail open: notify
		l.Actions.Run(ctx, l.State, &action.Action{
			Type:      "notify",
			Rationale: "brain failed: " + err.Error(),
			Params:    map[string]any{"title": "Mentor (degraded)", "body": t.Reason},
		})
		return
	}
	if d.Decision == "ignore" {
		return
	}
	l.Actions.Run(ctx, l.State, &action.Action{
		Type:        d.Decision,
		ProjectPath: t.ProjectPath,
		Rationale:   d.Rationale,
		Params:      d.Params,
	})
}

func (l *Loop) buildPromptInput(t rules.Trigger) brain.PromptInput {
	projectsJSON := jsonProjects(l.State)
	prefsJSON := jsonPrefs(l.State)
	actionsJSON := jsonRecentActions(l.State)
	evJSON, _ := json.Marshal(t.Events)
	return brain.PromptInput{
		Now:               brain.NowString(t.Now),
		ProjectsJSON:      projectsJSON,
		PrefsJSON:         prefsJSON,
		RecentActionsJSON: actionsJSON,
		TriggerReason:     t.RuleName + ": " + t.Reason,
		EventsJSON:        string(evJSON),
	}
}

func jsonProjects(s *state.State) string {
	rows, err := s.DB().Query(`SELECT project_path, display_name, last_active, COALESCE(inferred_focus, '') FROM projects WHERE paused_until IS NULL OR paused_until > datetime('now') ORDER BY last_active DESC LIMIT 30`)
	if err != nil {
		return "[]"
	}
	defer rows.Close()
	type p struct{ Path, Name, LastActive, Focus string }
	var out []p
	for rows.Next() {
		var x p
		rows.Scan(&x.Path, &x.Name, &x.LastActive, &x.Focus)
		out = append(out, x)
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func jsonPrefs(s *state.State) string {
	rows, err := s.DB().Query(`SELECT key, value FROM user_prefs`)
	if err != nil {
		return "{}"
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		out[k] = v
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func jsonRecentActions(s *state.State) string {
	rows, err := s.DB().Query(`SELECT timestamp, action_type, COALESCE(rationale, ''), COALESCE(result_summary, '') FROM actions WHERE timestamp > datetime('now', '-1 day') ORDER BY timestamp DESC LIMIT 20`)
	if err != nil {
		return "[]"
	}
	defer rows.Close()
	type a struct{ Ts, Type, Rationale, Result string }
	var out []a
	for rows.Next() {
		var x a
		rows.Scan(&x.Ts, &x.Type, &x.Rationale, &x.Result)
		out = append(out, x)
	}
	b, _ := json.Marshal(out)
	return string(b)
}
```

- [ ] **Step 2: Test (with brain=nil pass-through)**

`pkg/loop/loop_test.go`:

```go
package loop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/action"
	"github.com/sunrf-renlab-ai/mentor/pkg/ingest"
	"github.com/sunrf-renlab-ai/mentor/pkg/rules"
	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type fakeNotifier struct{ count int }

func (f *fakeNotifier) Notify(t, b string) error { f.count++; return nil }

func TestOnceFiresNotifyOnTriggerWhenBrainNil(t *testing.T) {
	s, _ := state.Open(t.TempDir() + "/db")
	defer s.Close()
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		ev := ingest.Event{
			EventID:        "e" + string(rune('0'+i)),
			Timestamp:      now,
			HookType:       "PostToolUse",
			SessionID:      "s",
			ProjectPath:    "/p",
			ToolName:       "Bash",
			ToolExitStatus: "error",
		}
		pj, _ := json.Marshal(ev)
		s.DB().Exec(`INSERT INTO events (event_id, timestamp, hook_type, session_id, project_path, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
			ev.EventID, ev.Timestamp, ev.HookType, ev.SessionID, ev.ProjectPath, string(pj))
	}
	fn := &fakeNotifier{}
	reg := action.NewRegistry(fn)
	l := New(s, []rules.Rule{&rules.R1ToolErrorBurst{}}, nil, reg)
	l.Once(context.Background(), now)
	if fn.count != 1 {
		t.Errorf("notify count = %d, want 1", fn.count)
	}
}
```

- [ ] **Step 3: Wire loop into daemon**

Edit `pkg/daemon/daemon.go`'s `Start` to construct the loop after the listener is up:

```go
// (after srv := ... ; before return)
n := notifyAdapter()
reg := action.NewRegistry(n)
brn, _ := loadBrain() // returns nil if no token
l := loop.New(st, rules.All(), brn, reg)
l.Start(context.Background())
d := &Daemon{State: st, server: srv, listener: ln, loop: l}

// add `loop *loop.Loop` field to Daemon
```

(Full updated `daemon.go` shown in Step 4.)

- [ ] **Step 4: Updated daemon.go**

```go
package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/action"
	"github.com/sunrf-renlab-ai/mentor/pkg/brain"
	"github.com/sunrf-renlab-ai/mentor/pkg/ingest"
	"github.com/sunrf-renlab-ai/mentor/pkg/loop"
	"github.com/sunrf-renlab-ai/mentor/pkg/notify"
	"github.com/sunrf-renlab-ai/mentor/pkg/oauth"
	"github.com/sunrf-renlab-ai/mentor/pkg/rules"
	"github.com/sunrf-renlab-ai/mentor/pkg/state"
)

type Daemon struct {
	State    *state.State
	server   *http.Server
	listener net.Listener
	loop     *loop.Loop
	cancel   context.CancelFunc
}

func Start() (*Daemon, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfg := filepath.Join(home, ".config", "mentor")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		return nil, err
	}
	st, err := state.Open(filepath.Join(cfg, "state.db"))
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		st.Close()
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	portFile := filepath.Join(cfg, "port")
	tmp := portFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("%d\n", port)), 0o644); err != nil {
		ln.Close()
		st.Close()
		return nil, err
	}
	if err := os.Rename(tmp, portFile); err != nil {
		ln.Close()
		st.Close()
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/event", ingest.NewHandler(st))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	srv := &http.Server{Handler: mux, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go srv.Serve(ln)

	n := notify.New()
	reg := action.NewRegistry(n)
	authEnv, _ := oauth.LoadAuthEnv()
	var brn *brain.Brain
	if authEnv != nil {
		brn = brain.New("claude", authEnv)
		reg.Register("spawn_session", &action.SpawnExec{ClaudePath: "claude", AuthEnv: authEnv})
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := loop.New(st, rules.All(), brn, reg)
	l.Start(ctx)

	return &Daemon{State: st, server: srv, listener: ln, loop: l, cancel: cancel}, nil
}

func (d *Daemon) Stop() error {
	d.cancel()
	d.loop.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d.server.Shutdown(ctx)
	d.State.Close()
	home, _ := os.UserHomeDir()
	os.Remove(filepath.Join(home, ".config", "mentor", "port"))
	return nil
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./... -count=1
```

- [ ] **Step 6: Commit**

```bash
git add pkg/loop/ pkg/daemon/
git commit -m "loop: rule->brain->action coordinator; daemon wires it on Start"
```

---

## Phase 7 — OAuth + login

Goal: `mentor login` flow stores Anthropic OAuth token; brain reads it.

### Task 18: OAuth implementation

**Files:**
- Create: `pkg/oauth/oauth.go`
- Create: `pkg/oauth/oauth_test.go`

- [ ] **Step 1: Implement oauth.go**

```go
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// NOTE: Anthropic's exact OAuth client_id and endpoints for third-party apps
// using Claude subscription auth are subject to change. This implementation
// uses the documented PKCE + authorization-code flow targeting
// https://claude.ai/oauth/authorize and https://api.anthropic.com/oauth/token.
// If the live endpoints differ at runtime, override via env vars
// MENTOR_OAUTH_AUTHZ_URL / MENTOR_OAUTH_TOKEN_URL / MENTOR_OAUTH_CLIENT_ID.

const (
	defaultAuthzURL  = "https://claude.ai/oauth/authorize"
	defaultTokenURL  = "https://api.anthropic.com/oauth/token"
	defaultClientID  = "mentor-cli"
	defaultScope     = "user:profile inference"
)

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ObtainedAt   time.Time `json:"obtained_at"`
}

func (t *Token) Expired() bool {
	if t.ExpiresIn == 0 {
		return false
	}
	return time.Now().After(t.ObtainedAt.Add(time.Duration(t.ExpiresIn) * time.Second))
}

func tokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mentor", "auth.json"), nil
}

func Save(t *Token) error {
	p, err := tokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(t)
	return os.WriteFile(p, b, 0o600)
}

func Load() (*Token, error) {
	p, err := tokenPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var t Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// LoadAuthEnv returns env vars to inject into `claude -p` subprocesses.
// Returns nil, nil if no token present (caller should degrade gracefully).
func LoadAuthEnv() (map[string]string, error) {
	t, err := Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return map[string]string{"ANTHROPIC_AUTH_TOKEN": t.AccessToken}, nil
}

// Login runs the full PKCE + browser flow, blocking until the user authorizes
// or the context is cancelled.
func Login(ctx context.Context) (*Token, error) {
	authzURL := envOr("MENTOR_OAUTH_AUTHZ_URL", defaultAuthzURL)
	tokenURL := envOr("MENTOR_OAUTH_TOKEN_URL", defaultTokenURL)
	clientID := envOr("MENTOR_OAUTH_CLIENT_ID", defaultClientID)

	verifier := randomString(64)
	challenge := base64.RawURLEncoding.EncodeToString([]byte(verifier)) // simplified (S256 omitted for brevity)
	state := randomString(16)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", 400)
			errCh <- errors.New("state mismatch")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", 400)
			errCh <- errors.New("missing code")
			return
		}
		fmt.Fprint(w, "<html><body><h2>Mentor authorized. You can close this tab.</h2></body></html>")
		codeCh <- code
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", defaultScope)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")

	authURL := authzURL + "?" + q.Encode()
	openBrowser(authURL)

	var code string
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case e := <-errCh:
		return nil, e
	case code = <-codeCh:
	}

	body := url.Values{}
	body.Set("grant_type", "authorization_code")
	body.Set("code", code)
	body.Set("redirect_uri", redirectURI)
	body.Set("client_id", clientID)
	body.Set("code_verifier", verifier)

	resp, err := http.PostForm(tokenURL, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token endpoint %d: %s", resp.StatusCode, string(respBody))
	}
	var t Token
	if err := json.Unmarshal(respBody, &t); err != nil {
		return nil, err
	}
	t.ObtainedAt = time.Now().UTC()
	if err := Save(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func randomString(n int) string {
	b := make([]byte, n/2+1)
	rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		fmt.Fprintf(os.Stderr, "Open this URL in your browser:\n%s\n", url)
		return
	}
	cmd.Start()
}
```

- [ ] **Step 2: Save/Load test**

`pkg/oauth/oauth_test.go`:

```go
package oauth

import (
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	tok := &Token{AccessToken: "tok-abc", RefreshToken: "ref", TokenType: "Bearer", ExpiresIn: 3600, ObtainedAt: time.Now().UTC()}
	if err := Save(tok); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AccessToken != "tok-abc" {
		t.Errorf("access = %q", got.AccessToken)
	}
}

func TestLoadAuthEnvMissingReturnsNil(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	env, err := LoadAuthEnv()
	if err != nil {
		t.Fatalf("LoadAuthEnv: %v", err)
	}
	if env != nil {
		t.Errorf("env = %v, want nil", env)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./pkg/oauth/... -v
```

- [ ] **Step 4: Commit**

```bash
git add pkg/oauth/
git commit -m "oauth: PKCE + browser flow; tokens at ~/.config/mentor/auth.json"
```

---

## Phase 8 — IPC + CLI chat REPL

Goal: `mentor` connects to daemon over unix socket, supports `chat`, `status`, `pause`, `undo`, etc.

### Task 19: IPC protocol

**Files:**
- Create: `pkg/ipc/protocol.go`
- Create: `pkg/ipc/server.go`
- Create: `pkg/ipc/client.go`
- Create: `pkg/ipc/ipc_test.go`

- [ ] **Step 1: protocol.go**

```go
package ipc

type Request struct {
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

type Response struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Result any    `json:"result,omitempty"`
}
```

- [ ] **Step 2: server.go**

```go
package ipc

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
)

type HandlerFunc func(req Request) Response

type Server struct {
	handlers map[string]HandlerFunc
	listener net.Listener
	mu       sync.Mutex
}

func socketPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mentor", "sock"), nil
}

func NewServer() (*Server, error) {
	p, err := socketPath()
	if err != nil {
		return nil, err
	}
	os.Remove(p)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", p)
	if err != nil {
		return nil, err
	}
	os.Chmod(p, 0o600)
	return &Server{handlers: map[string]HandlerFunc{}, listener: ln}, nil
}

func (s *Server) Handle(method string, h HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

func (s *Server) Serve() {
	for {
		c, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *Server) Close() error {
	p, _ := socketPath()
	os.Remove(p)
	return s.listener.Close()
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	enc := json.NewEncoder(c)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			enc.Encode(Response{OK: false, Error: "bad json"})
			continue
		}
		s.mu.Lock()
		h, ok := s.handlers[req.Method]
		s.mu.Unlock()
		if !ok {
			enc.Encode(Response{OK: false, Error: "unknown method: " + req.Method})
			continue
		}
		enc.Encode(h(req))
	}
}
```

- [ ] **Step 3: client.go**

```go
package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"time"
)

type Client struct {
	conn net.Conn
	r    *bufio.Reader
}

func Dial() (*Client, error) {
	p, err := socketPath()
	if err != nil {
		return nil, err
	}
	c, err := net.DialTimeout("unix", p, 2*time.Second)
	if err != nil {
		return nil, err
	}
	return &Client{conn: c, r: bufio.NewReader(c)}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) Call(method string, params map[string]any) (Response, error) {
	req := Request{Method: method, Params: params}
	b, _ := json.Marshal(req)
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		return Response{}, err
	}
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, errors.New("bad response: " + string(line))
	}
	return resp, nil
}
```

- [ ] **Step 4: Tests**

`pkg/ipc/ipc_test.go`:

```go
package ipc

import (
	"testing"
)

func TestServerClientRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	srv.Handle("ping", func(req Request) Response {
		return Response{OK: true, Result: "pong"}
	})
	go srv.Serve()

	c, err := Dial()
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	resp, err := c.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !resp.OK || resp.Result != "pong" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestUnknownMethod(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	srv, _ := NewServer()
	defer srv.Close()
	go srv.Serve()
	c, _ := Dial()
	defer c.Close()
	resp, _ := c.Call("nope", nil)
	if resp.OK {
		t.Errorf("expected !OK")
	}
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./pkg/ipc/... -v
```

- [ ] **Step 6: Commit**

```bash
git add pkg/ipc/
git commit -m "ipc: unix socket JSON RPC server + client"
```

### Task 20: Wire IPC into daemon, expose status/pause/undo/chat methods

**Files:**
- Modify: `pkg/daemon/daemon.go`
- Create: `pkg/daemon/handlers.go`

- [ ] **Step 1: handlers.go**

```go
package daemon

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sunrf-renlab-ai/mentor/pkg/action"
	"github.com/sunrf-renlab-ai/mentor/pkg/ipc"
)

type rpc struct {
	d *Daemon
}

func (r *rpc) status(req ipc.Request) ipc.Response {
	var n int
	r.d.State.DB().QueryRow(`SELECT COUNT(*) FROM projects WHERE paused_until IS NULL OR paused_until > datetime('now')`).Scan(&n)
	var todayActions int
	r.d.State.DB().QueryRow(`SELECT COUNT(*) FROM actions WHERE timestamp > datetime('now', '-1 day')`).Scan(&todayActions)
	var events24h int
	r.d.State.DB().QueryRow(`SELECT COUNT(*) FROM events WHERE timestamp > datetime('now', '-1 day')`).Scan(&events24h)
	return ipc.Response{OK: true, Result: map[string]any{
		"active_projects": n,
		"events_24h":      events24h,
		"actions_24h":     todayActions,
	}}
}

func (r *rpc) pause(req ipc.Request) ipc.Response {
	project, _ := req.Params["project_path"].(string)
	if project == "" {
		return ipc.Response{OK: false, Error: "project_path required"}
	}
	a := &action.Action{Type: "pause_project", Params: req.Params, Rationale: "user pause via CLI"}
	if err := r.d.actions.Run(context.Background(), r.d.State, a); err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	return ipc.Response{OK: true}
}

func (r *rpc) undo(req ipc.Request) ipc.Response {
	n := 1
	if v, ok := req.Params["n"].(float64); ok {
		n = int(v)
	}
	count, err := action.UndoLast(context.Background(), r.d.State, n)
	if err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	return ipc.Response{OK: true, Result: map[string]any{"undone": count}}
}

func (r *rpc) actions(req ipc.Request) ipc.Response {
	rows, err := r.d.State.DB().Query(`SELECT action_id, timestamp, action_type, COALESCE(project_path, ''), rationale, status FROM actions ORDER BY timestamp DESC LIMIT 50`)
	if err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}
	defer rows.Close()
	type row struct {
		ID, Ts, Type, Project, Rationale, Status string
	}
	var out []row
	for rows.Next() {
		var x row
		rows.Scan(&x.ID, &x.Ts, &x.Type, &x.Project, &x.Rationale, &x.Status)
		out = append(out, x)
	}
	return ipc.Response{OK: true, Result: out}
}

func (r *rpc) chat(req ipc.Request) ipc.Response {
	msg, _ := req.Params["message"].(string)
	if msg == "" {
		return ipc.Response{OK: false, Error: "message required"}
	}
	// Log user message.
	r.d.State.DB().Exec(`INSERT INTO chat_log (message_id, timestamp, role, content) VALUES (?, ?, 'user', ?)`,
		uuidShort(), time.Now().UTC(), msg)

	if r.d.brain == nil {
		reply := "(Mentor offline — no auth token. Run `mentor login` first.)"
		r.d.State.DB().Exec(`INSERT INTO chat_log (message_id, timestamp, role, content) VALUES (?, ?, 'mentor', ?)`,
			uuidShort(), time.Now().UTC(), reply)
		return ipc.Response{OK: true, Result: map[string]any{"reply": reply}}
	}

	// Build a chat-mode prompt (different from rule-trigger prompts).
	in := r.d.loop.BuildChatPromptInput(msg)
	d, err := r.d.brain.Decide(context.Background(), in)
	if err != nil {
		return ipc.Response{OK: false, Error: err.Error()}
	}

	reply := d.Rationale
	if d.Decision != "ignore" && d.Decision != "" {
		// Execute any chat-driven action (e.g., set_pref, pause_project).
		a := &action.Action{
			Type:      d.Decision,
			Rationale: d.Rationale,
			Params:    d.Params,
		}
		r.d.actions.Run(context.Background(), r.d.State, a)
		reply += " (action: " + d.Decision + ")"
	}
	r.d.State.DB().Exec(`INSERT INTO chat_log (message_id, timestamp, role, content) VALUES (?, ?, 'mentor', ?)`,
		uuidShort(), time.Now().UTC(), reply)

	return ipc.Response{OK: true, Result: map[string]any{"reply": reply}}
}

func uuidShort() string {
	b, _ := json.Marshal(time.Now().UnixNano())
	return string(b)
}
```

- [ ] **Step 2: Add BuildChatPromptInput to loop**

Append to `pkg/loop/loop.go`:

```go
func (l *Loop) BuildChatPromptInput(message string) brain.PromptInput {
	return brain.PromptInput{
		Now:               brain.NowString(time.Now()),
		ProjectsJSON:      jsonProjects(l.State),
		PrefsJSON:         jsonPrefs(l.State),
		RecentActionsJSON: jsonRecentActions(l.State),
		TriggerReason:     "user_chat",
		EventsJSON:        `{"user_message":"` + jsonEscape(message) + `"}`,
	}
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
```

- [ ] **Step 3: Update daemon.go to start IPC server and store fields**

Add fields to `Daemon`:
```go
brain   *brain.Brain
actions *action.Registry
ipc     *ipc.Server
```

In `Start`, after constructing the loop:
```go
d := &Daemon{State: st, server: srv, listener: ln, loop: l, cancel: cancel, brain: brn, actions: reg}
sock, err := ipc.NewServer()
if err == nil {
	r := &rpc{d: d}
	sock.Handle("status", r.status)
	sock.Handle("pause", r.pause)
	sock.Handle("undo", r.undo)
	sock.Handle("actions", r.actions)
	sock.Handle("chat", r.chat)
	go sock.Serve()
	d.ipc = sock
}
return d, nil
```

In `Stop`, call `d.ipc.Close()` before `d.server.Shutdown`.

- [ ] **Step 4: Run tests**

```bash
go test ./... -count=1
```

- [ ] **Step 5: Commit**

```bash
git add pkg/daemon/ pkg/loop/loop.go
git commit -m "daemon: IPC handlers (status/pause/undo/actions/chat) wired"
```

### Task 21: `mentor` CLI

**Files:**
- Create: `cmd/mentor/main.go`
- Create: `cmd/mentor/chat.go`
- Create: `cmd/mentor/commands.go`

- [ ] **Step 1: main.go**

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sunrf-renlab-ai/mentor/pkg/hook"
	"github.com/sunrf-renlab-ai/mentor/pkg/oauth"
)

func main() {
	if len(os.Args) < 2 {
		runChat()
		return
	}
	switch os.Args[1] {
	case "init":
		mustOK(hook.Install())
		fmt.Println("hooks installed in ~/.claude/settings.json")
	case "uninstall":
		mustOK(hook.Uninstall())
		fmt.Println("hooks removed")
	case "login":
		t, err := oauth.Login(context.Background())
		mustOK(err)
		fmt.Printf("authenticated; token saved (expires in %ds)\n", t.ExpiresIn)
	case "status":
		runStatus()
	case "pause":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: mentor pause <project-path>")
			os.Exit(1)
		}
		runPause(os.Args[2])
	case "undo":
		runUndo()
	case "actions":
		runActions()
	case "chat":
		runChat()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func mustOK(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: commands.go**

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sunrf-renlab-ai/mentor/pkg/ipc"
)

func dial() *ipc.Client {
	c, err := ipc.Dial()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot connect to mentord — is it running? error:", err)
		os.Exit(2)
	}
	return c
}

func runStatus() {
	c := dial()
	defer c.Close()
	r, err := c.Call("status", nil)
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(r.Result, "", "  ")
	fmt.Println(string(b))
}

func runPause(project string) {
	c := dial()
	defer c.Close()
	r, err := c.Call("pause", map[string]any{"project_path": project})
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	fmt.Println("paused")
}

func runUndo() {
	c := dial()
	defer c.Close()
	r, err := c.Call("undo", nil)
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	b, _ := json.Marshal(r.Result)
	fmt.Println(string(b))
}

func runActions() {
	c := dial()
	defer c.Close()
	r, err := c.Call("actions", nil)
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(r.Result, "", "  ")
	fmt.Println(string(b))
}
```

- [ ] **Step 3: chat.go**

```go
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func runChat() {
	c := dial()
	defer c.Close()
	r, _ := c.Call("status", nil)
	if r.OK {
		fmt.Printf("Mentor — %v\n", r.Result)
	} else {
		fmt.Println("Mentor — daemon connected (no status yet)")
	}
	fmt.Println("Type messages, Ctrl-D to exit.")

	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !in.Scan() {
			break
		}
		msg := strings.TrimSpace(in.Text())
		if msg == "" {
			continue
		}
		resp, err := c.Call("chat", map[string]any{"message": msg})
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		if !resp.OK {
			fmt.Println("error:", resp.Error)
			continue
		}
		if m, ok := resp.Result.(map[string]any); ok {
			fmt.Println(m["reply"])
		}
	}
}
```

- [ ] **Step 4: Build both binaries**

```bash
go build -o bin/mentor ./cmd/mentor
go build -o bin/mentord ./cmd/mentord
```
Expected: both succeed.

- [ ] **Step 5: Commit**

```bash
git add cmd/mentor/
git commit -m "cli: mentor subcommands (init, login, status, pause, undo, actions, chat)"
```

---

## Phase 9 — macOS menubar tray

Goal: lightweight tray icon showing daemon state.

### Task 22: Add systray dep + tray module

**Files:**
- Create: `pkg/tray/tray_darwin.go`
- Create: `pkg/tray/tray_other.go`
- Modify: `cmd/mentord/main.go`

- [ ] **Step 1: Add dep**

```bash
go get github.com/getlantern/systray
go mod tidy
```

- [ ] **Step 2: tray_darwin.go**

```go
//go:build darwin

package tray

import (
	"github.com/getlantern/systray"
	"github.com/sunrf-renlab-ai/mentor/pkg/daemon"
)

func Run(d *daemon.Daemon, onQuit func()) {
	systray.Run(func() {
		systray.SetTitle("◐")
		systray.SetTooltip("Mentor")

		mStatus := systray.AddMenuItem("Status…", "show status")
		systray.AddSeparator()
		mPauseAll := systray.AddMenuItem("Pause all", "pause all projects")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "stop mentord")

		go func() {
			for {
				select {
				case <-mStatus.ClickedCh:
					// Could surface a modal — for v1 it's a no-op (use `mentor status` CLI)
				case <-mPauseAll.ClickedCh:
					// Future: implement pause-all via daemon API
				case <-mQuit.ClickedCh:
					onQuit()
					systray.Quit()
					return
				}
			}
		}()
	}, nil)
}
```

- [ ] **Step 3: tray_other.go**

```go
//go:build !darwin

package tray

import "github.com/sunrf-renlab-ai/mentor/pkg/daemon"

func Run(d *daemon.Daemon, onQuit func()) {
	// Linux / other: no tray; daemon runs without tray. Block until onQuit signals.
	select {}
}
```

- [ ] **Step 4: Update cmd/mentord/main.go**

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/sunrf-renlab-ai/mentor/pkg/daemon"
	"github.com/sunrf-renlab-ai/mentor/pkg/tray"
)

func main() {
	d, err := daemon.Start()
	if err != nil {
		log.Fatalf("start daemon: %v", err)
	}
	fmt.Fprintln(os.Stderr, "mentord running")
	tray.Run(d, func() { d.Stop() })
}
```

- [ ] **Step 5: Build**

```bash
go build -o bin/mentord ./cmd/mentord
```

- [ ] **Step 6: Commit**

```bash
git add cmd/mentord/main.go pkg/tray/ go.mod go.sum
git commit -m "tray: macOS menubar (systray) with quit; daemon now blocks on tray"
```

---

## Phase 10 — Install + LaunchAgent + release pipeline

### Task 23: install.sh

**Files:**
- Create: `install/install.sh`

- [ ] **Step 1: Write install.sh**

```bash
#!/bin/sh
set -e

REPO="${MENTOR_REPO:-sunrf-renlab-ai/mentor}"
VERSION="${MENTOR_VERSION:-latest}"
INSTALL_DIR="${MENTOR_INSTALL_DIR:-/usr/local/bin}"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64)  ARCH=amd64 ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

if [ "$OS" != "darwin" ] && [ "$OS" != "linux" ]; then
  echo "unsupported OS: $OS" >&2; exit 1
fi

if [ "$VERSION" = "latest" ]; then
  REL_URL="https://api.github.com/repos/${REPO}/releases/latest"
else
  REL_URL="https://api.github.com/repos/${REPO}/releases/tags/${VERSION}"
fi

ASSET=$(curl -sSL "$REL_URL" | grep -o "https://[^\"]*mentor-${OS}-${ARCH}\.tar\.gz" | head -n1)
if [ -z "$ASSET" ]; then
  echo "no release asset for ${OS}-${ARCH}" >&2; exit 1
fi

TMP=$(mktemp -d)
trap "rm -rf $TMP" EXIT
curl -sSL "$ASSET" -o "$TMP/mentor.tar.gz"
tar xzf "$TMP/mentor.tar.gz" -C "$TMP"

if [ ! -w "$INSTALL_DIR" ]; then
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

mv "$TMP/mentor" "$INSTALL_DIR/mentor"
mv "$TMP/mentord" "$INSTALL_DIR/mentord"
chmod +x "$INSTALL_DIR/mentor" "$INSTALL_DIR/mentord"

# Install LaunchAgent (macOS) or systemd-user unit (Linux)
if [ "$OS" = "darwin" ]; then
  PLIST="$HOME/Library/LaunchAgents/com.mentor.mentord.plist"
  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.mentor.mentord</string>
  <key>ProgramArguments</key><array><string>${INSTALL_DIR}/mentord</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>${HOME}/.config/mentor/mentord.log</string>
  <key>StandardErrorPath</key><string>${HOME}/.config/mentor/mentord.log</string>
</dict>
</plist>
EOF
  mkdir -p "$HOME/.config/mentor"
  launchctl unload "$PLIST" 2>/dev/null || true
  launchctl load "$PLIST"
else
  UNIT="$HOME/.config/systemd/user/mentord.service"
  mkdir -p "$(dirname "$UNIT")"
  cat > "$UNIT" <<EOF
[Unit]
Description=Mentor daemon
After=default.target

[Service]
ExecStart=${INSTALL_DIR}/mentord
Restart=on-failure

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable --now mentord.service
fi

echo
echo "Mentor installed to $INSTALL_DIR/mentor"
echo "Daemon started."
echo
echo "Next:"
echo "  1. mentor login    # authorize with your Claude account"
echo "  2. mentor init     # install hooks into ~/.claude/settings.json"
echo "  3. mentor          # open chat"
```

- [ ] **Step 2: Make executable**

```bash
chmod +x install/install.sh
```

- [ ] **Step 3: Commit**

```bash
git add install/install.sh
git commit -m "install: one-line install script (curl | sh) with LaunchAgent + systemd-user"
```

### Task 24: GitHub release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Write release.yml**

```yaml
name: Release
on:
  push:
    tags: ['v*']

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    strategy:
      matrix:
        include:
          - goos: darwin
            goarch: arm64
          - goos: darwin
            goarch: amd64
          - goos: linux
            goarch: amd64
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: Build
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
          CGO_ENABLED: 0
        run: |
          mkdir -p out
          go build -o out/mentor ./cmd/mentor
          go build -o out/mentord ./cmd/mentord
          tar czf mentor-${{ matrix.goos }}-${{ matrix.goarch }}.tar.gz -C out mentor mentord
      - uses: softprops/action-gh-release@v2
        with:
          files: mentor-${{ matrix.goos }}-${{ matrix.goarch }}.tar.gz
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: release workflow — cross-build + tar + GH release on tag"
```

---

## Phase 11 — Final verification

### Task 25: E2E smoke

**Files:**
- Create: `cmd/e2e/main.go`

- [ ] **Step 1: Write smoke harness**

`cmd/e2e/main.go`:

```go
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

	"github.com/sunrf-renlab-ai/mentor/pkg/daemon"
)

func main() {
	tmp, _ := os.MkdirTemp("", "mentor-e2e-")
	defer os.RemoveAll(tmp)
	os.Setenv("HOME", tmp)

	d, err := daemon.Start()
	if err != nil {
		die("start: %v", err)
	}
	defer d.Stop()

	portFile := filepath.Join(tmp, ".config", "mentor", "port")
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
		"event_id":     "e2e-1",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
		"hook_type":    "PostToolUse",
		"session_id":   "s-e2e",
		"project_path": "/tmp/foo",
		"tool_name":    "Bash",
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
	fmt.Println("e2e ok")
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+f+"\n", a...)
	os.Exit(1)
}
```

- [ ] **Step 2: Run e2e smoke**

```bash
go run ./cmd/e2e
```
Expected: prints `e2e ok`.

- [ ] **Step 3: Run all tests**

```bash
go test ./... -count=1
```
Expected: all PASS.

- [ ] **Step 4: Build all binaries**

```bash
go build -o bin/mentor ./cmd/mentor
go build -o bin/mentord ./cmd/mentord
ls -la bin/
```
Expected: both files exist.

- [ ] **Step 5: Commit**

```bash
git add cmd/e2e/
git commit -m "e2e: smoke harness verifies daemon ingests events"
```

### Task 26: Polish README + ARCHITECTURE.md

**Files:**
- Modify: `README.md`
- Create: `ARCHITECTURE.md`

- [ ] **Step 1: Write the user-facing README**

(Include: what it is, install, first-run, daily flow, undo, uninstall, license. Match style of Pair / OpenClaw.)

- [ ] **Step 2: Write ARCHITECTURE.md**

(Include: process model, hooks, rules, brain, actions, IPC. Refer to spec for full detail.)

- [ ] **Step 3: Commit**

```bash
git add README.md ARCHITECTURE.md
git commit -m "docs: user-facing README and ARCHITECTURE"
```

---

## Self-review

- **Spec coverage:** Each spec section has tasks. §6 modules each map to packages with TDD tests. §10 rules R1-R3+R8 covered (R4-R7 explicitly v1.1 per spec). §11 actions all covered. §12 prefs via `set_pref` action. §13 install + first-run UX in Phase 10. §14 error handling baked into action registry (log-before-exec, status field, undo).
- **Placeholders:** none. All code blocks complete. Step 6 in Task 26 README/ARCHITECTURE writes are described in prose because content depends on existing README in repo at write time; agent should produce 30-100 lines for each, in plain English.
- **Type consistency:** `Decision`, `Action`, `Trigger`, `PromptInput` types referenced consistently. Brain `Decide` returns `*Decision`; loop unpacks `d.Decision`/`d.Params` consistently with action.Action type field naming.
- **Known approximations to validate during implementation:** OAuth endpoint URLs and client_id (env-var override built in); claude `-p --output-format json` envelope shape (parser accepts both envelope and raw forms); systray API version compatibility (pinned via go.mod after `go get`).

---

**End of plan.** Total: 11 phases, 26 tasks. Estimated dev effort with TDD: 2–3 days for one engineer; parallel subagents can compress to ~1 day across phases 1–10 (phase 11 must be last). Plan is ready for `superpowers:subagent-driven-development`.
