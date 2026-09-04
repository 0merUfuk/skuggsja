package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/0merUfuk/skuggsja/internal/cli"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := cli.Execute(ctx, version); err != nil {
		fmt.Fprintf(os.Stderr, "skuggsja: %v\n", err)
		os.Exit(1)
	}
}
