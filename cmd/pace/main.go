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
	case "version":
		fmt.Println("pace v0.1.0-alpha")
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

Usage:
  pace                     open chat REPL (default)
  pace init                install hooks into ~/.claude/settings.json
  pace uninstall           remove pace hooks
  pace login               OAuth-authorize Pace to use your Claude account
  pace logout              remove stored OAuth token
  pace status              show daemon status
  pace pause <project>     pause a project (pace will ignore it)
  pace undo                undo the last pace action
  pace actions             list recent pace actions
  pace chat                same as bare 'pace'
  pace version             print version

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
