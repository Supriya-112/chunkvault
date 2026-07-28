package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Supriya-112/chunkvault/internal/vault"
)

var (
	restoreVault          string
	restorePassphraseFile string
)

var restoreCmd = &cobra.Command{
	Use:   "restore <snapshot-id> <target-dir>",
	Short: "Restore a snapshot from the vault into a directory",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		passphrase, err := passphraseForVault(cmd, restoreVault, restorePassphraseFile)
		if err != nil {
			return err
		}
		res, err := vault.Restore(restoreVault, args[0], args[1], passphrase)
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
	restoreCmd.Flags().StringVar(&restorePassphraseFile, "passphrase-file", "", "read the vault passphrase from this file")
	rootCmd.AddCommand(restoreCmd)
}
