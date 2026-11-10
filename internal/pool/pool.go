// Package pool manages the shared repo pool: one clone per repo at <root>/<owner>/<repo>.
package pool

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/canonical/wb/internal/config"
	"github.com/canonical/wb/internal/gitx"
	"github.com/canonical/wb/internal/names"
)

const defaultCloneURL = "git@github.com:{owner}/{repo}.git"

func RepoDir(root, full string) string {
	owner, name := names.Split(full)
	return filepath.Join(root, owner, name)
}

func IsCloned(root, full string) bool {
	_, err := os.Stat(filepath.Join(RepoDir(root, full), ".git"))
	return err == nil
}

// CloneURL expands $WB_CLONE_URL ({owner} and {repo} placeholders), SSH by default.
func CloneURL(full string) string {
	template := os.Getenv("WB_CLONE_URL")
	if template == "" {
		template = defaultCloneURL
	}
	owner, name := names.Split(full)
	return strings.NewReplacer("{owner}", owner, "{repo}", name).Replace(template)
}

// EnsureIgnore makes a folder ignore its whole content, so a root repo never tracks the pool.
func EnsureIgnore(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	marker := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	return os.WriteFile(marker, []byte("*\n"), 0o644)
}

func Clone(root, full string) (string, error) {
	dest := RepoDir(root, full)
	if err := EnsureIgnore(filepath.Dir(dest)); err != nil {
		return "", err
	}
	if _, err := gitx.Run("", "clone", CloneURL(full), dest); err != nil {
		return "", err
	}
	return dest, nil
}

func EnsureCloned(root, full string) (string, error) {
	if IsCloned(root, full) {
		return RepoDir(root, full), nil
	}
	return Clone(root, full)
}

// OnDisk lists pool repos that exist as <root>/<owner>/<repo> checkouts, listed or not.
func OnDisk(root string) []string {
	var found []string
	owners, _ := os.ReadDir(root)
	for _, owner := range owners {
		if strings.HasPrefix(owner.Name(), ".") || !owner.IsDir() {
			continue
		}
		repos, _ := os.ReadDir(filepath.Join(root, owner.Name()))
		for _, repo := range repos {
			full := owner.Name() + "/" + repo.Name()
			if names.ValidRepo(full) && IsCloned(root, full) {
				found = append(found, full)
			}
		}
	}
	return found
}

func Repos(root string, cfg *config.Config) []string {
	set := map[string]bool{}
	for _, repo := range cfg.KnownRepos() {
		set[repo] = true
	}
	for _, repo := range OnDisk(root) {
		set[repo] = true
	}
	repos := make([]string, 0, len(set))
	for repo := range set {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	return repos
}

// Resolve maps a REPO argument (unique name or owner/repo) to "owner/repo".
func Resolve(arg string, known []string, add bool) (string, error) {
	if strings.Contains(arg, "/") {
		if !names.ValidRepo(arg) {
			return "", fmt.Errorf("%q is not a valid 'owner/repo'", arg)
		}
		for _, repo := range known {
			if repo == arg {
				return arg, nil
			}
		}
		if add {
			return arg, nil
		}
		return "", fmt.Errorf("%s is not in the pool; pass --add to clone it anyway", arg)
	}
	var matches []string
	for _, repo := range known {
		if _, name := names.Split(repo); name == arg {
			matches = append(matches, repo)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("unknown repo %q; use owner/repo with --add to add a new one", arg)
	}
	return "", fmt.Errorf("%q is ambiguous (%s); use owner/repo", arg, strings.Join(matches, ", "))
}

func DefaultBranch(root string, cfg *config.Config, full string) (string, error) {
	if configured := cfg.SettingsFor(full).DefaultBranch; configured != "" {
		return configured, nil
	}
	if found := gitx.OriginDefaultBranch(RepoDir(root, full)); found != "" {
		return found, nil
	}
	return "", fmt.Errorf("%s: cannot determine the default branch; set default_branch", full)
}

// Sync fetches one main checkout, fast-forwards it when safe, and describes the result.
func Sync(root string, cfg *config.Config, full string) (string, error) {
	dest := RepoDir(root, full)
	if !IsCloned(root, full) {
		return fmt.Sprintf("%-40s not cloned", full), nil
	}
	if _, err := gitx.Run(dest, "fetch", "--prune", "origin"); err != nil {
		return "", err
	}
	target, err := DefaultBranch(root, cfg, full)
	if err != nil {
		return "", err
	}
	branch := gitx.CurrentBranch(dest)
	dirty, err := gitx.DirtyCount(dest)
	if err != nil {
		return "", err
	}
	note := ""
	if branch == target {
		if ahead, behind, ok := gitx.AheadBehind(dest, "origin/"+target); ok && behind > 0 {
			switch {
			case dirty > 0:
				note = "not fast-forwarded: working tree is dirty"
			case ahead > 0:
				note = "not fast-forwarded: diverged from origin"
			default:
				if _, err := gitx.Run(dest, "merge", "--ff-only", "origin/"+target); err != nil {
					return "", err
				}
				note = "fast-forwarded"
			}
		}
	} else {
		shown := branch
		if shown == "" {
			shown = "detached"
		}
		note = fmt.Sprintf("default branch %s not updated (checked out: %s)", target, shown)
	}
	reference := gitx.Upstream(dest)
	if reference == "" {
		reference = "origin/" + target
	}
	divergence := "-"
	if ahead, behind, ok := gitx.AheadBehind(dest, reference); ok {
		divergence = fmt.Sprintf("↑%d ↓%d", ahead, behind)
	}
	if branch == "" {
		branch = "(detached)"
	}
	line := fmt.Sprintf("%-40s %-24s %-8s dirty %-3d %s", full, branch, divergence, dirty, note)
	return strings.TrimRight(line, " "), nil
}
