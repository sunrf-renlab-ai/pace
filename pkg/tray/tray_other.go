//go:build !darwin

package tray

// Run is a no-op on non-macOS platforms; the daemon must use a signal handler
// instead. Returns immediately so the caller knows tray is unavailable.
func Run(onQuit func()) {}

// Available reports whether the tray subsystem is usable on this platform.
func Available() bool { return false }
