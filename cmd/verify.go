package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Supriya-112/chunkvault/internal/vault"
)

var (
	verifyVault          string
	verifyPassphraseFile string
	verifyWorkers        int
	verifyQuick          bool
)

var verifyCmd = &cobra.Command{
	Use:   "verify [snapshot-id]",
	Short: "Check the integrity of the vault, or of one snapshot",
	Long: `verify reads back every stored chunk, decrypting and decompressing it and
checking it against the ID it is stored under, so bit-rot or tampering is
detected. It also confirms that every chunk a snapshot references is present.

With no argument the whole vault is checked; with a snapshot ID only that
snapshot's chunks are. Use --quick to check that chunks are present without
reading their contents. The command exits non-zero if any problem is found.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		passphrase, err := passphraseForVault(cmd, verifyVault, verifyPassphraseFile)
		if err != nil {
			return err
		}
		var snapID string
		if len(args) == 1 {
			snapID = args[0]
		}

		rep, err := vault.Verify(cmd.Context(), verifyVault, snapID, passphrase, verifyWorkers, verifyQuick)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		mode := "deep"
		if verifyQuick {
			mode = "quick"
		}
		fmt.Fprintf(out, "checked %s across %s (%s)\n", plural(rep.Chunks, "chunk"), plural(rep.Snapshots, "snapshot"), mode)
		if rep.OK() {
			fmt.Fprintln(out, "  no problems found")
			return nil
		}
		if len(rep.Corrupt) > 0 {
			fmt.Fprintf(out, "  corrupt chunks (%d):\n", len(rep.Corrupt))
			for _, id := range rep.Corrupt {
				fmt.Fprintf(out, "    %s\n", id)
			}
		}
		if len(rep.Missing) > 0 {
			fmt.Fprintf(out, "  missing chunks (%d):\n", len(rep.Missing))
			for _, id := range rep.Missing {
				fmt.Fprintf(out, "    %s\n", id)
			}
		}
		if len(rep.Broken) > 0 {
			fmt.Fprintf(out, "  affected snapshots: %s\n", strings.Join(rep.Broken, ", "))
		}
		return fmt.Errorf("integrity check failed: %d corrupt, %d missing", len(rep.Corrupt), len(rep.Missing))
	},
}

// plural formats a count with a naively pluralized noun ("1 chunk", "2 chunks").
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func init() {
	verifyCmd.Flags().StringVar(&verifyVault, "vault", "./vault", "path to the vault directory")
	verifyCmd.Flags().StringVar(&verifyPassphraseFile, "passphrase-file", "", "read the vault passphrase from this file")
	verifyCmd.Flags().IntVar(&verifyWorkers, "workers", 0, "number of verification workers (0 = one per CPU)")
	verifyCmd.Flags().BoolVar(&verifyQuick, "quick", false, "only check that chunks are present, without reading their contents")
	rootCmd.AddCommand(verifyCmd)
}
