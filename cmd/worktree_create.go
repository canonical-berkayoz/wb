package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/canonical/wb/internal/hooks"
	"github.com/canonical/wb/internal/worktree"
)

var worktreeCreateCmd = &cobra.Command{
	Use:   "create [NAME]",
	Short: "Create a worktree on a shared branch, then add repos with `worktree add NAME REPO...`",
	Long: `Create the worktree folder, manifest and branch name first, without any repos.
Decide which repos belong to the change as you go and add each with
"wb worktree add NAME REPO...". The folder is available right away via "wb worktree path NAME".`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _, err := env.worktreeName(args)
		if err != nil {
			return err
		}
		group, existing, _, err := env.openGroup(name)
		if err != nil {
			return err
		}
		if _, err := env.Hooks(); err != nil {
			return err
		}
		if existing != nil {
			fmt.Fprintf(env.out, "= worktree %s already exists on branch %s\n", group.Slug, group.Branch)
		} else {
			if err := worktree.Create(group); err != nil {
				return err
			}
			fmt.Fprintf(env.out, "worktree %s on branch %s [%s]\n", group.Slug, group.Branch, group.Workbench)
		}
		fmt.Fprintf(env.out, "→ %s\n", group.Directory())
		if len(group.Repos) == 0 {
			fmt.Fprintf(env.out, "add repos when you know them: wb worktree add %s REPO...\n", group.Slug)
		}
		if existing == nil && !env.runHooks(hooks.Event{Hook: hooks.PostWorktreeCreate, Group: group}) {
			return errSilent
		}
		return nil
	},
}

func init() {
	worktreeCmd.AddCommand(worktreeCreateCmd)
}
