package main

import (
	"context"
	"fmt"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	root := newRoot(os.Stdin, os.Stdout, os.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "shiki:", version.Describe(err, buildinfo.Version))
		os.Exit(1)
	}
}
