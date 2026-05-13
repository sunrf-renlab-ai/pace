//go:build darwin

package tray

import (
	"github.com/getlantern/systray"
)

// Run launches the macOS menubar tray. Blocks the calling goroutine until
// the user clicks Quit, at which point onQuit is invoked. systray must run
// on the main goroutine on macOS.
func Run(onQuit func()) {
	onReady := func() {
		systray.SetTitle("◐")
		systray.SetTooltip("Mentor")

		mStatus := systray.AddMenuItem("Status…", "show status (use `mentor status`)")
		systray.AddSeparator()
		mPauseAll := systray.AddMenuItem("Pause all (use `mentor pause`)", "pause notifications")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit Mentor", "stop mentord")

		go func() {
			for {
				select {
				case <-mStatus.ClickedCh:
					// Surface deferred to CLI for v1; tray is ambient indicator only.
				case <-mPauseAll.ClickedCh:
					// Future: implement pause-all via daemon API
				case <-mQuit.ClickedCh:
					onQuit()
					systray.Quit()
					return
				}
			}
		}()
	}
	systray.Run(onReady, func() {})
}

// Available reports whether the tray subsystem is usable on this platform.
func Available() bool { return true }
