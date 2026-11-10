package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/canonical/wb/internal/parallel"
	"github.com/canonical/wb/internal/pool"
)

var syncAll bool

var syncCmd = &cobra.Command{
	Use:   "sync [REPO...]",
	Short: "Fetch main checkouts and fast-forward clean default branches",
	RunE: func(cmd *cobra.Command, args []string) error {
		selected, err := env.SelectRepos(args, syncAll, false)
		if err != nil {
			return err
		}
		cfg, err := env.Config()
		if err != nil {
			return err
		}
		failed := 0
		for outcome := range parallel.Run(selected, parallel.DefaultWorkers, func(repo string) (string, error) {
			return pool.Sync(env.rootDir, cfg, repo)
		}) {
			if outcome.Err != nil {
				failed++
				fmt.Fprintf(env.out, "✗ %s: %v\n", outcome.Item, outcome.Err)
				continue
			}
			fmt.Fprintln(env.out, outcome.Value)
		}
		if failed > 0 {
			return errSilent
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().BoolVar(&syncAll, "all", false, "sync every repo in the pool")
}
