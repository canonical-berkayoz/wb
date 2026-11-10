package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/canonical/wb/internal/config"
	"github.com/canonical/wb/internal/gitx"
	"github.com/canonical/wb/internal/manifest"
	"github.com/canonical/wb/internal/names"
	"github.com/canonical/wb/internal/pick"
	"github.com/canonical/wb/internal/worktree"
)

var (
	worktreeAddBase string
	worktreeAddAdd  bool
)

func (e *Env) pickRepos(workbench *config.Workbench) ([]string, error) {
	all, err := e.Pool()
	if err != nil {
		return nil, err
	}
	preferred := map[string]bool{}
	for _, repo := range workbench.Repos {
		preferred[repo] = true
	}
	items := append([]string{}, workbench.Repos...)
	var rest []string
	for _, repo := range all {
		if !preferred[repo] {
			rest = append(rest, repo)
		}
	}
	if len(items) > 0 && len(rest) > 0 {
		items = append(items, pick.Separator)
	}
	return e.picker.Pick(append(items, rest...), true, "repos")
}

var worktreeAddCmd = &cobra.Command{
	Use:   "add [NAME] [REPO...]",
	Short: "Create a worktree per repo on one shared branch, or grow an existing group",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := env.Config()
		if err != nil {
			return err
		}
		var name string
		if len(args) > 0 {
			name, args = args[0], args[1:]
		} else if name, err = env.picker.Prompt("Worktree name, e.g. feat/add-rate-limiting"); err != nil {
			return err
		}
		slug, err := names.Slugify(name)
		if err != nil {
			return err
		}
		existing, err := manifest.Load(env.rootDir, slug)
		if err != nil {
			return err
		}
		group := existing
		var workbench *config.Workbench
		if existing == nil {
			if workbench, err = env.Workbench(); err != nil {
				return err
			}
			branchName, err := names.BranchName(name)
			if err != nil {
				return err
			}
			branch := workbench.BranchPrefix + branchName
			if !gitx.ValidBranchName(branch) {
				return fmt.Errorf("%q is not a valid branch name", branch)
			}
			group = manifest.NewGroup(env.rootDir, slug, branch, workbench.Name)
		}

		var selected []string
		switch {
		case len(args) > 0:
			for _, arg := range args {
				full, err := env.Resolve(arg, worktreeAddAdd)
				if err != nil {
					return err
				}
				selected = append(selected, full)
			}
		case existing != nil:
			for _, entry := range existing.Repos {
				selected = append(selected, entry.Repo)
			}
		default:
			if selected, err = env.pickRepos(workbench); err != nil {
				return err
			}
		}

		fmt.Fprintf(env.out, "worktree %s on branch %s [%s]\n", slug, group.Branch, group.Workbench)
		ok, err := worktree.AddRepos(cfg, group, selected, worktreeAddBase, func(line string) { fmt.Fprintln(env.out, line) })
		if err != nil {
			return err
		}
		if len(group.Repos) > 0 {
			fmt.Fprintf(env.out, "→ %s\n", group.Directory())
		}
		if !ok {
			return errSilent
		}
		return nil
	},
}

func init() {
	worktreeCmd.AddCommand(worktreeAddCmd)
	worktreeAddCmd.Flags().StringVar(&worktreeAddBase, "base", "", "base ref (default: origin/<default branch>)")
	worktreeAddCmd.Flags().BoolVar(&worktreeAddAdd, "add", false, "allow owner/repo not in any workbench")
}
