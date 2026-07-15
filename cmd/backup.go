package cmd

import (
	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use:   "backup <source-dir>",
	Short: "Back up a directory into the vault",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Printf("backup: not implemented yet (arriving in milestone M1) — source: %s\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(backupCmd)
}
