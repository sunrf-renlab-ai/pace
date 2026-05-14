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
| `pkg/ingest`     | HTTP `/event` endpoint; validates payload, writes to `events` table, upserts `projects`, fires the optional `OnEvent` callback after a successful store (daemon wires this to `Loop.Notify`). |
| `pkg/hook`       | Idempotently merges Pace's hook entries into `~/.claude/settings.json`; respects existing hooks. |
| `pkg/brain`      | Spawns `claude -p --input-format stream-json --output-format stream-json --verbose --permission-mode bypassPermissions` (v0.8 bidirectional streaming, modeled after `multica/server/pkg/agent/claude.go`). Writes the user prompt to stdin as NDJSON, scans stdout NDJSON line-by-line: accumulates assistant text, records every `tool_use` block name into `Decision.ToolsUsed`, sums per-call `Usage` (input/output/cache tokens). Brain has tool access (Read/Glob/Bash) — `pace review` / `pace consult` actually read your code. Decision types: `ignore` (most common), `notify`, `generate_plan`, `mentor_review`, `spawn_session`, `sync_files`, `pause_project`, `set_pref`, `batch`. 5-min subprocess timeout. |
| `pkg/pm`         | v0.3 project-management layer: per-project goals, current focus declaration, generated plan documents. Pure data + persistence — no LLM calls. |
| `pkg/mentor`     | v0.4 mentor layer: durable structured opinions (observation, concern, recommendation, confidence, evidence refs) with ack/dismiss lifecycle. Pure data + persistence. |
| `pkg/action`     | Action registry + executors (`notify`, `spawn_session`, `sync_files`, `pause_project`, `set_pref`, `generate_plan`, `mentor_review`); each action logged BEFORE execution. `generate_plan` writes markdown to `~/.config/pace/plans/`. `mentor_review` saves N opinions to `mentor_opinions` and notifies once with a summary. |
| `pkg/notify`     | OS notification backend (`osascript` on macOS, `notify-send` on Linux). Build tags. |
| `pkg/loop`       | Brain coordinator. v0.6 is event-driven: ingest calls `Loop.Notify()` on each event → debouncer waits 5s quiet (max 30s) → brain runs ONCE on every event since the previous run. A 30-min strategic tick fires brain regardless of events for periodic reflection. nil brain = no-op (no fallback). |
| `pkg/ipc`        | Unix socket JSON-RPC server + client. CLI talks to daemon over this. |
| `pkg/oauth`      | Optional PKCE flow against Anthropic OAuth endpoints (env-overridable). Tokens live at `~/.config/pace/auth.json` mode `0600`. |
| `pkg/tray`       | macOS menubar (`getlantern/systray`); no-op on Linux. |
| `pkg/daemon`     | Composition root: opens state, binds ephemeral HTTP port, writes the port file, wires loop+brain+actions+IPC. |
| `cmd/pace`     | CLI: `init`, `login`, `status`, `pause`, `undo`, `actions`, `chat`, v0.3 `plan`, `standup`, `focus`, `goal`, `goals`, v0.4 `mentor`, `ask`, `review`, `consult`. |
| `cmd/paced`    | Daemon entrypoint. Calls `daemon.Start()`, runs tray (macOS) or waits on signals. |
| `cmd/e2e`        | Smoke harness: spins up daemon, posts a synthetic event, verifies it lands in SQLite. |

## Key invariants

1. **Daemon never writes user project files directly.** The only writes outside `~/.config/pace/` go through `spawn_session`, which is just `claude -p` — same trust boundary as the user invoking it themselves.
2. **Hooks fail loud (v0.7).** The hook script POSTs synchronously with a 2s timeout and `exit 1` on any failure (daemon unreachable, non-2xx response). The user sees a stderr message in the Claude session — "$HOME/.config/pace/port missing — daemon not running" or similar — instead of silent data loss. Earlier versions were fail-open with a 500ms background fire-and-forget; v0.7 reverses that trade.
3. **Actions are logged before they execute.** `actions.Run` inserts the row with `status='pending'` first, then runs the executor, then updates to `done` or `failed`. A daemon crash mid-execute leaves a forensic trace.
4. **All timestamps are UTC at the SQL boundary.** `Loop.Once` calls `now.UTC()` before passing to rules. `ingest.store` converts incoming timestamps to UTC. This avoids lexicographic comparison failures in SQLite TEXT-stored times.
5. **Single writer to SQLite.** The daemon is the only process that opens the DB for write. CLI talks to daemon over the socket; it never opens the DB.
6. **Daemon binds an ephemeral port.** Port number written to `~/.config/pace/port` (atomic rename). The hook script reads this file at every invocation, so the port can change across restarts without breaking anything.

## Reasoning loop (the heart)

v0.5 deleted `pkg/rules`. v0.6 deleted the periodic 90s tick. The loop now has two parallel goroutines:

**1. Debouncer (event-driven primary path)**
   - Blocks on `notifyCh` until the first signal of a new window arrives.
   - Starts two timers:
     - `quietTimer` (default 5s): resets every time another notify arrives — fires when N seconds of silence pass.
     - `maxTimer` (default 30s): fixed deadline from window start — fires regardless if events keep streaming.
   - When either timer fires, calls `Once(ctx, now)`.
   - `Loop.Notify()` is non-blocking: the channel is buffered 1, so multiple notifies during a window collapse to one consumer wake-up.

**2. Strategic safety tick (long-interval fallback)**
   - `time.NewTicker(30 * time.Minute)` → fires `Once(ctx, now)` regardless of events.
   - Catches morning standup, deadlines, focus drift on quiet days.

Both paths call the same `Once(ctx, now)`:
1. Normalize `now` to UTC and atomically advance `lastTick`.
2. Pull all events ingested between the previous `lastTick` and `now` from SQLite (`SELECT payload_json FROM events WHERE timestamp > ? ORDER BY timestamp ASC`, capped at 200). For strategic ticks the event set is usually empty.
3. Build a single `DeciderInput` containing: window bounds + event list, active projects, goals (`pkg/pm`), focus, recent plans, open mentor opinions (`pkg/mentor`), user prefs, recent actions, current wall-clock time, `TriggerReason: "events"` if events exist or `"strategic"` otherwise.
4. Call `Brain.Decide()` ONCE.
5. Expand the resulting `Decision`:
   - `ignore` or empty → no actions (typical case).
   - `batch` → iterate `params.actions` and recursively execute each sub-decision.
   - anything else → run a single action through the registry, with `project_path` from `params.project_path` if present.

Without `claude` CLI on PATH and no OAuth token, brain is nil and `Once` short-circuits to no-op. There's no rule-based fallback — judgment is brain or nothing.

The rule-free design relies on the brain prompt template (`pkg/brain/prompt.tmpl`) to encode all judgment policy: signal-vs-noise heuristics, time-of-day plan generation, burst detection, mentor discipline (two-pass adversarial), refusal-when-unsure. Adding a new pattern of behavior in v0.5+ means editing the prompt, not adding Go code.

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
