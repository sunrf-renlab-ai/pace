package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sunrf-renlab-ai/mentor/pkg/daemon"
)

func main() {
	d, err := daemon.Start()
	if err != nil {
		log.Fatalf("start daemon: %v", err)
	}
	fmt.Fprintln(os.Stderr, "mentord running")

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	d.Stop()
}
