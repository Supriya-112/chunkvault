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

// noProgress disables the live progress display (also skipped automatically
// when stdout is not a terminal).
var noProgress bool

// S3 connection overrides for S3-compatible services. When set they populate the
// corresponding AWS environment variables the S3 backend reads.
var (
	s3Endpoint string
	s3Region   string
)

func init() {
	rootCmd.PersistentFlags().BoolVar(&noProgress, "no-progress", false, "disable the live progress display")
	rootCmd.PersistentFlags().StringVar(&s3Endpoint, "s3-endpoint", "", "S3-compatible endpoint URL for s3:// vaults (or set AWS_ENDPOINT_URL)")
	rootCmd.PersistentFlags().StringVar(&s3Region, "s3-region", "", "S3 region for s3:// vaults (or set AWS_REGION)")
	rootCmd.PersistentPreRunE = func(*cobra.Command, []string) error {
		if s3Endpoint != "" {
			if err := os.Setenv("AWS_ENDPOINT_URL", s3Endpoint); err != nil {
				return err
			}
		}
		if s3Region != "" {
			if err := os.Setenv("AWS_REGION", s3Region); err != nil {
				return err
			}
		}
		return nil
	}
}

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
