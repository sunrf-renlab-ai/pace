package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sunrf-renlab-ai/pace/pkg/daemon"
	"github.com/sunrf-renlab-ai/pace/pkg/tray"
)

func main() {
	d, err := daemon.Start()
	if err != nil {
		log.Fatalf("start daemon: %v", err)
	}
	fmt.Fprintln(os.Stderr, "paced running")

	if tray.Available() && os.Getenv("PACE_NO_TRAY") == "" {
		// systray.Run blocks the main goroutine; install a signal handler
		// that calls daemon.Stop on shutdown via tray.Run's onQuit.
		go func() {
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
			<-sigs
			d.Stop()
			os.Exit(0)
		}()
		tray.Run(func() { d.Stop() })
		return
	}

	// Headless: just wait for signal.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	d.Stop()
}
