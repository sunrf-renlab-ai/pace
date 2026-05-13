package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// runPlan: `pace plan` (show latest), `pace plan generate` (force regenerate)
func runPlan(args []string) {
	c := dial()
	defer c.Close()

	method := "plan.show"
	if len(args) > 0 && args[0] == "generate" {
		method = "plan.generate"
	}
	r, err := c.Call(method, map[string]any{"scope": "today"})
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	if m, ok := r.Result.(map[string]any); ok {
		if msg, ok := m["message"].(string); ok && m["plan"] == nil {
			fmt.Println(msg)
			return
		}
		printPlan(m)
		return
	}
	b, _ := json.MarshalIndent(r.Result, "", "  ")
	fmt.Println(string(b))
}

func printPlan(m map[string]any) {
	scope, _ := m["scope"].(string)
	gen, _ := m["generated_at"].(string)
	body, _ := m["content_md"].(string)
	if scope != "" || gen != "" {
		fmt.Printf("# Plan (%s) — generated %s\n\n", scope, gen)
	}
	if body != "" {
		fmt.Println(body)
	}
}

func runStandup() {
	// standup is the same surface as `pace plan` but trimmed for a single-line brief if possible
	c := dial()
	defer c.Close()
	r, err := c.Call("plan.show", map[string]any{"scope": "today"})
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	m, _ := r.Result.(map[string]any)
	if m == nil || m["content_md"] == nil {
		fmt.Println("No standup yet today. Run `pace plan generate`.")
		return
	}
	body, _ := m["content_md"].(string)
	// First 12 lines as a brief
	lines := strings.SplitN(body, "\n", 13)
	if len(lines) > 12 {
		lines = lines[:12]
		lines = append(lines, "  …(truncated — full plan: `pace plan`)")
	}
	fmt.Println(strings.Join(lines, "\n"))
}

func runFocus(args []string) {
	c := dial()
	defer c.Close()

	if len(args) == 0 {
		// Show
		r, err := c.Call("focus.get", nil)
		mustOK(err)
		if !r.OK {
			fmt.Fprintln(os.Stderr, r.Error)
			os.Exit(1)
		}
		if r.Result == nil {
			fmt.Println("(no focus set)")
			return
		}
		b, _ := json.MarshalIndent(r.Result, "", "  ")
		fmt.Println(string(b))
		return
	}

	if args[0] == "clear" {
		r, err := c.Call("focus.clear", nil)
		mustOK(err)
		if !r.OK {
			fmt.Fprintln(os.Stderr, r.Error)
			os.Exit(1)
		}
		fmt.Println("focus cleared")
		return
	}

	// Set: pace focus <project> [--reason "..."] [--until DATE]
	params := map[string]any{"project_path": args[0]}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--reason":
			if i+1 < len(args) {
				params["reason"] = args[i+1]
				i++
			}
		case "--until":
			if i+1 < len(args) {
				params["until"] = args[i+1]
				i++
			}
		}
	}
	r, err := c.Call("focus.set", params)
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	fmt.Printf("focus set: %s\n", args[0])
}

func runGoal(args []string) {
	c := dial()
	defer c.Close()

	if len(args) == 0 {
		runGoals()
		return
	}

	project := args[0]
	rest := args[1:]

	// Delete
	for _, a := range rest {
		if a == "--delete" {
			r, err := c.Call("goal.delete", map[string]any{"project_path": project})
			mustOK(err)
			if !r.OK {
				fmt.Fprintln(os.Stderr, r.Error)
				os.Exit(1)
			}
			fmt.Println("goal deleted")
			return
		}
	}

	// Get
	if len(rest) == 0 {
		r, err := c.Call("goal.get", map[string]any{"project_path": project})
		mustOK(err)
		if !r.OK {
			fmt.Fprintln(os.Stderr, r.Error)
			os.Exit(1)
		}
		b, _ := json.MarshalIndent(r.Result, "", "  ")
		fmt.Println(string(b))
		return
	}

	// Set: pace goal <project> "<desc>" [--deadline DATE]
	params := map[string]any{"project_path": project, "description": rest[0]}
	for i := 1; i < len(rest); i++ {
		if rest[i] == "--deadline" && i+1 < len(rest) {
			params["deadline"] = rest[i+1]
			i++
		}
	}
	r, err := c.Call("goal.set", params)
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	fmt.Printf("goal set for %s\n", project)
}

func runGoals() {
	c := dial()
	defer c.Close()
	r, err := c.Call("goal.get", nil)
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(r.Result, "", "  ")
	fmt.Println(string(b))
}
