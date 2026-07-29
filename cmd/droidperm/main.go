package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/yutakobayashidev/droidperm/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	command := cli.New(version, os.Stdin, os.Stdout, os.Stderr)
	if err := command.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cli.ExitCode(err))
	}
}
