package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Supriya-112/chunkvault/internal/vault"
)

var (
	backupVault   string
	backupWorkers int
)

var backupCmd = &cobra.Command{
	Use:   "backup <source-dir>",
	Short: "Back up a directory into the vault",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := vault.Backup(cmd.Context(), args[0], backupVault, 0, backupWorkers)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "snapshot %s\n", res.SnapshotID)
		fmt.Fprintf(out, "  files:   %d\n", res.Files)
		fmt.Fprintf(out, "  chunks:  %d total, %d new\n", res.TotalChunks, res.NewChunks)
		fmt.Fprintf(out, "  data:    %s scanned, %s stored (%.0f%% deduplicated)\n",
			humanBytes(res.TotalBytes), humanBytes(res.StoredBytes), res.DedupRatio()*100)
		if res.Skipped > 0 {
			fmt.Fprintf(out, "  skipped: %d non-regular entries (symlinks, devices, etc.)\n", res.Skipped)
		}
		return nil
	},
}

func init() {
	backupCmd.Flags().StringVar(&backupVault, "vault", "./vault", "path to the vault directory")
	backupCmd.Flags().IntVar(&backupWorkers, "workers", 0, "number of chunk workers (0 = one per CPU)")
	rootCmd.AddCommand(backupCmd)
}

// humanBytes formats a byte count in a human-readable form (e.g. 1.5 MiB).
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
