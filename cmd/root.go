package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags, and defaults to "dev".
var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "chunkvault",
	Short:   "A content-addressable, deduplicating backup tool",
	Version: version,
	Long: `chunkvault backs up directories by splitting files into chunks and
storing only the unique ones, so unchanged data is never stored twice.

It is a small, readable take on tools like restic and borg, built to
demonstrate content-defined chunking, deduplication, and concurrent I/O.`,

	// Execute prints errors itself, and a failing command shouldn't dump usage.
	SilenceErrors: true,
	SilenceUsage:  true,
}

// Execute runs the root command and exits non-zero on error. The command
// context is cancelled on the first interrupt signal, so a long backup stops
// cleanly on Ctrl-C.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
