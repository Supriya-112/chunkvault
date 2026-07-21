package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Supriya-112/chunkvault/internal/vault"
)

var statsVault string

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show deduplication statistics for the vault",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := vault.ComputeStats(statsVault)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "vault %s\n", statsVault)
		fmt.Fprintf(out, "  snapshots:     %d\n", st.Snapshots)
		fmt.Fprintf(out, "  unique chunks: %d\n", st.UniqueChunks)
		fmt.Fprintf(out, "  logical size:  %s across %d chunk references\n", humanBytes(st.LogicalBytes), st.ChunkRefs)
		fmt.Fprintf(out, "  stored size:   %s\n", humanBytes(st.StoredBytes))
		fmt.Fprintf(out, "  saved:         %s (%.0f%% deduplicated)\n", humanBytes(st.SavedBytes()), st.DedupRatio()*100)
		return nil
	},
}

func init() {
	statsCmd.Flags().StringVar(&statsVault, "vault", "./vault", "path to the vault directory")
	rootCmd.AddCommand(statsCmd)
}
