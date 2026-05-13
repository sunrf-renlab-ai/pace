package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func runMentor(args []string) {
	c := dial()
	defer c.Close()

	if len(args) == 0 {
		// list open
		r, err := c.Call("mentor.list", map[string]any{"scope": "open"})
		mustOK(err)
		if !r.OK {
			fmt.Fprintln(os.Stderr, r.Error)
			os.Exit(1)
		}
		printOpinions(r.Result)
		return
	}
	switch args[0] {
	case "all":
		r, err := c.Call("mentor.list", map[string]any{"scope": "all"})
		mustOK(err)
		if !r.OK {
			fmt.Fprintln(os.Stderr, r.Error)
			os.Exit(1)
		}
		printOpinions(r.Result)
	case "ack":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: pace mentor ack <opinion_id>")
			os.Exit(1)
		}
		r, err := c.Call("mentor.ack", map[string]any{"opinion_id": args[1]})
		mustOK(err)
		if !r.OK {
			fmt.Fprintln(os.Stderr, r.Error)
			os.Exit(1)
		}
		fmt.Println("acknowledged")
	case "dismiss":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: pace mentor dismiss <opinion_id>")
			os.Exit(1)
		}
		r, err := c.Call("mentor.dismiss", map[string]any{"opinion_id": args[1]})
		mustOK(err)
		if !r.OK {
			fmt.Fprintln(os.Stderr, r.Error)
			os.Exit(1)
		}
		fmt.Println("dismissed")
	default:
		fmt.Fprintf(os.Stderr, "unknown mentor subcommand: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: pace mentor [all|ack <id>|dismiss <id>]")
		os.Exit(1)
	}
}

func runAsk(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pace ask \"<question>\"")
		os.Exit(1)
	}
	q := strings.Join(args, " ")
	c := dial()
	defer c.Close()
	r, err := c.Call("mentor.ask", map[string]any{"question": q})
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	m, _ := r.Result.(map[string]any)
	if m == nil {
		fmt.Println("(no answer)")
		return
	}
	if ans, ok := m["answer"].(string); ok {
		fmt.Println(ans)
		return
	}
	if ops, ok := m["opinions"].([]any); ok {
		printOpinions(ops)
	}
}

func runReview(args []string) {
	cwd, _ := os.Getwd()
	project := cwd
	sha := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project":
			if i+1 < len(args) {
				project = args[i+1]
				i++
			}
		default:
			if !strings.HasPrefix(args[i], "--") {
				sha = args[i]
			}
		}
	}
	c := dial()
	defer c.Close()
	r, err := c.Call("mentor.review", map[string]any{
		"project_path": project,
		"sha":          sha,
	})
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	m, _ := r.Result.(map[string]any)
	if m == nil {
		fmt.Println("(no review)")
		return
	}
	if ans, ok := m["answer"].(string); ok {
		fmt.Println(ans)
		return
	}
	if ops, ok := m["opinions"].([]any); ok {
		printOpinions(ops)
	}
}

func runConsult(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pace consult <project-path>")
		os.Exit(1)
	}
	c := dial()
	defer c.Close()
	r, err := c.Call("mentor.consult", map[string]any{"project_path": args[0]})
	mustOK(err)
	if !r.OK {
		fmt.Fprintln(os.Stderr, r.Error)
		os.Exit(1)
	}
	m, _ := r.Result.(map[string]any)
	if m == nil {
		fmt.Println("(no consult result)")
		return
	}
	if ans, ok := m["answer"].(string); ok {
		fmt.Println(ans)
		return
	}
	if ops, ok := m["opinions"].([]any); ok {
		printOpinions(ops)
	}
}

func printOpinions(raw any) {
	ops, ok := raw.([]any)
	if !ok || len(ops) == 0 {
		fmt.Println("(no opinions)")
		return
	}
	for i, o := range ops {
		m, _ := o.(map[string]any)
		if m == nil {
			continue
		}
		fmt.Printf("\n──── %d ────────────────────────────────\n", i+1)
		if v, _ := m["topic"].(string); v != "" {
			conf, _ := m["confidence"].(string)
			fmt.Printf("📌 %s  [confidence: %s]\n", v, conf)
		}
		if v, _ := m["project_path"].(string); v != "" {
			fmt.Printf("   project: %s\n", v)
		}
		if v, _ := m["observation"].(string); v != "" {
			fmt.Printf("\n   OBSERVATION:\n   %s\n", indentWrap(v))
		}
		if v, _ := m["concern"].(string); v != "" {
			fmt.Printf("\n   CONCERN:\n   %s\n", indentWrap(v))
		}
		if v, _ := m["recommendation"].(string); v != "" {
			fmt.Printf("\n   RECOMMENDATION:\n   %s\n", indentWrap(v))
		}
		if ev, ok := m["evidence"].([]any); ok && len(ev) > 0 {
			fmt.Printf("\n   EVIDENCE: ")
			parts := make([]string, 0, len(ev))
			for _, e := range ev {
				if s, ok := e.(string); ok {
					parts = append(parts, s)
				}
			}
			fmt.Println(strings.Join(parts, ", "))
		}
		if id, _ := m["opinion_id"].(string); id != "" {
			fmt.Printf("\n   id: %s\n", id)
		}
	}
	fmt.Println()
	fmt.Println("Acknowledge: pace mentor ack <id>    Dismiss: pace mentor dismiss <id>")
}

// fall back to JSON dump if the structure is unexpected
func dumpJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func indentWrap(s string) string {
	// simple word-wrap at ~80 cols, indent continuation lines with 3 spaces
	const width = 76
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if len(line) <= width {
			out = append(out, line)
			continue
		}
		words := strings.Fields(line)
		cur := ""
		for _, w := range words {
			if len(cur)+len(w)+1 > width {
				out = append(out, cur)
				cur = w
			} else if cur == "" {
				cur = w
			} else {
				cur += " " + w
			}
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return strings.Join(out, "\n   ")
}
