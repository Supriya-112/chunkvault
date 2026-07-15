package cmd

import (
	"github.com/spf13/cobra"
)

var restoreCmd = &cobra.Command{
	Use:   "restore <snapshot> <target-dir>",
	Short: "Restore a snapshot from the vault into a directory",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Printf("restore: not implemented yet (arriving in milestone M2) — snapshot: %s target: %s\n", args[0], args[1])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(restoreCmd)
}
