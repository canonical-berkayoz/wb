package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var worktreePathCmd = &cobra.Command{
	Use:   "path [NAME] [REPO]",
	Short: "Print a worktree folder or one of its checkouts",
	Long:  "Print a worktree folder or one of its checkouts, e.g. cd \"$(wb worktree path NAME REPO)\".",
	Args:  cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var name, repo string
		if len(args) > 0 {
			name = args[0]
		}
		if len(args) > 1 {
			repo = args[1]
		}
		group, err := env.PickGroup(name)
		if err != nil {
			return err
		}
		if repo == "" {
			fmt.Fprintln(env.out, group.Directory())
			return nil
		}
		entry, err := group.Find(repo)
		if err != nil {
			return err
		}
		fmt.Fprintln(env.out, entry.Checkout)
		return nil
	},
}

func init() {
	worktreeCmd.AddCommand(worktreePathCmd)
}
