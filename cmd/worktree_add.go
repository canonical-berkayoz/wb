package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/canonical/wb/internal/config"
	"github.com/canonical/wb/internal/gitx"
	"github.com/canonical/wb/internal/hooks"
	"github.com/canonical/wb/internal/manifest"
	"github.com/canonical/wb/internal/names"
	"github.com/canonical/wb/internal/pick"
	"github.com/canonical/wb/internal/worktree"
)

var (
	worktreeAddBase string
	worktreeAddAdd  bool

	worktreeAddCreate bool
)

// parseRepoArg splits REPO[@BRANCH | @REMOTE:BRANCH] into the repo and the requested branch.
func parseRepoArg(arg string) (repo, branch, remote string, err error) {
	repo, ref, hasRef := strings.Cut(arg, "@")
	if !hasRef {
		return repo, "", "", nil
	}
	branch = ref
	if before, after, found := strings.Cut(ref, ":"); found {
		remote, branch = before, after
		if remote == "" || strings.ContainsAny(remote, " \t") {
			return "", "", "", fmt.Errorf("%q: invalid remote name in %s", arg, ref)
		}
	}
	if !gitx.ValidBranchName(branch) {
		return "", "", "", fmt.Errorf("%q: %q is not a valid branch name", arg, branch)
	}
	return repo, branch, remote, nil
}

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

// openGroup loads the worktree called name, or describes a new one (not saved yet)
// on the workbench's branch prefix. The workbench is nil for an existing group.
func (e *Env) openGroup(name string) (group, existing *manifest.Group, workbench *config.Workbench, err error) {
	slug, err := names.Slugify(name)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err = e.Root(); err != nil {
		return nil, nil, nil, err
	}
	if existing, err = manifest.Load(e.rootDir, slug); err != nil || existing != nil {
		return existing, existing, nil, err
	}
	if workbench, err = e.Workbench(); err != nil {
		return nil, nil, nil, err
	}
	branchName, err := names.BranchName(name)
	if err != nil {
		return nil, nil, nil, err
	}
	branch := workbench.BranchPrefix + branchName
	if !gitx.ValidBranchName(branch) {
		return nil, nil, nil, fmt.Errorf("%q is not a valid branch name", branch)
	}
	return manifest.NewGroup(e.rootDir, name, slug, branch, workbench.Name), nil, workbench, nil
}

func (e *Env) worktreeName(args []string) (string, []string, error) {
	if len(args) > 0 {
		return args[0], args[1:], nil
	}
	name, err := e.picker.Prompt("Worktree name, e.g. feat/add-rate-limiting")
	return name, nil, err
}

func explicitBranches(specs []worktree.RepoSpec) bool {
	for _, spec := range specs {
		if spec.Branch != "" {
			return true
		}
	}
	return false
}

var worktreeAddCmd = &cobra.Command{
	Use:   "add [NAME] [REPO[@BRANCH | @REMOTE:BRANCH]...]",
	Short: "Create a worktree per repo (new branch, or an existing one with REPO@BRANCH), or grow an existing group",
	Long: `Add repos to the worktree NAME, creating it if missing.

  REPO                  a new branch named after the worktree, from the default branch
  REPO@BRANCH           exactly BRANCH (case kept, no prefix): the local branch, or one created
                        from origin/BRANCH with that upstream so "git push" updates it
  REPO@REMOTE:BRANCH    the same, taking BRANCH from the git remote REMOTE (e.g. a fork)

Each repo can be on a different branch. A BRANCH that exists nowhere is an error unless
--create is given, which starts it from the base. wb never adds remotes: use "git remote add".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := env.Config()
		if err != nil {
			return err
		}
		name, args, err := env.worktreeName(args)
		if err != nil {
			return err
		}
		group, existing, workbench, err := env.openGroup(name)
		if err != nil {
			return err
		}
		slug := group.Slug
		if _, err := env.Hooks(); err != nil {
			return err
		}

		var selected []worktree.RepoSpec
		switch {
		case len(args) > 0:
			for _, arg := range args {
				name, branch, remote, err := parseRepoArg(arg)
				if err != nil {
					return err
				}
				full, err := env.Resolve(name, worktreeAddAdd)
				if err != nil {
					return err
				}
				selected = append(selected, worktree.RepoSpec{Repo: full, Branch: branch, Remote: remote})
			}
		case existing != nil:
			for _, entry := range existing.Repos {
				selected = append(selected, worktree.RepoSpec{Repo: entry.Repo})
			}
		default:
			picked, err := env.pickRepos(workbench)
			if err != nil {
				return err
			}
			for _, full := range picked {
				selected = append(selected, worktree.RepoSpec{Repo: full})
			}
		}

		if explicitBranches(selected) {
			fmt.Fprintf(env.out, "worktree %s [%s]\n", slug, group.Workbench)
		} else {
			fmt.Fprintf(env.out, "worktree %s on branch %s [%s]\n", slug, group.Branch, group.Workbench)
		}
		if existing != nil && len(selected) == 0 {
			fmt.Fprintf(env.out, "no repos yet; add some with: wb worktree add %s REPO...\n", slug)
			return nil
		}
		added, ok, err := worktree.AddRepos(cfg, group, selected, worktreeAddBase, worktreeAddCreate, func(line string) { fmt.Fprintln(env.out, line) })
		if err != nil {
			return err
		}
		if len(group.Repos) > 0 {
			fmt.Fprintf(env.out, "→ %s\n", group.Directory())
		}

		var events []hooks.Event
		if existing == nil && len(group.Repos) > 0 {
			events = append(events, hooks.Event{Hook: hooks.PostWorktreeCreate, Group: group})
		}
		repos := addedRepos(added)
		for _, repo := range repos {
			if repo.Created {
				events = append(events, hooks.Event{Hook: hooks.PostWorktreeRepoAdd, Group: group, Repos: repos})
				break
			}
		}
		if !env.runHooks(events...) || !ok {
			return errSilent
		}
		return nil
	},
}

func init() {
	worktreeCmd.AddCommand(worktreeAddCmd)
	worktreeAddCmd.Flags().StringVar(&worktreeAddBase, "base", "", "base ref (default: origin/<default branch>)")
	worktreeAddCmd.Flags().BoolVar(&worktreeAddCreate, "create", false, "start REPO@BRANCH from the base when BRANCH does not exist")
	worktreeAddCmd.Flags().BoolVar(&worktreeAddAdd, "add", false, "allow owner/repo not in any workbench")
}
