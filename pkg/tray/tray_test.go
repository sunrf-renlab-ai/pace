package tray

import "testing"

// Smoke test: package compiles and Available reports a value. We can't
// drive the systray UI in headless tests, so we just sanity-check the API.
func TestAvailableReturnsBool(t *testing.T) {
	_ = Available()
}
