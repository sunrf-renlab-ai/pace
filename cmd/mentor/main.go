package main

import (
	"fmt"
	"os"

	"github.com/sunrf-renlab-ai/mentor/pkg/hook"
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
	case "version":
		fmt.Println("mentor v0.1.0-alpha")
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
	fmt.Println(`mentor — autonomous AI project manager for Claude Code projects

Usage:
  mentor                     open chat REPL (default)
  mentor init                install hooks into ~/.claude/settings.json
  mentor uninstall           remove mentor hooks
  mentor status              show daemon status
  mentor pause <project>     pause a project (mentor will ignore it)
  mentor undo                undo the last mentor action
  mentor actions             list recent mentor actions
  mentor chat                same as bare 'mentor'
  mentor version             print version

The daemon (mentord) must be running. Install via the install script
or launch manually.`)
}
