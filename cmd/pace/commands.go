package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sunrf-renlab-ai/pace/pkg/ipc"
)

func dial() *ipc.Client {
	c, err := ipc.Dial()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot connect to paced — is it running?")
		fmt.Fprintln(os.Stderr, "  details:", err)
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
