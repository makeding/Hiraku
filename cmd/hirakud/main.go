package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/makeding/hiraku/internal/agent"
	"github.com/makeding/hiraku/internal/config"
)

func main() {
	configPath := flag.String("config", "/etc/hiraku/config.json", "path to agent config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hirakud: %v\n", err)
		os.Exit(1)
	}

	logger := log.New(os.Stderr, "hirakud: ", log.LstdFlags)
	server := agent.NewServer(cfg, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "hirakud: %v\n", err)
		os.Exit(1)
	}
}
