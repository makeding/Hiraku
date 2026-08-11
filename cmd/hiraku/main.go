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
		printUsage()
		os.Exit(2)
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	var err error
	if os.Args[1] == "pipe" {
		err = client.RunPipe(signalCtx, os.Args[2], os.Args[3], os.Args[4], os.Stdin, os.Stdout)
	} else {
		ctx, cancel := context.WithCancel(signalCtx)
		defer cancel()
		go cancelOnStdinData(os.Stdin, cancel)
		err = client.Run(ctx, os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Stdout)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintf(os.Stderr, "hiraku: %v\n", err)
		var remoteExit *client.RemoteExitError
		if errors.As(err, &remoteExit) && remoteExit.Code > 0 && remoteExit.Code < 126 {
			os.Exit(remoteExit.Code)
		}
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: hiraku <host:port> <secret> <mode> <channel>")
	fmt.Fprintln(os.Stderr, "       hiraku pipe <host:port> <secret> <pipe>")
}

func cancelOnStdinData(r io.Reader, cancel context.CancelFunc) {
	var buf [1]byte
	n, err := r.Read(buf[:])
	if n > 0 || errors.Is(err, io.EOF) {
		cancel()
	}
}
