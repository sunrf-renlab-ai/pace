//go:build darwin

package tray

// v0.2.0 ships without a tray on macOS to keep the release pipeline cgo-free.
// macOS native menubar is on the v0.3 roadmap (built on a macos-latest runner
// with CGO_ENABLED=1).
func Run(onQuit func()) {}

// Available reports whether the tray subsystem is usable on this platform.
func Available() bool { return false }
