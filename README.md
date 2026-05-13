# Mentor

> **Autonomous AI project manager for developers running many parallel Claude Code projects.** Watches every session, decides what matters, takes action — uses your existing Claude subscription, no API key.

`mentor` is a single Go binary that runs as a background daemon on your machine. It silently injects hooks into every Claude Code session, ingests the events, runs them through cheap local rules first, and only then decides whether to invoke your LLM (`claude -p` subprocess) for a richer judgment. When something matters, it takes action: a macOS notification, a cross-project sync note, or even spawning a helper Claude session in another project.

Everything is logged. Anything can be undone.

**Status:** alpha (v0.2). Core loop is solid; LLM brain is wired; OAuth login is scaffolded but optional (Mentor inherits your existing `claude` CLI auth via subprocess).

---

## Install

One line, when released:

```bash
curl -fsSL https://mentor.sh/install | sh
```

The script downloads the right binary for your platform, drops it in `/usr/local/bin` (or `~/.local/bin` if no sudo), installs a `LaunchAgent` (macOS) or `systemd --user` unit (Linux) so the daemon starts at login, then prints next steps.

### From source

```bash
git clone https://github.com/sunrf-renlab-ai/mentor
cd mentor
go build -o bin/mentor   ./cmd/mentor
go build -o bin/mentord  ./cmd/mentord
```

## First run

```bash
$ mentor init                # writes hooks into ~/.claude/settings.json
$ mentord &                  # start the daemon (LaunchAgent does this automatically)
$ mentor                     # opens chat REPL
Mentor — 0 active project(s), 0 event(s) today, 0 action(s) today

>
```

That's it. From now on, every time you `claude` in any project, Mentor sees the events. When something interesting happens (test fail, deploy fail, two consecutive errors in one session, a 30-minute strategic sweep, …) it acts on its own.

## Daily UX

You don't open Mentor's UI. It's ambient.

- **macOS notification** — fires when a rule + LLM judgment surfaces something.
- **Menubar icon** — `◐` indicator in the top bar, click for status / quit.
- **`mentor` chat** — when you want to ask Mentor what it's been doing or change how it manages.

```bash
$ mentor
Mentor — 4 active projects, 18 events today, 3 actions today

> what did you do today?
Today: pair deploy failed once (notified, didn't retry per your prefs);
agora hit two test failures in a row (notified); 30-min sweep didn't surface
anything new. socialmind paused until 2026-06-13.

> stop bugging me about agora deploy retries
Got it. Setting prefs: agora.deploy_retry_notify=false.
```

## CLI commands

```
mentor                      open chat (default)
mentor init                 install hooks into ~/.claude/settings.json
mentor uninstall            remove mentor hooks
mentor login                OAuth-authorize Mentor (optional — see "Auth" below)
mentor logout               remove stored OAuth token
mentor status               show daemon status (active projects, event/action counts)
mentor pause <project>      pause a project (mentor will ignore it)
mentor undo                 reverse the last mentor action
mentor actions              list recent mentor actions
mentor version              print version
```

## Auth

Mentor's brain shells out to `claude -p` to make decisions. There are two ways for the subprocess to find your auth:

1. **Inherit from your existing `claude` CLI** *(recommended, default)* — if you've already run `claude setup-token` (you have, if you use Claude Code), the subprocess Just Works. Mentor adds no setup overhead.
2. **`mentor login`** *(optional)* — runs an OAuth PKCE flow against Anthropic's endpoints, stores a separate token in `~/.config/mentor/auth.json`. Useful if you want to scope a Mentor-specific token.

If neither is configured, Mentor still runs — the loop degrades to direct rule-triggered notifications without LLM judgment. You see less, but it's not broken.

## What gets watched

Mentor scans every project where a Claude session has run. The first event from a new project auto-registers it; you don't need to configure anything.

You can pause a project (`mentor pause <path>`) to suppress notifications for it, or `mentor pause <path> --until 2026-06-13` to set a date.

## Rules (v0.2)

Initial rule set:

| Rule | Fires when |
|------|-----------|
| **R1** Tool error burst | Same session, 2+ tool errors within 5 minutes |
| **R2** Test fail | A test command (`go test`, `npm test`, `pytest`, …) exited non-zero |
| **R3** Deploy fail | A deploy command (`vercel deploy`, `fly deploy`, …) exited non-zero |
| **R8** Periodic overview | Every 30 minutes, a "any blind spots?" sweep |

R4–R7 (cross-project schema drift, stale uncommitted changes, etc.) are roadmap.

## Privacy

- Mentor talks to `127.0.0.1` only — nothing leaves your machine except the LLM call your `claude` subprocess makes (which is whatever Anthropic does for you anyway).
- Hook payloads are truncated to 200-char summaries before they hit the daemon. Mentor never sees full prompts or file contents.
- Token (if you `mentor login`) is mode `0600` in your home dir.

## Architecture

See [`ARCHITECTURE.md`](./ARCHITECTURE.md) for the full system overview, or [`docs/superpowers/specs/`](./docs/superpowers/specs/) for the design spec.

```
[every Claude session's hooks]
   ├─ POST event → mentord (HTTP loopback)
                       ↓
                  Ingestor → Rule gate → LLM brain (claude -p) → Action executor
                                              ↓                     ↓
                                          (degraded to             notify /
                                           direct notify           spawn_session /
                                           if no LLM)              sync_files /
                                                                   set_pref /
                                                                   pause / undo
                  IPC over unix socket ← mentor CLI (chat / status / pause / undo)
```

## Build & test

```bash
go test ./... -race -count=1
go build -o bin/mentor   ./cmd/mentor
go build -o bin/mentord  ./cmd/mentord
go run ./cmd/e2e          # end-to-end smoke
```

## License

MIT.
