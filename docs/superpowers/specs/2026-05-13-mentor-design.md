# Mentor — Design Spec

**Date:** 2026-05-13
**Status:** Draft, pending user approval
**Repo:** `~/project/mentor` (open-source, will publish on GitHub)

---

## 1. What it is

**Mentor** is an autonomous, real-time, cross-project AI project manager + technical mentor for developers running many parallel Claude Code (and compatible CLI agent) projects on one machine.

It is a single Go binary that:

- runs as a background daemon (LaunchAgent on macOS, systemd user unit on Linux)
- silently injects hooks into every Claude Code session
- watches all your active projects in real time
- decides on its own when something matters (cheap heuristics first, LLM second)
- takes action on its own (spawns helper Claude sessions, runs cross-project syncs, triggers macOS notifications)
- logs everything so you can undo
- exposes ONE conversational CLI (`mentor`) where you can shape its behavior in natural language

Use only your existing Claude subscription — no API key, no extra LLM cost.

## 2. Why now

Every existing tool in this niche stops at one of two layers:
- **Display** (Conductor, Claude Squad, Vibe Kanban): show many sessions, do not reason
- **Periodic scan** (cron + summary tools): batch reports, not real-time, not interventional

There is no tool that is **always on, always watching, always reasoning, autonomous to act**. That is the gap Mentor fills.

The user runs 10+ parallel Claude Code projects daily. Cognitive overhead of remembering "where did I leave each one, what's the cross-project blast radius of this change, what should I do next" is the bottleneck. Mentor is the fix.

## 3. Product principles

1. **Real-time, not periodic.** Local rules trigger sub-second. LLM reasoning runs within seconds when needed.
2. **Maximum autonomy.** Daemon acts first, logs after. No "are you sure?" dialogs by default.
3. **Zero per-project setup.** A new project is auto-discovered and watched the moment a Claude session runs in it.
4. **Zero LLM marginal cost.** All reasoning goes through `claude -p` subprocesses, billed against the user's existing Claude subscription via OAuth (`mentor login`).
5. **One conversational surface.** All user influence on Mentor's behavior happens in `mentor` CLI chat. No GUI forms.
6. **Single binary, single config dir.** No runtime deps. `~/.config/mentor/` for state, `~/.config/mentor/sock` for IPC.
7. **Reversible.** Every action is logged with enough info to undo. `mentor undo` rolls back the last N actions.

## 4. User stories (the "kinds of moments")

These are the situations Mentor is built to handle. Each maps to specific rule + action behavior in §10.

| Moment | Mentor's action |
|---|---|
| You commit a schema change in project A; project B imports A's types | Auto-spawn `claude -p --add-dir B` to update B; notify "synced B with A's schema change" |
| Test suite fails twice in a row in any project | Notify with stack trace summary; do NOT auto-fix (high risk) |
| `claude` Stop hook fires in a project, then no activity for 30 min | Mentor "checks in": "Did you finish X in project Y, or did it stall?" via notification |
| You start working in project C while project A's deploy is still running | Notify "deploy A still in progress, last status: pending 3 min" |
| You ask `mentor` "what's going on", "what did you do today", "stop bugging me about X", "I'm focused on Y this week" | Conversational handling: queries, prefs, pauses |
| Two projects have a same-named file (e.g. `schema.sql`) and one just changed | Notify potential drift, do NOT auto-merge |
| You have not touched project D in 14 days but it still has uncommitted changes | Weekly digest mention: "D is sitting on uncommitted work, archive or resume?" |

## 5. High-level architecture

```
[~/.claude/settings.json — global hooks injected by `mentor init`]
   ├─ UserPromptSubmit hook ─┐
   ├─ PostToolUse hook       ├─→ HTTP POST → 127.0.0.1:<port>/event
   └─ Stop hook              ─┘
                                       │
                                       ▼
   ┌─────────────────────────────────────────────────────────┐
   │  mentord  (Go single binary, LaunchAgent / systemd-user) │
   │                                                         │
   │   Ingestor → Rule gate → LLM brain → Action executor    │
   │   (HTTP)    (Go pure)   (claude -p)  (claude -p +       │
   │      │         │           │           macOS notify)    │
   │      ▼         ▼           ▼              ▲             │
   │  ┌───────────────────────────────────────┐│             │
   │  │  SQLite (~/.config/mentor/state.db)   │┘             │
   │  │  events / projects / actions /        │              │
   │  │  inferred_priorities / user_prefs /   │              │
   │  │  chat_log / paused_projects           │              │
   │  └───────────────────────────────────────┘              │
   │           ▲                                             │
   │           │ unix socket: ~/.config/mentor/sock          │
   │           │ (line-delimited JSON RPC)                   │
   └───────────┼─────────────────────────────────────────────┘
               │
       ┌───────┴────────┬─────────────────┐
       ▼                ▼                 ▼
   `mentor` CLI    menubar tray       OAuth helper
   (chat REPL,    (Go systray lib,   (`mentor login`,
    status,        emits menu        opens browser,
    pause, undo)   from daemon       catches OAuth
                   state)             callback, stores
                                     token in keyring)
```

### 5.1 Process model

There are **at most two processes**:

1. **`mentord`** — the daemon. Always running. LaunchAgent.
2. **`mentor`** — short-lived CLI. Connects to daemon via unix socket. The chat REPL is a `mentor` process.

The menubar tray is part of `mentord` (uses `getlantern/systray` library, runs in main goroutine on macOS). On Linux first ship, no tray; only notifications via `notify-send`.

### 5.2 Why two processes only

- LaunchAgent's restart-on-crash works cleanly with one binary
- `mentor` CLI being short-lived (REPL exits cleanly with Ctrl-D) means no zombie state
- Tray + ingestor + reasoning + state in one daemon = trivial state consistency, no IPC between subsystems

## 6. Module breakdown

Each module is its own Go package, isolated, independently testable.

### 6.1 `pkg/hook`
Generates and installs hook entries in `~/.claude/settings.json`. The hook is a tiny shell script that reads the daemon port from `~/.config/mentor/port` and POSTs the hook payload to `127.0.0.1:<port>/event` with a 200ms timeout. Failure to reach daemon must NOT block the Claude session — hooks fail open.

The daemon binds to an OS-assigned ephemeral port at startup and writes it to `~/.config/mentor/port` (atomic rename). Avoids fixed-port collision; stays robust across reinstall.

Public API:
- `Install() error` — idempotent, merges with existing hooks
- `Uninstall() error` — removes only mentor's hooks, leaves others
- `IsInstalled() (bool, error)`

### 6.2 `pkg/ingest`
HTTP server on `127.0.0.1:<port>`. One endpoint: `POST /event`. Validates payload schema, writes to `events` table, returns 200 immediately. No reasoning here — pure intake.

Payload schema (§7).

### 6.3 `pkg/rules`
Pure-Go heuristic gate. Reads recent events + project state from SQLite, returns `[]Trigger`. A Trigger says "this signal is interesting, here is the context bundle for the LLM". Returns empty slice = ignore.

Initial rule set in §10. Rules are pure functions of state — easy to test, easy to add.

### 6.4 `pkg/brain`
Spawns `claude -p` with a packaged prompt + context bundle. Parses LLM output (structured JSON via `--output-format json`). Output is a `Decision` struct: action type, parameters, rationale.

`brain` does not execute actions. It only decides.

### 6.5 `pkg/action`
Executes Decisions. Each action type is a pure function with a clear contract. Catalog in §11. Logs every action to `actions` table BEFORE executing (so even a crash mid-execute leaves a trace).

### 6.6 `pkg/notify`
macOS: `osascript -e 'display notification ...'` with optional sound. Supports click handler URL — clicking a notification runs `mentor show <action_id>`. Linux: `notify-send`.

### 6.7 `pkg/state`
Single SQLite handle, all DB access goes through this. Exposes typed query methods, no string SQL leaks elsewhere. Migrations are embedded `.sql` files run on daemon startup.

### 6.8 `pkg/ipc`
Unix socket server (daemon side) + client (CLI side). Line-delimited JSON. Methods: `chat`, `status`, `pause`, `resume`, `undo`, `actions`, `menu` (for tray).

### 6.9 `pkg/oauth`
Implements Anthropic's OAuth setup-token flow. `mentor login` opens browser to `https://claude.ai/oauth/authorize?...`, runs a one-off `127.0.0.1:<random>` listener for the callback, stores the token in macOS Keychain (`security` shell-out) or Linux Secret Service (`secret-tool`).

Token is read by `pkg/brain` when spawning `claude -p`. Two-tier mechanism (first that works wins):
1. Set env `ANTHROPIC_AUTH_TOKEN=<oauth-token>` for the subprocess
2. Fallback: write a temporary `~/.config/mentor/claude-creds.json` in the format `claude` CLI expects, point `XDG_CONFIG_HOME` at our config dir for the subprocess

Implementation step 4 (`pkg/oauth`) includes a one-day spike to verify which mechanism `claude` CLI honors for the current version, and locks in the choice.

### 6.10 `pkg/tray`
Wraps `getlantern/systray`. Reads daemon state, renders menubar items: project list with status badges, "Pause all", "Open chat", "Quit". Pure presenter — no business logic.

### 6.11 `cmd/mentord`
Daemon entrypoint. Wires modules. LaunchAgent target.

### 6.12 `cmd/mentor`
CLI entrypoint. Subcommands: `init`, `login`, `chat` (default if no subcommand), `status`, `pause`, `resume`, `undo`, `actions`, `logs`, `uninstall`.

## 7. Event payload schema

What every Claude hook POSTs to `/event`:

```json
{
  "event_id": "uuid",
  "timestamp": "2026-05-13T14:32:11Z",
  "hook_type": "UserPromptSubmit | PostToolUse | Stop",
  "session_id": "claude-session-uuid",
  "project_path": "/Users/blink/project/agora",
  "git_branch": "main",
  "git_head_sha": "abc123",
  "tool_name": "Bash | Edit | Write | Read | ...",  // PostToolUse only
  "tool_input_summary": "first 200 chars",          // PostToolUse only
  "tool_result_summary": "first 200 chars",         // PostToolUse only
  "tool_exit_status": "ok | error",                  // PostToolUse only
  "user_prompt_summary": "first 200 chars",         // UserPromptSubmit only
  "stop_reason": "user_stop | turn_complete"        // Stop only
}
```

The hook script computes summaries (truncating) before POSTing — daemon never sees full prompts/files. Privacy by truncation.

## 8. SQLite schema

```sql
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
  display_name     TEXT,                  -- inferred from dir name
  first_seen       TIMESTAMP NOT NULL,
  last_active      TIMESTAMP NOT NULL,
  inferred_focus   TEXT,                  -- LLM-summarized "what is this project"
  paused_until     TIMESTAMP              -- NULL = active
);

CREATE TABLE actions (
  action_id        TEXT PRIMARY KEY,
  timestamp        TIMESTAMP NOT NULL,
  action_type      TEXT NOT NULL,         -- spawn_session | notify | sync_files | retry | ...
  project_path     TEXT,                  -- NULL for cross-project actions
  trigger_event_id TEXT,                  -- which event triggered this
  rationale        TEXT NOT NULL,         -- LLM's reason
  parameters_json  TEXT NOT NULL,
  status           TEXT NOT NULL,         -- pending | running | done | failed | undone
  result_summary   TEXT,
  undo_payload     TEXT                    -- enough info to reverse
);
CREATE INDEX idx_actions_ts ON actions(timestamp DESC);

CREATE TABLE user_prefs (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  set_at TIMESTAMP NOT NULL,
  source TEXT NOT NULL                     -- chat | cli_flag | default
);

CREATE TABLE chat_log (
  message_id TEXT PRIMARY KEY,
  timestamp  TIMESTAMP NOT NULL,
  role       TEXT NOT NULL,               -- user | mentor
  content    TEXT NOT NULL
);
```

`user_prefs` examples (set via chat):
- `priority.pair = high`
- `priority.socialmind = paused_until:2026-06-13`
- `rule.deploy_fail = notify_no_retry`
- `notification.quiet_hours = 22:00-08:00`

## 9. LLM brain — prompt structure

Every `claude -p` invocation by `pkg/brain` uses this template:

```
You are Mentor — an autonomous cross-project assistant for a developer running
many parallel Claude Code projects.

CURRENT TIME: {{ts}}
ACTIVE PROJECTS: {{json list of projects with last_active and inferred_focus}}
USER PREFERENCES: {{json from user_prefs table}}
RECENT ACTIONS YOU TOOK (last 24h): {{json from actions table}}

TRIGGER REASON: {{which rule fired}}
TRIGGER CONTEXT: {{events, file diffs, git state — bundled by rules pkg}}

Your job: decide ONE of:
  - "ignore"  (context turned out to be noise)
  - "notify"  (tell user via macOS notification)
  - "spawn_session"  (launch a helper claude -p in a target project)
  - "sync_files" (write a cross-project note to ~/.config/mentor/notes/)
  - "ask"     (only if you genuinely cannot decide — uses notification)

Output JSON:
  {"decision": "...", "rationale": "...", "params": {...}}

Constraints:
- Never decide "ignore" without a one-line rationale.
- Never propose an action that would modify user files outside the trigger project unless the trigger is an explicit cross-project signal.
- Respect quiet_hours for notify decisions (downgrade to "ignore" + log).
```

The prompt is a Go template, kept in `pkg/brain/prompt.tmpl`. Tunable without code changes.

## 10. Initial rule set (`pkg/rules`)

Each rule is `func(ctx) []Trigger`. Triggers fired only if rule's condition matches.

| Rule | Condition | Trigger context to LLM |
|------|-----------|------------------------|
| `R1.tool_error_burst` | 2+ PostToolUse with `exit_status=error` for same tool in same session within 5 min | Last 5 events of that session |
| `R2.test_fail` | PostToolUse where `tool_input_summary` matches `/(npm test|bun test|go test|pytest)/` and `exit_status=error` | The test command + output summary |
| `R3.deploy_fail` | PostToolUse where input matches `/(vercel deploy|render deploys|fly deploy|gh workflow)/` and `exit_status=error` | Command + output |
| `R4.commit_in_dependency` | A `git commit` in project A, AND project B has imported types/files from A in last 7 days (heuristic via `grep` on B for path containing A's name) | Both projects' recent state |
| `R5.session_idle` | Stop event, then no event for that session for 30+ min, AND no commit in that project | Last 10 events of session |
| `R6.cross_project_filename` | Edit/Write to `schema.sql` / `types.ts` / `.env.example` in project A, when same filename exists in another active project | Both files' diff |
| `R7.uncommitted_stale` | Once daily at 09:00, list projects with uncommitted changes older than 14 days | Project list |
| `R8.periodic_overview` | Every 30 min, regardless of events, run a "any blind spots?" pass | All projects' last_active + inferred_focus |

Rules are added incrementally; v1 ships with R1-R3 + R8 only (most ROI, least false positives). R4-R7 in v1.1.

## 11. Action catalog (`pkg/action`)

| Action | Effect | Undo |
|--------|--------|------|
| `notify` | macOS notification, optional sound, optional click-handler URL | n/a (passive) |
| `spawn_session` | Run `claude -p --add-dir <project> "<prompt>"` as detached subprocess; capture stdout to `actions.result_summary` | Kill subprocess if still running |
| `sync_files` | Append to `~/.config/mentor/notes/<topic>.md`; intended as persistent cross-project memory the user can read | Truncate the appended block |
| `retry` | Re-run the failed command (only used if user prefs explicitly enable per-rule) | n/a (passive) |
| `pause_project` | Set `projects.paused_until` | Clear `paused_until` |
| `set_pref` | Insert into `user_prefs` | Restore previous value (kept in `undo_payload`) |

`mentor undo` reverses the last N actions in reverse order, calling each action's undo function.

## 12. Configuration & user prefs

No config file. All configuration lives in SQLite `user_prefs` table. Set via:

- `mentor` chat (natural language: "stop bugging me about socialmind" → LLM-mediated set_pref action)
- `mentor pref set <key> <value>` (escape hatch for scripted setup)

Defaults are baked into the binary. `mentor pref reset` wipes all overrides.

Watched roots default: `$HOME/project`, `$HOME/dev`, `$HOME/code`, `$HOME/src`, `$HOME/repos`. Plus any path where a Claude session has been seen (auto-discovery via the hook payload's `project_path`). Override via `pref set watched_roots ...`.

## 13. Install & first-run UX

### Install (one line)

```bash
curl -fsSL https://mentor.sh/install | sh
```

The script:
1. Detects platform (darwin-arm64 / darwin-amd64 / linux-amd64)
2. Downloads matching binary from latest GitHub Release
3. Installs to `/usr/local/bin/mentor` (or `~/.local/bin/mentor` if no sudo)
4. Prints next-step: `mentor login`

(For v1, `mentor.sh` is a Cloudflare Worker that 302s to the GitHub Release `install.sh` raw URL. Buy `mentor.sh` separately, but install works without the domain via direct GitHub URL.)

### First run

```bash
$ mentor login
Opening browser to authorize Mentor to use your Claude account...
[browser opens to claude.ai OAuth consent]
[user clicks Authorize]
✓ Authenticated. Token stored in macOS Keychain.

$ mentor init
Installing hooks to ~/.claude/settings.json... ✓
Starting daemon (LaunchAgent: com.mentor.mentord)... ✓
Watching: ~/project/* (5 projects with .claude/ found)

You're set. Run `mentor` to chat.

$ mentor
Mentor v0.1 — connected to daemon, watching 5 projects.

>
```

### Subsequent days

Daemon is always on. User just opens projects and runs `claude` as usual. Notifications appear when warranted. `mentor` for chat anytime.

## 14. Error handling & safety

| Failure mode | Behavior |
|--------------|----------|
| Daemon down when hook fires | Hook script silently fails (200ms timeout); session unaffected |
| `claude` CLI not found | Daemon logs error, sends macOS notification "Mentor cannot find claude CLI", suspends LLM brain (rules still log events to SQLite) |
| OAuth token expired/revoked | Brain returns "ignore" + logs; next `mentor` chat surfaces "please re-run mentor login" |
| LLM returns malformed JSON | Action defaults to `notify` with the raw output, so user sees what happened |
| Action execution fails | Action status → `failed`, full error in `result_summary`, surfaces in `mentor actions` |
| SQLite locked | Retry with backoff up to 5s, then drop the event with a logged warning |
| Disk full | Daemon refuses new events, sends notification, exits gracefully |
| User runs `mentor uninstall` | Removes hooks from `~/.claude/settings.json`, stops & removes LaunchAgent, leaves SQLite intact (user can `rm -rf ~/.config/mentor` to fully wipe) |

**Safety invariants:**
- Daemon never writes to user project files except via `spawn_session` (which is just running `claude` — same trust boundary as the user running it)
- Daemon never executes shell commands directly except `claude -p` and `osascript`/`notify-send`
- Hooks fail open (never block Claude session)
- All actions are logged before execution, so a daemon crash mid-action leaves a forensic trace

## 15. Testing strategy

Per superpowers TDD discipline:

- `pkg/rules` — pure functions, table-driven tests with fake SQLite state. 100% coverage on rules is realistic and the highest ROI to test.
- `pkg/state` — integration tests against real SQLite tempfile.
- `pkg/ingest` — HTTP test server, fixed payloads, assert DB writes.
- `pkg/brain` — fake `claude` CLI subprocess (a shell script that echoes a fixture JSON), test that brain wires context correctly and parses output.
- `pkg/action` — each action has a `dryRun` flag for tests; assert side effects via fake notifier / fake spawner.
- `pkg/oauth` — fake OAuth server, full flow test.
- `pkg/hook` — write to a tempfile JSON, assert merge behavior preserves existing entries.
- `cmd/mentor` (CLI) — golden-file tests on REPL transcripts using a fake daemon socket.
- End-to-end smoke test: spin up daemon, fire a synthetic hook event, assert action recorded. Run in CI.

Minimum bar to ship v1: every `pkg/*` has tests; e2e smoke green.

## 16. Open-source repo layout

```
mentor/
├── cmd/
│   ├── mentor/        # CLI binary
│   └── mentord/       # daemon binary
├── pkg/
│   ├── hook/
│   ├── ingest/
│   ├── rules/
│   ├── brain/
│   │   └── prompt.tmpl
│   ├── action/
│   ├── notify/
│   ├── state/
│   │   └── migrations/
│   ├── ipc/
│   ├── oauth/
│   └── tray/
├── install/
│   ├── install.sh
│   ├── com.mentor.mentord.plist     # LaunchAgent template
│   └── mentor.service                # systemd-user template
├── docs/
│   ├── README.md
│   ├── ARCHITECTURE.md
│   └── superpowers/
│       ├── specs/
│       │   └── 2026-05-13-mentor-design.md  (this file)
│       └── plans/
├── .github/
│   └── workflows/
│       ├── ci.yml                    # test+build on every push
│       └── release.yml                # cross-build + release on tag
├── go.mod
├── LICENSE                            # MIT
└── README.md
```

Build: `go build -o bin/mentor ./cmd/mentor && go build -o bin/mentord ./cmd/mentord`. Cross-build via `GOOS/GOARCH` matrix in `release.yml`.

## 17. Out of scope for v1

Explicitly NOT in v1, to keep ship-able:

- Linux menubar tray (only notify-send)
- Windows support
- Web UI / mobile app
- Multi-user / team sharing
- Cloud sync of state
- Plugin system for custom rules (rules are Go-compiled in v1)
- Other CLI agents (Codex, OpenClaw, Hermes) — only Claude Code in v1
- Workspace concept (each project is independent in v1)
- Memory of long-term user goals (only this-week priority)

These are roadmap items, tracked separately, NOT in v1 scope.

## 18. Implementation order (preview — full plan is next document)

1. Skeleton: monorepo layout, `pkg/state` with migrations, basic CI
2. `pkg/ingest` + `pkg/hook` — events flowing in end-to-end (no LLM yet)
3. `pkg/rules` with R1-R3 + R8
4. `pkg/oauth` + `mentor login`
5. `pkg/brain` (with fake LLM in tests, real `claude -p` in dev)
6. `pkg/action` — `notify` first, then `spawn_session`, then rest
7. `pkg/ipc` + `cmd/mentor` chat REPL
8. `pkg/tray` (macOS only for v1)
9. `install/install.sh` + LaunchAgent + GitHub release pipeline
10. README + ARCHITECTURE.md + 1-line install demo gif

Each step shippable independently and testable in isolation.

---

**End of design spec.** Awaiting user approval before invoking `superpowers:writing-plans`.
