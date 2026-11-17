package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/canonical/wb/internal/hooks"
	"github.com/canonical/wb/internal/manifest"
	"github.com/canonical/wb/internal/worktree"
)

var worktreeRemoveForce bool

var worktreeRemoveCmd = &cobra.Command{
	Use:   "remove [NAME] [REPO...]",
	Short: "Remove a worktree's checkouts (branches are kept)",
	RunE: func(cmd *cobra.Command, args []string) error {
		var name string
		if len(args) > 0 {
			name, args = args[0], args[1:]
		}
		group, err := env.PickGroup(name)
		if err != nil {
			return err
		}
		selected := append([]*manifest.RepoEntry{}, group.Repos...)
		if len(args) > 0 {
			selected = selected[:0]
			for _, arg := range args {
				entry, err := group.Find(arg)
				if err != nil {
					return err
				}
				selected = append(selected, entry)
			}
		}
		if _, err := env.Hooks(); err != nil {
			return err
		}
		removal, ok, err := worktree.RemoveRepos(group, selected, worktreeRemoveForce, func(line string) { fmt.Fprintln(env.out, line) })
		if err != nil {
			return err
		}
		var events []hooks.Event
		if len(removal.Removed) > 0 {
			events = append(events, hooks.Event{Hook: hooks.PostWorktreeRepoRemove, Group: group, Repos: removedRepos(removal.Removed)})
		}
		if removal.Deleted {
			events = append(events, hooks.Event{Hook: hooks.PostWorktreeDelete, Group: group})
		}
		if !env.runHooks(events...) || !ok {
			return errSilent
		}
		return nil
	},
}

func init() {
	worktreeCmd.AddCommand(worktreeRemoveCmd)
	worktreeRemoveCmd.Flags().BoolVar(&worktreeRemoveForce, "force", false, "remove dirty checkouts too")
}
