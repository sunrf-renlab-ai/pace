package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func runChat() {
	c := dial()
	defer c.Close()
	r, _ := c.Call("status", nil)
	if r.OK {
		if m, ok := r.Result.(map[string]any); ok {
			fmt.Printf("Mentor — %v active project(s), %v event(s) today, %v action(s) today\n",
				m["active_projects"], m["events_24h"], m["actions_24h"])
			if b, _ := m["brain"].(bool); !b {
				fmt.Println("(brain offline — running rules-only with direct notifications)")
			}
		}
	} else {
		fmt.Println("Mentor — daemon connected")
	}
	fmt.Println("Type messages, Ctrl-D to exit.")

	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !in.Scan() {
			break
		}
		msg := strings.TrimSpace(in.Text())
		if msg == "" {
			continue
		}
		resp, err := c.Call("chat", map[string]any{"message": msg})
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		if !resp.OK {
			fmt.Println("error:", resp.Error)
			continue
		}
		if m, ok := resp.Result.(map[string]any); ok {
			fmt.Println(m["reply"])
		}
	}
	fmt.Println()
}
