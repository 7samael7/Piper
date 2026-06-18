package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/pipeline-workbench/engine/internal/api"
	"github.com/pipeline-workbench/engine/internal/persistence"
)

func main() {
	log.SetOutput(os.Stderr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := persistence.OpenDefault(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open persistence store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	server := api.NewServer(os.Stdin, os.Stdout, store)
	if err := server.Run(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "run daemon: %v\n", err)
		os.Exit(1)
	}
}
