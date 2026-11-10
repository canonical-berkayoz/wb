// Package worktree creates and removes same-branch worktrees across pool repos.
package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func addCheckout(root string, cfg *config.Config, group *manifest.Group, full, checkout, baseOpt string) (prepared, error) {
	source, err := pool.EnsureCloned(root, full)
	if err != nil {
		return prepared{}, err
	}
	if _, err := gitx.Run(source, "fetch", "origin"); err != nil {
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
	entry := &manifest.RepoEntry{Repo: full, Checkout: checkout, Source: source, Base: base}
	if _, err := os.Stat(checkout); err == nil {
		if registered(source, checkout) {
			return prepared{entry, false}, nil
		}
		return prepared{}, fmt.Errorf("%s exists but is not a worktree of %s", checkout, full)
	}
	if gitx.BranchExists(source, group.Branch) {
		_, err = gitx.Run(source, "worktree", "add", checkout, group.Branch)
	} else {
		_, err = gitx.Run(source, "worktree", "add", "--no-track", "-b", group.Branch, checkout, base)
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

// AddRepos checks out repos into the group, saves the manifest, then runs setup.
// It returns true when everything succeeded.
func AddRepos(cfg *config.Config, group *manifest.Group, repos []string, baseOpt string, report Report) (bool, error) {
	root := group.Root
	var todo []string
	seen := map[string]bool{}
	for _, full := range repos {
		if seen[full] {
			continue
		}
		seen[full] = true
		if group.Entry(full) != nil {
			report(fmt.Sprintf("= %s (already present)", full))
			continue
		}
		todo = append(todo, full)
	}
	if len(todo) == 0 {
		return true, nil
	}

	all := make([]string, 0, len(group.Repos)+len(todo))
	for _, entry := range group.Repos {
		all = append(all, entry.Repo)
	}
	folders := names.CheckoutNames(append(all, todo...))
	if err := pool.EnsureIgnore(filepath.Join(root, manifest.WorktreesDir)); err != nil {
		return false, err
	}
	if err := os.MkdirAll(group.Directory(), 0o755); err != nil {
		return false, err
	}
	if err := renameExisting(group, folders, report); err != nil {
		return false, err
	}

	results := map[string]prepared{}
	ok := true
	for outcome := range parallel.Run(todo, parallel.DefaultWorkers, func(full string) (prepared, error) {
		return addCheckout(root, cfg, group, full, filepath.Join(group.Directory(), folders[full]), baseOpt)
	}) {
		if outcome.Err != nil {
			ok = false
			report(fmt.Sprintf("✗ %s: %v", outcome.Item, outcome.Err))
			continue
		}
		results[outcome.Item] = outcome.Value
		rel, _ := filepath.Rel(root, outcome.Value.entry.Checkout)
		if outcome.Value.created {
			report(fmt.Sprintf("+ %s → %s (%.0fs)", outcome.Item, rel, outcome.Elapsed.Seconds()))
		} else {
			report(fmt.Sprintf("= %s → %s (already present)", outcome.Item, rel))
		}
	}

	var fresh []*manifest.RepoEntry
	for _, full := range todo {
		result, present := results[full]
		if !present {
			continue
		}
		group.Repos = append(group.Repos, result.entry)
		if result.created && cfg.SettingsFor(full).Setup != "" {
			fresh = append(fresh, result.entry)
		}
	}
	if len(group.Repos) > 0 {
		if err := manifest.Save(group); err != nil {
			return false, err
		}
	} else if _, err := os.Stat(group.Directory()); err == nil {
		manifest.Delete(group)
	}

	if !runAllSetup(cfg, group, fresh, report) {
		ok = false
	}
	return ok, nil
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

// RemoveRepos removes the selected checkouts and keeps refused ones. It returns
// true when everything selected was removed.
func RemoveRepos(group *manifest.Group, selected []*manifest.RepoEntry, force bool, report Report) (bool, error) {
	removed := map[*manifest.RepoEntry]bool{}
	sources := map[string]bool{}
	for _, entry := range selected {
		sources[entry.Source] = true
		if _, err := os.Stat(entry.Checkout); err != nil {
			report(fmt.Sprintf("✓ %s (checkout already gone)", entry.Repo))
			removed[entry] = true
			continue
		}
		args := []string{"worktree", "remove"}
		if force {
			args = append(args, "--force")
		}
		res, err := gitx.Attempt(entry.Source, append(args, entry.Checkout)...)
		if err != nil {
			return false, err
		}
		if res.OK() {
			report(fmt.Sprintf("✓ %s removed", entry.Repo))
			removed[entry] = true
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
			return false, err
		}
		refused := len(selected) - len(removed)
		if refused > 0 {
			report("some checkouts were kept; inspect them, or rerun with --force to discard changes")
		}
		return refused == 0, nil
	}
	if manifest.Delete(group) {
		report(fmt.Sprintf("worktree %s removed (branch %s kept)", group.Slug, group.Branch))
	} else {
		report(fmt.Sprintf("note: %s still has content and was left in place", group.Directory()))
	}
	return true, nil
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
