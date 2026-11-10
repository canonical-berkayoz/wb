package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/canonical/wb/internal/manifest"
	"github.com/canonical/wb/internal/worktree"
)

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List worktrees (only those of -w WORKBENCH, if given) with dirty/ahead/behind per repo",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := env.Config()
		if err != nil {
			return err
		}
		groups, err := manifest.All(env.rootDir)
		if err != nil {
			return err
		}
		var shown []*manifest.Group
		for _, group := range groups {
			if opt := env.workbenchOpt(); opt == "" || group.Workbench == opt {
				shown = append(shown, group)
			}
		}
		if len(shown) == 0 {
			fmt.Fprintln(env.out, "no worktrees")
		}
		for _, group := range shown {
			fmt.Fprintf(env.out, "%s  [%s]  branch %s\n", group.Slug, group.Workbench, group.Branch)
			for _, entry := range group.Repos {
				changes, divergence := worktree.RepoStatus(entry)
				fmt.Fprintf(env.out, "  %-40s %-9s %s\n", entry.Repo, changes, divergence)
			}
		}
		if untracked := worktree.UntrackedWorktrees(env.rootDir, cfg, groups); len(untracked) > 0 {
			fmt.Fprintln(env.out, "untracked worktrees:")
			for _, item := range untracked {
				branch := item.Branch
				if branch == "" {
					branch = "detached"
				}
				fmt.Fprintf(env.out, "  %-40s %s  (%s)\n", item.Repo, item.Path, branch)
			}
		}
		return nil
	},
}

func init() {
	worktreeCmd.AddCommand(worktreeListCmd)
}
