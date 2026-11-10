package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/canonical/wb/internal/manifest"
	"github.com/canonical/wb/internal/pool"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List workbenches with cloned/total repos and open worktrees",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := env.Config()
		if err != nil {
			return err
		}
		if len(cfg.Workbenches) == 0 {
			fmt.Fprintf(env.out, "no workbenches in %s/workbenches; create one with `wb new`\n", env.rootDir)
			return nil
		}
		groups, err := manifest.All(env.rootDir)
		if err != nil {
			return err
		}
		for _, name := range cfg.WorkbenchNames() {
			workbench := cfg.Workbenches[name]
			cloned := 0
			for _, repo := range workbench.Repos {
				if pool.IsCloned(env.rootDir, repo) {
					cloned++
				}
			}
			open := 0
			for _, group := range groups {
				if group.Workbench == name {
					open++
				}
			}
			line := fmt.Sprintf("%-24s %d/%d cloned  %d open worktrees  %s",
				name, cloned, len(workbench.Repos), open, workbench.Description)
			fmt.Fprintln(env.out, strings.TrimRight(line, " "))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
