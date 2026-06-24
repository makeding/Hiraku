package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	go cancelOnStdinData(os.Stdin, cancel)

	err := client.Run(ctx, os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Stdout)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintf(os.Stderr, "hiraku: %v\n", err)
		os.Exit(1)
	}
}

func cancelOnStdinData(r io.Reader, cancel context.CancelFunc) {
	var buf [1]byte
	n, err := r.Read(buf[:])
	if n > 0 {
		cancel()
	}
	if err != nil {
		return
	}
}
