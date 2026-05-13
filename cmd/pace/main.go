package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sunrf-renlab-ai/pace/pkg/hook"
	"github.com/sunrf-renlab-ai/pace/pkg/oauth"
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
	case "logout":
		p, _ := tokenPathForCli()
		os.Remove(p)
		fmt.Println("logged out (token removed)")
	case "status":
		runStatus()
	case "pause":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: pace pause <project-path>")
			os.Exit(1)
		}
		runPause(os.Args[2])
	case "undo":
		runUndo()
	case "actions":
		runActions()
	case "chat":
		runChat()
	case "plan":
		runPlan(os.Args[2:])
	case "standup":
		runStandup()
	case "focus":
		runFocus(os.Args[2:])
	case "goal":
		runGoal(os.Args[2:])
	case "goals":
		runGoals()
	case "mentor":
		runMentor(os.Args[2:])
	case "ask":
		runAsk(os.Args[2:])
	case "review":
		runReview(os.Args[2:])
	case "consult":
		runConsult(os.Args[2:])
	case "version":
		fmt.Println("pace v0.5.0")
	case "help", "-h", "--help":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printHelp()
		os.Exit(1)
	}
}

func mustOK(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println(`pace — autonomous AI project manager for Claude Code projects

USAGE
  pace                                     open chat REPL (default)

INSTALL / AUTH
  pace init                                install hooks into ~/.claude/settings.json
  pace uninstall                           remove pace hooks
  pace login                               OAuth-authorize Pace (optional)
  pace logout                              remove stored OAuth token

DAILY OPS
  pace status                              show daemon status
  pace actions                             list recent pace actions
  pace undo                                reverse the last pace action
  pace pause <project-path>                pause a project (pace will ignore it)

PROJECT MANAGEMENT (v0.3)
  pace plan                                show today's latest plan
  pace plan generate                       brain generates a fresh plan now
  pace standup                             one-line morning brief (alias for plan)
  pace focus                               show current focus
  pace focus <project> [--reason "..."]    set this week's focus project
                          [--until DATE]
  pace focus clear                         clear focus
  pace goal                                list all project goals
  pace goal <project>                      show one project's goal
  pace goal <project> "<description>"      set/update a goal
                          [--deadline DATE]
  pace goal <project> --delete             remove a goal
  pace goals                               list all goals (alias)

MENTOR (v0.4) — evidence-grounded opinions, two-pass adversarial
  pace mentor                              list open opinions (unack'd)
  pace mentor all                          list all opinions
  pace mentor ack <id>                     mark an opinion acknowledged
  pace mentor dismiss <id>                 dismiss an opinion
  pace ask "<question>"                    ask the mentor anything (a few seconds)
  pace review [<commit-sha>]               review HEAD (or sha) of cwd
                  [--project <path>]
  pace consult <project-path>              deep-dive on a whole project (~30s, costs tokens)

DATE format: YYYY-MM-DD or full RFC3339.

If 'claude' is on PATH and authenticated, Pace will spawn it via subprocess
to make decisions — 'pace login' is only needed if you want a separate token.

The daemon (paced) must be running. Install via the install script
or launch manually.`)
}

func tokenPathForCli() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + "/.config/pace/auth.json", nil
}
