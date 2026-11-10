package cmd

import "github.com/spf13/cobra"

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Same-branch worktrees across pool repos, under <root>/.worktrees/<name>/",
}

func init() {
	rootCmd.AddCommand(worktreeCmd)
}
