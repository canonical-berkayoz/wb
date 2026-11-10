package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/canonical/wb/internal/parallel"
	"github.com/canonical/wb/internal/pool"
)

var cloneAll, cloneAdd bool

var cloneCmd = &cobra.Command{
	Use:   "clone [REPO...]",
	Short: "Clone missing repos in parallel into <root>/<owner>/<repo>",
	Long:  "Clone missing repos in parallel into <root>/<owner>/<repo>.\nWithout REPO arguments, the active workbench's repos are cloned.",
	RunE: func(cmd *cobra.Command, args []string) error {
		selected, err := env.SelectRepos(args, cloneAll, cloneAdd)
		if err != nil {
			return err
		}
		var missing []string
		for _, repo := range selected {
			if pool.IsCloned(env.rootDir, repo) {
				fmt.Fprintf(env.out, "· %s (already cloned)\n", repo)
			} else {
				missing = append(missing, repo)
			}
		}
		failed := 0
		for outcome := range parallel.Run(missing, parallel.DefaultWorkers, func(repo string) (string, error) {
			return pool.Clone(env.rootDir, repo)
		}) {
			if outcome.Err != nil {
				failed++
				fmt.Fprintf(env.out, "✗ %s: %v\n", outcome.Item, outcome.Err)
				continue
			}
			fmt.Fprintf(env.out, "✓ %s (%.0fs)\n", outcome.Item, outcome.Elapsed.Seconds())
		}
		if failed > 0 {
			return errSilent
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cloneCmd)
	cloneCmd.Flags().BoolVar(&cloneAll, "all", false, "clone every repo in the pool")
	cloneCmd.Flags().BoolVar(&cloneAdd, "add", false, "allow owner/repo not in any workbench")
}
