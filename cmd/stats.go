package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Supriya-112/chunkvault/internal/vault"
)

var (
	statsVault          string
	statsPassphraseFile string
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show deduplication statistics for the vault",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		passphrase, err := passphraseForVault(cmd, statsVault, statsPassphraseFile)
		if err != nil {
			return err
		}
		st, err := vault.ComputeStats(statsVault, passphrase)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "vault %s\n", statsVault)
		fmt.Fprintf(out, "  snapshots:     %d\n", st.Snapshots)
		fmt.Fprintf(out, "  unique chunks: %d\n", st.UniqueChunks)
		fmt.Fprintf(out, "  logical size:  %s across %d chunk references\n", humanBytes(st.LogicalBytes), st.ChunkRefs)
		fmt.Fprintf(out, "  stored size:   %s\n", humanBytes(st.StoredBytes))
		fmt.Fprintf(out, "  saved:         %s (%.0f%% smaller after dedup + compression)\n", humanBytes(nonNeg(st.SavedBytes())), nonNegRatio(st.ReductionRatio())*100)
		return nil
	},
}

func init() {
	statsCmd.Flags().StringVar(&statsVault, "vault", "./vault", "path to the vault directory")
	statsCmd.Flags().StringVar(&statsPassphraseFile, "passphrase-file", "", "read the vault passphrase from this file")
	rootCmd.AddCommand(statsCmd)
}
