package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/yottonoko/classic-web-system-lsp/internal/lspserver"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] != "--stdio" {
		fmt.Fprintf(os.Stderr, "unsupported transport %q: only --stdio is supported\n", os.Args[1])
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := lspserver.New(os.Stdin, os.Stdout, os.Stderr)
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "asp-lsp-go: %v\n", err)
		os.Exit(1)
	}
}
