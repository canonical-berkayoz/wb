// Package names holds the naming rules for repos, workbenches and worktrees.
package names

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	repoRE      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9_.-]+$`)
	workbenchRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	unusableRE  = regexp.MustCompile(`[^a-z0-9._]+`)
	dotsRE      = regexp.MustCompile(`\.{2,}`)
)

func ValidRepo(full string) bool { return repoRE.MatchString(full) }

func ValidWorkbench(name string) bool { return workbenchRE.MatchString(name) }

// Split returns the owner and repo name of "owner/repo".
func Split(full string) (owner, name string) {
	owner, name, _ = strings.Cut(full, "/")
	return owner, name
}

// checkNoSpaces rejects worktree names containing whitespace instead of silently rewriting them.
func checkNoSpaces(name string) error {
	if strings.IndexFunc(name, unicode.IsSpace) >= 0 {
		return fmt.Errorf("worktree name %q must not contain spaces; use dashes, e.g. feat/add-rate-limiting", name)
	}
	return nil
}

// Slugify returns the flat folder name for a worktree name ("feat/x" is "feat-x").
func Slugify(name string) (string, error) {
	if err := checkNoSpaces(name); err != nil {
		return "", err
	}
	slug := unusableRE.ReplaceAllString(strings.ToLower(name), "-")
	slug = strings.Trim(dotsRE.ReplaceAllString(slug, "."), "-._")
	if slug == "" {
		return "", fmt.Errorf("%q does not contain any usable characters for a worktree name", name)
	}
	return slug, nil
}

// BranchName turns a worktree name into a branch name: each "/"-separated
// segment is slugified and the slashes are kept, so "Feat/Add-X" is "feat/add-x".
func BranchName(name string) (string, error) {
	if err := checkNoSpaces(name); err != nil {
		return "", err
	}
	var parts []string
	for _, segment := range strings.Split(name, "/") {
		if slug, err := Slugify(segment); err == nil {
			parts = append(parts, slug)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("%q does not contain any usable characters for a worktree name", name)
	}
	return strings.Join(parts, "/"), nil
}

// CheckoutNames returns the folder name per repo in a worktree group; repos
// sharing a name use "<owner>-<repo>".
func CheckoutNames(repos []string) map[string]string {
	counts := map[string]int{}
	for _, full := range repos {
		_, name := Split(full)
		counts[name]++
	}
	out := make(map[string]string, len(repos))
	for _, full := range repos {
		_, name := Split(full)
		if counts[name] > 1 {
			out[full] = strings.ReplaceAll(full, "/", "-")
		} else {
			out[full] = name
		}
	}
	return out
}
