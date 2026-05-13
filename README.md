# Pace

> **Autonomous AI project manager for developers running many parallel Claude Code projects.** Watches every session, plans your day, decides what matters, takes action — uses your existing Claude subscription, no API key.

`pace` is a single Go binary that runs as a background daemon on your machine. The architecture in v0.5 is intentionally rule-free:

- **Hooks → events → tick** — silently injects hooks into every Claude Code session, every hook fires events into a local SQLite event log. Every ~90s, the brain (`claude -p` subprocess) gets the full event window plus all your state (active projects, goals, focus, plans, prior mentor opinions, recent actions) and decides what to do. No regex matching, no fixed schedule for "morning plan" — the brain reads the wall clock and decides.
- **Tick output** — the brain emits 0+ actions per tick: notify on a real failure, generate a plan if it's morning and none exists, surface a mentor opinion if drift is detected, or just `ignore` (most ticks). All actions are logged in advance so a crash leaves a forensic trace, and every action has an `undo`.
- **Manual mentor consultation** — `pace ask "..."`, `pace review [sha]`, `pace consult <project>` bypass the tick and invoke the brain in mentor mode synchronously, with the same two-pass adversarial discipline.

Everything is logged. Anything can be undone.

**Status:** alpha (v0.5). **No hardcoded rules** — every event flows directly to the LLM brain, which categorizes signal vs noise itself and decides 0+ actions per tick. Per-project goals + focus + plans. Two-pass adversarial mentor mode for code review opinions you can ack or dismiss. OAuth login optional — Pace inherits your existing `claude` CLI auth via subprocess.

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

# mentor (v0.4) — evidence-grounded opinions
pace mentor                              list open opinions
pace mentor all                          list all opinions (incl ack'd/dismissed)
pace mentor ack <id>                     mark opinion acknowledged
pace mentor dismiss <id>                 dismiss opinion
pace ask "<question>"                    ask the mentor anything
pace review [<commit-sha>]               code-review HEAD (or sha) of cwd
              [--project <path>]
pace consult <project-path>              deep-dive on a project
```

## Auth

Pace's brain shells out to `claude -p` to make decisions. There are two ways for the subprocess to find your auth:

1. **Inherit from your existing `claude` CLI** *(recommended, default)* — if you've already run `claude setup-token` (you have, if you use Claude Code), the subprocess Just Works. Pace adds no setup overhead.
2. **`pace login`** *(optional)* — runs an OAuth PKCE flow against Anthropic's endpoints, stores a separate token in `~/.config/pace/auth.json`. Useful if you want to scope a Pace-specific token.

If neither is configured, the tick loop becomes a no-op (events still ingest; no decisions get made). v0.5 has no rule-based fallback — without brain there is no judgment at all.

## What gets watched

Pace scans every project where a Claude session has run. The first event from a new project auto-registers it; you don't need to configure anything.

You can pause a project (`pace pause <path>`) to suppress notifications for it, or `pace pause <path> --until 2026-06-13` to set a date.

## How decisions get made (v0.5: rule-free)

There is no `pkg/rules`. There is no regex matching for "test fail" or "deploy fail". There is no fixed 09:00 morning standup schedule.

Instead, every ~90 seconds, the daemon:
1. Pulls all events ingested since the previous tick.
2. Builds the full state snapshot (active projects, goals, focus, recent plans, open mentor opinions, user prefs, recent actions, current wall-clock time).
3. Calls the brain (`claude -p` subprocess) ONCE.
4. Brain returns one of: `ignore` (most ticks — most events are noise), `notify`, `generate_plan`, `mentor_review`, `spawn_session`, `sync_files`, `pause_project`, `set_pref`, or `batch` (multiple actions in one tick).
5. Loop expands the decision and runs each action through the registry.

Why this works: the brain has the full context, so it can apply judgment that hardcoded rules can't — for example noticing that "this is the third deploy fail today on the focus project a day before the deadline" deserves a different response than "single deploy fail on a non-focus project at midnight". The prompt template tells the brain its responsibilities (signal-vs-noise, time-of-day plan generation, burst detection, mentor discipline) and trusts it to apply them.

Cost: ~960 ticks/day × 1 LLM call per tick × usually short prompts → fits comfortably inside Claude Max 20x. Most ticks are `ignore` and complete in 2-5 seconds.

## Mentor mode

Pace is also your senior engineer. When the brain decides to emit `mentor_review` (or you trigger one via CLI), it follows a stricter contract:

1. **Initial review** — brain reads the trigger context, may use file tools to read code, and lists 1-5 candidate observations. Each has: topic, observation (with evidence), concern, recommendation, confidence label, evidence refs.
2. **Adversarial pass** — brain plays devil's advocate against each candidate: is this a nitpick? Does the recommendation make code worse? Did I miss context? Am I just pattern-matching?
3. **Output only the survivors** — better to surface 1 strong opinion than 5 weak ones. If everything got challenged out, output zero opinions and explain in rationale.

```bash
# Ask anything, get a calibrated opinion in seconds
pace ask "Should I extract this duplication or YAGNI it for now?"

# Code review HEAD (or specific sha) of cwd
pace review
pace review abc1234

# Deep-dive on a project (~30s, costs more tokens)
pace consult ~/project/pair

# Inbox of mentor's open opinions
pace mentor
pace mentor ack <id>      # I read it, accept the point
pace mentor dismiss <id>  # I disagree / not relevant
```

**Reliability mechanisms:**
- Every opinion cites specific file:line, commit SHA, function names — no vague references
- Confidence labels (high/medium/low) — brain can also refuse to opine if context is insufficient
- Brain sees its own past opinions and won't re-raise the same one
- Stored in `mentor_opinions` table so you can compare opinions over time, see which ones panned out

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
