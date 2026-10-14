package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/agent"
	"github.com/GhandyP/schema-validated-synthetic-data/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	code := cli.Run(ctx, os.Args[1:], cli.Deps{
		NewCompiler: func(ctx context.Context) (agent.Compiler, error) {
			return agent.NewGeminiCompiler(ctx)
		},
	}, os.Stdout, os.Stderr)

	stop()
	os.Exit(code)
}
