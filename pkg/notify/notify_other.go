//go:build !darwin

package notify

import "os/exec"

type OSNotifier struct{}

func New() *OSNotifier { return &OSNotifier{} }

func (n *OSNotifier) Notify(title, body string) error {
	return exec.Command("notify-send", title, body).Run()
}
