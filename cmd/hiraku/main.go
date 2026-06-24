package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/makeding/hiraku/internal/client"
)

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: hiraku <host:port> <secret> <mode> <channel>")
		os.Exit(2)
	}

	done := make(chan struct{})
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-signals:
			os.Exit(0)
		case <-done:
		}
	}()

	err := client.Run(os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Stdout)
	close(done)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hiraku: %v\n", err)
		os.Exit(1)
	}
}
