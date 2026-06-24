package main

import (
	"flag"
	"fmt"
	"log"
	"os"

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
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "hirakud: %v\n", err)
		os.Exit(1)
	}
}
