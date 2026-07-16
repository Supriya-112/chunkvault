package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Supriya-112/chunkvault/internal/vault"
)

var restoreVault string

var restoreCmd = &cobra.Command{
	Use:   "restore <snapshot-id> <target-dir>",
	Short: "Restore a snapshot from the vault into a directory",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := vault.Restore(restoreVault, args[0], args[1])
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "restored snapshot %s\n", args[0])
		fmt.Fprintf(out, "  files: %d\n", res.Files)
		fmt.Fprintf(out, "  data:  %s\n", humanBytes(res.Bytes))
		return nil
	},
}

func init() {
	restoreCmd.Flags().StringVar(&restoreVault, "vault", "./vault", "path to the vault directory")
	rootCmd.AddCommand(restoreCmd)
}
