# Pace

> **Autonomous AI project manager for developers running many parallel Claude Code projects.** Watches every session, plans your day, decides what matters, takes action — uses your existing Claude subscription, no API key.

`pace` is a single Go binary that runs as a background daemon on your machine. It does two things simultaneously:

- **Reactive** — silently injects hooks into every Claude Code session, ingests the events, runs them through cheap local rules first, and only then decides whether to invoke your LLM (`claude -p` subprocess) for a richer judgment. When something matters (test fail, deploy fail, repeated errors, drift from your declared focus), it takes action: a macOS notification, a cross-project sync note, or spawning a helper Claude session in another project.
- **Proactive** — knows each project's goal + deadline + your declared focus. Generates a real markdown plan every morning ("today, do X first because of Y; B is blocked on A; C has a deadline this Friday") and stores it. `pace plan` shows it. `pace standup` gives you a brief.

Everything is logged. Anything can be undone.

**Status:** alpha (v0.3). 6 rules, LLM brain, OAuth login (optional — Pace inherits your existing `claude` CLI auth via subprocess), per-project goals, focus declarations, and morning plan generation.

---

## Install

One line, when released:

```bash
curl -fsSL https://pace.sh/install | sh
```

The script downloads the right binary for your platform, drops it in `/usr/local/bin` (or `~/.local/bin` if no sudo), installs a `LaunchAgent` (macOS) or `systemd --user` unit (Linux) so the daemon starts at login, then prints next steps.

### From source

```bash
git clone https://github.com/sunrf-renlab-ai/pace
cd pace
go build -o bin/pace   ./cmd/pace
go build -o bin/paced  ./cmd/paced
```

## First run

```bash
$ pace init                # writes hooks into ~/.claude/settings.json
$ paced &                  # start the daemon (LaunchAgent does this automatically)
$ pace                     # opens chat REPL
Pace — 0 active project(s), 0 event(s) today, 0 action(s) today

>
```

That's it. From now on, every time you `claude` in any project, Pace sees the events. When something interesting happens (test fail, deploy fail, two consecutive errors in one session, a 30-minute strategic sweep, …) it acts on its own.

## Daily UX

You don't open Pace's UI. It's ambient.

- **macOS notification** — fires when a rule + LLM judgment surfaces something.
- **Menubar icon** — `◐` indicator in the top bar, click for status / quit.
- **`pace` chat** — when you want to ask Pace what it's been doing or change how it manages.

```bash
$ pace
Pace — 4 active projects, 18 events today, 3 actions today

> what did you do today?
Today: pair deploy failed once (notified, didn't retry per your prefs);
agora hit two test failures in a row (notified); 30-min sweep didn't surface
anything new. socialmind paused until 2026-06-13.

> stop bugging me about agora deploy retries
Got it. Setting prefs: agora.deploy_retry_notify=false.
```

## CLI commands

```
pace                                     open chat (default)

# install / auth
pace init                                install hooks into ~/.claude/settings.json
pace uninstall                           remove pace hooks
pace login                               OAuth-authorize Pace (optional — see "Auth")
pace logout                              remove stored OAuth token

# daily ops
pace status                              show daemon status
pace actions                             list recent pace actions
pace undo                                reverse the last pace action
pace pause <project>                     pause a project (pace will ignore it)

# project management (v0.3)
pace plan                                show today's plan
pace plan generate                       brain generates a fresh plan now
pace standup                             one-line morning brief
pace focus                               show current focus
pace focus <project> [--reason "..."]    set this week's focus project
              [--until DATE]
pace focus clear                         clear focus
pace goal                                list all project goals
pace goal <project>                      show one project's goal
pace goal <project> "<description>"      set/update a goal
              [--deadline DATE]
pace goal <project> --delete             remove a goal
pace goals                               alias for `pace goal`

pace version                             print version
```

## Auth

Pace's brain shells out to `claude -p` to make decisions. There are two ways for the subprocess to find your auth:

1. **Inherit from your existing `claude` CLI** *(recommended, default)* — if you've already run `claude setup-token` (you have, if you use Claude Code), the subprocess Just Works. Pace adds no setup overhead.
2. **`pace login`** *(optional)* — runs an OAuth PKCE flow against Anthropic's endpoints, stores a separate token in `~/.config/pace/auth.json`. Useful if you want to scope a Pace-specific token.

If neither is configured, Pace still runs — the loop degrades to direct rule-triggered notifications without LLM judgment. You see less, but it's not broken.

## What gets watched

Pace scans every project where a Claude session has run. The first event from a new project auto-registers it; you don't need to configure anything.

You can pause a project (`pace pause <path>`) to suppress notifications for it, or `pace pause <path> --until 2026-06-13` to set a date.

## Rules (v0.3)

| Rule | Fires when |
|------|-----------|
| **R1** Tool error burst | Same session, 2+ tool errors within 5 minutes |
| **R2** Test fail | A test command (`go test`, `npm test`, `pytest`, …) exited non-zero |
| **R3** Deploy fail | A deploy command (`vercel deploy`, `fly deploy`, …) exited non-zero |
| **R8** Periodic overview | Every 30 minutes, a "any blind spots?" sweep |
| **R9** Morning standup | Once per day at 09:00 local — brain generates today's plan |
| **R10** Focus drift | You declared focus on A, but the past hour has 3× more activity on B |

R4–R7 (cross-project schema drift, stale uncommitted changes, etc.) are roadmap.

## Project management

Pace is not just reactive. You can give it goals + focus, and it generates real plans:

```bash
# Tell pace what each project is for + when it's due
pace goal /Users/blink/project/pair "ship dual-agent simulator MVP" --deadline 2026-06-01
pace goal /Users/blink/project/agora "fix auth race condition reported in INC-247"

# Tell it where your attention is this week
pace focus /Users/blink/project/pair --reason "release this Friday" --until 2026-05-20

# At 9 AM (or anytime via `pace plan generate`) it writes a real plan
pace plan
# →
# # Today's Plan — Wed 2026-05-13
# **Focus this week:** /Users/blink/project/pair — release this Friday
#
# ## 1. ⭐ pair — ship dual-agent simulator MVP  (focus, deadline 2026-06-01)
# Today: finish the SVG radar component, hook up the report extractor.
# Blockers: orchestrator on Render needs 3 env vars set before integration test.
#
# ## 2. agora — fix auth race condition INC-247
# Today: add the regression test, then patch. ~2hr.
#
# ## 3. socialmind — paused until 2026-06-13 (skipped)
```

Plans are stored in `~/.config/pace/plans/<date>-today.md` so you can share, edit, or pin them.

## Privacy

- Pace talks to `127.0.0.1` only — nothing leaves your machine except the LLM call your `claude` subprocess makes (which is whatever Anthropic does for you anyway).
- Hook payloads are truncated to 200-char summaries before they hit the daemon. Pace never sees full prompts or file contents.
- Token (if you `pace login`) is mode `0600` in your home dir.

## Architecture

See [`ARCHITECTURE.md`](./ARCHITECTURE.md) for the full system overview, or [`docs/superpowers/specs/`](./docs/superpowers/specs/) for the design spec.

```
[every Claude session's hooks]
   ├─ POST event → paced (HTTP loopback)
                       ↓
                  Ingestor → Rule gate → LLM brain (claude -p) → Action executor
                                              ↓                     ↓
                                          (degraded to             notify /
                                           direct notify           spawn_session /
                                           if no LLM)              sync_files /
                                                                   set_pref /
                                                                   pause / undo
                  IPC over unix socket ← pace CLI (chat / status / pause / undo)
```

## Build & test

```bash
go test ./... -race -count=1
go build -o bin/pace   ./cmd/pace
go build -o bin/paced  ./cmd/paced
go run ./cmd/e2e          # end-to-end smoke
```

## License

MIT.
