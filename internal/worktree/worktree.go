// Package worktree creates and removes worktrees across pool repos, each repo on its own branch.
package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/canonical/wb/internal/config"
	"github.com/canonical/wb/internal/gitx"
	"github.com/canonical/wb/internal/manifest"
	"github.com/canonical/wb/internal/names"
	"github.com/canonical/wb/internal/parallel"
	"github.com/canonical/wb/internal/pool"
)

const setupTailLines = 15

type Report func(string)

type prepared struct {
	entry   *manifest.RepoEntry
	created bool
}

func registered(source, checkout string) bool {
	target := gitx.RealPath(checkout)
	trees, err := gitx.Worktrees(source)
	if err != nil {
		return false
	}
	for _, tree := range trees {
		if tree.Path == target {
			return true
		}
	}
	return false
}

// renameExisting renames checkouts whose folder must change now that two repos share a name.
func renameExisting(group *manifest.Group, folders map[string]string, report Report) error {
	for _, entry := range group.Repos {
		wanted := filepath.Join(group.Directory(), folders[entry.Repo])
		if entry.Checkout == wanted {
			continue
		}
		if _, err := os.Stat(entry.Checkout); err != nil {
			continue
		}
		if _, err := gitx.Run(entry.Source, "worktree", "move", entry.Checkout, wanted); err != nil {
			return err
		}
		entry.Checkout = wanted
		report(fmt.Sprintf("~ %s: renamed checkout to %s", entry.Repo, filepath.Base(wanted)))
	}
	return nil
}

// RepoSpec is a repo to add and the branch it should be on. An empty Branch means a
// new branch named after the worktree. A non-empty Branch is used exactly as given and
// must already exist, unless the caller allows creating it. Remote names the remote that
// holds Branch; it defaults to origin.
type RepoSpec struct {
	Repo   string
	Branch string
	Remote string
}

func (s RepoSpec) remote() string {
	if s.Remote == "" {
		return "origin"
	}
	return s.Remote
}

func fetch(source, full string, spec RepoSpec) error {
	if _, err := gitx.Run(source, "fetch", "origin"); err != nil {
		return err
	}
	remote := spec.remote()
	if remote == "origin" {
		return nil
	}
	if !slices.Contains(gitx.Remotes(source), remote) {
		return fmt.Errorf("%s has no remote %q; add it with: git -C %s remote add %s URL", full, remote, source, remote)
	}
	_, err := gitx.Run(source, "fetch", remote)
	return err
}

// addBranchWorktree creates the checkout for a requested branch: the local branch if it
// exists, else a local branch tracking <remote>/<branch> so that git push updates it.
func addBranchWorktree(source, checkout, base string, spec RepoSpec, create bool) error {
	remote := spec.remote()
	var err error
	switch {
	case gitx.BranchExists(source, spec.Branch):
		_, err = gitx.Run(source, "worktree", "add", checkout, spec.Branch)
	case gitx.RefExists(source, "refs/remotes/"+remote+"/"+spec.Branch):
		_, err = gitx.Run(source, "worktree", "add", "--track", "-b", spec.Branch, checkout, remote+"/"+spec.Branch)
	case create:
		_, err = gitx.Run(source, "worktree", "add", "--no-track", "-b", spec.Branch, checkout, base)
	default:
		err = fmt.Errorf("branch %q does not exist locally or on %s; check the name, or pass --create to start it from %s", spec.Branch, remote, base)
	}
	return err
}

func addCheckout(root string, cfg *config.Config, group *manifest.Group, spec RepoSpec, checkout, baseOpt string, create bool) (prepared, error) {
	full := spec.Repo
	source, err := pool.EnsureCloned(root, full)
	if err != nil {
		return prepared{}, err
	}
	if err := fetch(source, full, spec); err != nil {
		return prepared{}, err
	}
	base := baseOpt
	if base == "" {
		branch, err := pool.DefaultBranch(root, cfg, full)
		if err != nil {
			return prepared{}, err
		}
		base = "origin/" + branch
	}
	if !gitx.RefExists(source, base) {
		return prepared{}, fmt.Errorf("base %q does not exist in %s", base, full)
	}
	branch := spec.Branch
	if branch == "" {
		branch = group.Branch
	}
	entry := &manifest.RepoEntry{Repo: full, Checkout: checkout, Source: source, Base: base, Branch: branch}
	if _, err := os.Stat(checkout); err == nil {
		if registered(source, checkout) {
			return prepared{entry, false}, nil
		}
		return prepared{}, fmt.Errorf("%s exists but is not a worktree of %s", checkout, full)
	}
	if spec.Branch != "" {
		err = addBranchWorktree(source, checkout, base, spec, create)
	} else if gitx.BranchExists(source, branch) {
		_, err = gitx.Run(source, "worktree", "add", checkout, branch)
	} else {
		_, err = gitx.Run(source, "worktree", "add", "--no-track", "-b", branch, checkout, base)
	}
	if err != nil {
		return prepared{}, err
	}
	return prepared{entry, true}, nil
}

func runSetup(entry *manifest.RepoEntry, command string, group *manifest.Group) (int, string) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = entry.Checkout
	cmd.Env = append(os.Environ(),
		"WB_SOURCE="+entry.Source,
		"WB_CHECKOUT="+entry.Checkout,
		"WB_REPO="+entry.Repo,
		"WB_WORKBENCH="+group.Workbench,
		"WB_WORKTREE="+group.Slug,
		"WB_BRANCH="+entry.Branch,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), string(out)
	}
	return 1, err.Error()
}

// Create saves a group with no repos, so they can be added later.
func Create(group *manifest.Group) error {
	if err := pool.EnsureIgnore(filepath.Join(group.Root, manifest.WorktreesDir)); err != nil {
		return err
	}
	return manifest.Save(group)
}

// Added is a requested repo that is in the group after AddRepos; Created is false
// when it was already there.
type Added struct {
	Entry   *manifest.RepoEntry
	Created bool
}

// AddRepos checks out repos into the group, saves the manifest, then runs setup.
// With create, a requested branch that exists nowhere is started from the base.
// It returns the requested repos that are now in the group, and true when everything succeeded.
func AddRepos(cfg *config.Config, group *manifest.Group, repos []RepoSpec, baseOpt string, create bool, report Report) ([]Added, bool, error) {
	root := group.Root
	var todo []RepoSpec
	var added []Added
	ok := true
	seen := map[string]RepoSpec{}
	for _, spec := range repos {
		if previous, dup := seen[spec.Repo]; dup {
			if spec.Branch != "" && previous.Branch != "" && spec != previous {
				return nil, false, fmt.Errorf("%s is requested twice with different branches", spec.Repo)
			}
			continue
		}
		seen[spec.Repo] = spec
		if entry := group.Entry(spec.Repo); entry != nil {
			if spec.Branch != "" && spec.Branch != entry.Branch {
				ok = false
				report(fmt.Sprintf("✗ %s: already in this worktree on branch %s, not %s", spec.Repo, entry.Branch, spec.Branch))
				continue
			}
			report(fmt.Sprintf("= %s (already present)", spec.Repo))
			added = append(added, Added{entry, false})
			continue
		}
		todo = append(todo, spec)
	}
	if len(todo) == 0 {
		return added, ok, nil
	}

	all := make([]string, 0, len(group.Repos)+len(todo))
	for _, entry := range group.Repos {
		all = append(all, entry.Repo)
	}
	for _, spec := range todo {
		all = append(all, spec.Repo)
	}
	folders := names.CheckoutNames(all)
	if err := pool.EnsureIgnore(filepath.Join(root, manifest.WorktreesDir)); err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(group.Directory(), 0o755); err != nil {
		return nil, false, err
	}
	if err := renameExisting(group, folders, report); err != nil {
		return nil, false, err
	}

	results := map[string]prepared{}
	for outcome := range parallel.Run(todo, parallel.DefaultWorkers, func(spec RepoSpec) (prepared, error) {
		return addCheckout(root, cfg, group, spec, filepath.Join(group.Directory(), folders[spec.Repo]), baseOpt, create)
	}) {
		full := outcome.Item.Repo
		if outcome.Err != nil {
			ok = false
			report(fmt.Sprintf("✗ %s: %v", full, outcome.Err))
			continue
		}
		results[full] = outcome.Value
		rel, _ := filepath.Rel(root, outcome.Value.entry.Checkout)
		if outcome.Value.created {
			report(fmt.Sprintf("+ %s → %s on %s (%.0fs)", full, rel, outcome.Value.entry.Branch, outcome.Elapsed.Seconds()))
		} else {
			report(fmt.Sprintf("= %s → %s (already present)", full, rel))
		}
	}

	var fresh []*manifest.RepoEntry
	for _, spec := range todo {
		full := spec.Repo
		result, present := results[full]
		if !present {
			continue
		}
		group.Repos = append(group.Repos, result.entry)
		added = append(added, Added{result.entry, result.created})
		if result.created && cfg.SettingsFor(full).Setup != "" {
			fresh = append(fresh, result.entry)
		}
	}
	if len(group.Repos) > 0 {
		if err := manifest.Save(group); err != nil {
			return nil, false, err
		}
	} else if _, err := os.Stat(group.ManifestPath()); err != nil {
		// Nothing was added to a group that was never saved: leave no trace.
		manifest.Delete(group)
	}

	if !runAllSetup(cfg, group, fresh, report) {
		ok = false
	}
	return added, ok, nil
}

type setupResult struct {
	code   int
	output string
}

func runAllSetup(cfg *config.Config, group *manifest.Group, entries []*manifest.RepoEntry, report Report) bool {
	ok := true
	for outcome := range parallel.Run(entries, parallel.DefaultWorkers, func(entry *manifest.RepoEntry) (setupResult, error) {
		code, output := runSetup(entry, cfg.SettingsFor(entry.Repo).Setup, group)
		return setupResult{code, output}, nil
	}) {
		name := outcome.Item.Repo
		if outcome.Value.code == 0 {
			report(fmt.Sprintf("✓ %s setup (%.0fs)", name, outcome.Elapsed.Seconds()))
			continue
		}
		ok = false
		report(fmt.Sprintf("✗ %s setup failed (exit %d); last %d lines:", name, outcome.Value.code, setupTailLines))
		lines := strings.Split(strings.TrimRight(outcome.Value.output, "\n"), "\n")
		if len(lines) > setupTailLines {
			lines = lines[len(lines)-setupTailLines:]
		}
		for _, line := range lines {
			report("    " + line)
		}
	}
	return ok
}

// Removal is what RemoveRepos took away. Deleted is true when no repos are left,
// so the worktree itself is gone.
type Removal struct {
	Removed []*manifest.RepoEntry
	Deleted bool
}

// RemoveRepos removes the selected checkouts and keeps refused ones. It returns
// true when everything selected was removed.
func RemoveRepos(group *manifest.Group, selected []*manifest.RepoEntry, force bool, report Report) (Removal, bool, error) {
	removed := map[*manifest.RepoEntry]bool{}
	var result Removal
	sources := map[string]bool{}
	for _, entry := range selected {
		sources[entry.Source] = true
		if _, err := os.Stat(entry.Checkout); err != nil {
			report(fmt.Sprintf("✓ %s (checkout already gone)", entry.Repo))
			removed[entry] = true
			result.Removed = append(result.Removed, entry)
			continue
		}
		args := []string{"worktree", "remove"}
		if force {
			args = append(args, "--force")
		}
		res, err := gitx.Attempt(entry.Source, append(args, entry.Checkout)...)
		if err != nil {
			return result, false, err
		}
		if res.OK() {
			report(fmt.Sprintf("✓ %s removed", entry.Repo))
			removed[entry] = true
			result.Removed = append(result.Removed, entry)
		} else {
			report(fmt.Sprintf("✗ %s kept: %s", entry.Repo, gitx.FirstLine(res.Stderr)))
		}
	}

	sorted := make([]string, 0, len(sources))
	for source := range sources {
		sorted = append(sorted, source)
	}
	sort.Strings(sorted)
	for _, source := range sorted {
		if _, err := os.Stat(source); err == nil {
			_, _ = gitx.Attempt(source, "worktree", "prune")
		}
	}

	var remaining []*manifest.RepoEntry
	for _, entry := range group.Repos {
		if !removed[entry] {
			remaining = append(remaining, entry)
		}
	}
	group.Repos = remaining
	if len(remaining) > 0 {
		if err := manifest.Save(group); err != nil {
			return result, false, err
		}
		refused := len(selected) - len(removed)
		if refused > 0 {
			report("some checkouts were kept; inspect them, or rerun with --force to discard changes")
		}
		return result, refused == 0, nil
	}
	result.Deleted = true
	if manifest.Delete(group) {
		report(fmt.Sprintf("worktree %s removed (branches kept)", group.Slug))
	} else {
		report(fmt.Sprintf("note: %s still has content and was left in place", group.Directory()))
	}
	return result, true, nil
}

// RepoStatus summarizes one checkout as (changes, divergence).
func RepoStatus(entry *manifest.RepoEntry) (string, string) {
	if _, err := os.Stat(entry.Checkout); err != nil {
		return "missing", "-"
	}
	changes := "clean"
	if dirty, err := gitx.DirtyCount(entry.Checkout); err != nil {
		changes = "unknown"
	} else if dirty > 0 {
		changes = fmt.Sprintf("dirty %d", dirty)
	}
	if ahead, behind, ok := gitx.AheadBehind(entry.Checkout, entry.Base); ok {
		return changes, fmt.Sprintf("↑%d ↓%d vs %s", ahead, behind, entry.Base)
	}
	return changes, "? vs " + entry.Base
}

type Untracked struct {
	Repo   string
	Path   string
	Branch string
}

// UntrackedWorktrees lists linked git worktrees of pool repos that no manifest knows about.
func UntrackedWorktrees(root string, cfg *config.Config, groups []*manifest.Group) []Untracked {
	tracked := map[string]bool{}
	for _, group := range groups {
		for _, entry := range group.Repos {
			tracked[gitx.RealPath(entry.Checkout)] = true
		}
	}
	var found []Untracked
	for _, full := range pool.Repos(root, cfg) {
		if !pool.IsCloned(root, full) {
			continue
		}
		trees, err := gitx.Worktrees(pool.RepoDir(root, full))
		if err != nil || len(trees) == 0 {
			continue
		}
		for _, tree := range trees[1:] {
			if !tracked[tree.Path] && !tree.Prunable && !tree.Bare {
				found = append(found, Untracked{full, tree.Path, tree.Branch})
			}
		}
	}
	return found
}
