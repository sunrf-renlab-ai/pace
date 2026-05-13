# Architecture

This document is a hands-on tour of how Pace is wired together. For the design rationale, see `docs/superpowers/specs/2026-05-13-pace-design.md`.

## Process model

There are at most two processes:

- **`paced`** — the daemon. Always running. LaunchAgent on macOS, `systemd --user` on Linux.
- **`pace`** — short-lived CLI. Connects to daemon via unix socket. The chat REPL is a `pace` process.

The menubar tray on macOS runs inside `paced` (the systray library requires the main goroutine). On Linux there is no tray; the daemon runs headless.

## Data flow

```
~/.claude/settings.json           ── pace-managed hook entries write events to:
   ├─ UserPromptSubmit  ─┐
   ├─ PostToolUse       ├──── POST /event → 127.0.0.1:<port>
   └─ Stop              ─┘
                              │
                              ▼
   ┌────────────────────────────────────────────────────────────────────┐
   │  paced  (Go single binary)                                       │
   │                                                                    │
   │   Ingestor  →  Rule gate  →  LLM brain  →  Action executor         │
   │   (HTTP)      (pure Go)     (claude -p   (notify / spawn /         │
   │     │           │           subprocess)   sync / set_pref / etc.)  │
   │     ▼           ▼              ▼              ▲                    │
   │  ┌────────────────────────────────────────┐   │                    │
   │  │  SQLite (~/.config/pace/state.db)    │───┘                    │
   │  └────────────────────────────────────────┘                        │
   │           ▲                                                        │
   │           │ unix socket: ~/.config/pace/sock                     │
   │           │ (line-delimited JSON RPC)                              │
   └───────────┼────────────────────────────────────────────────────────┘
               │
               ▼
          `pace` CLI (chat, status, pause, undo, actions)
```

## Modules

Each Go package has one responsibility. All are independently testable.

| Package          | What it does |
|------------------|-------------|
| `pkg/state`      | Opens SQLite + runs embedded migrations. Exposes `*State` to the world. |
| `pkg/ingest`     | HTTP `/event` endpoint; validates payload, writes to `events` table, upserts `projects`. |
| `pkg/hook`       | Idempotently merges Pace's hook entries into `~/.claude/settings.json`; respects existing hooks. |
| `pkg/rules`      | Pure-Go heuristics. Each `Rule.Evaluate(state, now)` returns 0+ `Trigger`s. v0.4 ships R1, R2, R3, R8, R9, R10, R11 (commit review), R15 (mentor pulse). |
| `pkg/brain`      | Spawns `claude -p` with a packaged prompt, parses the JSON `Decision`. Implements `loop.Decider`. The prompt has three modes: reactive (R1-3, R8), proactive PM (R9, R10), **mentor** (R11, R15, cli:ask/review/consult) — mentor mode runs an adversarial self-critique pass and outputs only opinions that survive. 5-min subprocess timeout for code-reading reviews. |
| `pkg/pm`         | v0.3 project-management layer: per-project goals, current focus declaration, generated plan documents. Pure data + persistence — no LLM calls. |
| `pkg/mentor`     | v0.4 mentor layer: durable structured opinions (observation, concern, recommendation, confidence, evidence refs) with ack/dismiss lifecycle. Pure data + persistence. |
| `pkg/action`     | Action registry + executors (`notify`, `spawn_session`, `sync_files`, `pause_project`, `set_pref`, `generate_plan`, `mentor_review`); each action logged BEFORE execution. `generate_plan` writes markdown to `~/.config/pace/plans/`. `mentor_review` saves N opinions to `mentor_opinions` and notifies once with a summary. |
| `pkg/notify`     | OS notification backend (`osascript` on macOS, `notify-send` on Linux). Build tags. |
| `pkg/loop`       | Glues rules → brain → action with a 30-second ticker. Degrades to direct notify when brain is nil. |
| `pkg/ipc`        | Unix socket JSON-RPC server + client. CLI talks to daemon over this. |
| `pkg/oauth`      | Optional PKCE flow against Anthropic OAuth endpoints (env-overridable). Tokens live at `~/.config/pace/auth.json` mode `0600`. |
| `pkg/tray`       | macOS menubar (`getlantern/systray`); no-op on Linux. |
| `pkg/daemon`     | Composition root: opens state, binds ephemeral HTTP port, writes the port file, wires loop+brain+actions+IPC. |
| `cmd/pace`     | CLI: `init`, `login`, `status`, `pause`, `undo`, `actions`, `chat`, v0.3 `plan`, `standup`, `focus`, `goal`, `goals`, v0.4 `mentor`, `ask`, `review`, `consult`. |
| `cmd/paced`    | Daemon entrypoint. Calls `daemon.Start()`, runs tray (macOS) or waits on signals. |
| `cmd/e2e`        | Smoke harness: spins up daemon, posts a synthetic event, verifies it lands in SQLite. |

## Key invariants

1. **Daemon never writes user project files directly.** The only writes outside `~/.config/pace/` go through `spawn_session`, which is just `claude -p` — same trust boundary as the user invoking it themselves.
2. **Hooks fail open.** The hook script POSTs with a 500ms timeout and exits 0 regardless. If `paced` is down, Claude sessions are not affected.
3. **Actions are logged before they execute.** `actions.Run` inserts the row with `status='pending'` first, then runs the executor, then updates to `done` or `failed`. A daemon crash mid-execute leaves a forensic trace.
4. **All timestamps are UTC at the SQL boundary.** `Loop.Once` calls `now.UTC()` before passing to rules. `ingest.store` converts incoming timestamps to UTC. This avoids lexicographic comparison failures in SQLite TEXT-stored times.
5. **Single writer to SQLite.** The daemon is the only process that opens the DB for write. CLI talks to daemon over the socket; it never opens the DB.
6. **Daemon binds an ephemeral port.** Port number written to `~/.config/pace/port` (atomic rename). The hook script reads this file at every invocation, so the port can change across restarts without breaking anything.

## Reasoning loop (the heart)

Every 30 seconds, `Loop.Once(ctx, now)` does:

1. Normalize `now` to UTC.
2. For each rule in `rules.All()`:
   - `triggers := rule.Evaluate(ctx, state, now)`
   - For each trigger:
     - If brain is nil, run a `notify` action directly with the trigger's reason as the body. Done.
     - Otherwise, build a `DeciderInput` (current projects, user prefs, recent actions, trigger context), call `brain.Decide()`, parse the `Decision`, and run the corresponding action.

Rules in v0.2:

- **R1** (`r1_tool_error.go`) — same session, ≥2 PostToolUse events with `tool_exit_status=error` within 5 min.
- **R2** (`r2_test_fail.go`) — PostToolUse where the command matches a test runner regex AND exit status is error, within 2 min.
- **R3** (`r3_deploy_fail.go`) — PostToolUse where the command matches a deploy command regex AND exit status is error, within 2 min.
- **R8** (`r8_overview.go`) — every 30 min, fire a "strategic sweep" trigger so the brain can look across all projects.

## Auth model

Pace's brain spawns `claude -p` as a subprocess. There are two sources of auth for that subprocess (in order of preference):

1. **Inherited from the user's shell** — `os.Environ()` passes through, so if the user has `claude setup-token`'d into Claude Code, Pace inherits.
2. **Pace-specific token** — if `~/.config/pace/auth.json` exists (via `pace login`), `pkg/oauth.LoadAuthEnv()` returns `{ANTHROPIC_AUTH_TOKEN: <token>}` which the daemon merges into the subprocess env.

Either works. Both can coexist (the Pace-specific token wins because it's added later).

## State persistence

```sql
CREATE TABLE events (event_id PK, timestamp, hook_type, session_id, project_path, payload_json);
CREATE TABLE projects (project_path PK, display_name, first_seen, last_active, inferred_focus, paused_until);
CREATE TABLE actions (action_id PK, timestamp, action_type, project_path, trigger_event_id, rationale, parameters_json, status, result_summary, undo_payload);
CREATE TABLE user_prefs (key PK, value, set_at, source);
CREATE TABLE chat_log (message_id PK, timestamp, role, content);
```

Migrations are embedded `.sql` files under `pkg/state/migrations/` and run on every daemon start. WAL mode + 5-second busy timeout.

## Testing

```bash
go test ./... -race -count=1     # all packages
go run ./cmd/e2e                 # end-to-end daemon smoke
```

Each rule has table-driven unit tests. The brain has a fake `claude` binary (`testdata/fakeclaude/`) that emits canned JSON. OAuth has a stub HTTP server. IPC has a server↔client round-trip test that uses a short unix-socket path (macOS limits these to ~104 chars).

## Roadmap

- v0.3: R4–R7 (cross-project drift, stale uncommitted, etc.)
- v0.4: configurable rules at runtime (Lua? CEL? still deciding)
- v0.5: Windows support
- v0.6: team mode (sharing daemon state across machines)
