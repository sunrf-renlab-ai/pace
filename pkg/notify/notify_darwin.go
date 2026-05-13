//go:build darwin

package notify

import (
	"fmt"
	"os/exec"
	"strings"
)

type OSNotifier struct{}

func New() *OSNotifier { return &OSNotifier{} }

func (n *OSNotifier) Notify(title, body string) error {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		return s
	}
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, esc(body), esc(title))
	return exec.Command("osascript", "-e", script).Run()
}
