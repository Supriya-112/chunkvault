package cmd

import (
	"fmt"
	"os"

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
}

// Execute runs the root command and exits non-zero on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
