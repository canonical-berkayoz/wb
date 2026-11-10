// Package gitx runs the git binary and parses the few outputs wb needs.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Result struct {
	Stdout, Stderr string
	Code           int
}

func (r Result) OK() bool { return r.Code == 0 }

// Attempt runs git and returns its result; the error is only for failing to start git.
func Attempt(dir string, args ...string) (Result, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		res.Code = exit.ExitCode()
	case errors.Is(err, exec.ErrNotFound):
		return res, errors.New("git was not found on PATH")
	default:
		return res, err
	}
	return res, nil
}

// Run runs git and returns trimmed stdout, or an error carrying git's message.
func Run(dir string, args ...string) (string, error) {
	res, err := Attempt(dir, args...)
	if err != nil {
		return "", err
	}
	if !res.OK() {
		detail := strings.TrimSpace(res.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(res.Stdout)
		}
		if detail == "" {
			detail = fmt.Sprintf("exit code %d", res.Code)
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), detail)
	}
	return strings.TrimSpace(res.Stdout), nil
}

func FirstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

func succeeds(dir string, args ...string) bool {
	res, err := Attempt(dir, args...)
	return err == nil && res.OK()
}

func output(dir string, args ...string) string {
	res, err := Attempt(dir, args...)
	if err != nil || !res.OK() {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

func CurrentBranch(repo string) string {
	return output(repo, "symbolic-ref", "--short", "-q", "HEAD")
}

func DirtyCount(repo string) (int, error) {
	out, err := Run(repo, "status", "--porcelain")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			count++
		}
	}
	return count, nil
}

func RefExists(repo, ref string) bool {
	return succeeds(repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
}

func BranchExists(repo, branch string) bool {
	return succeeds(repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
}

// AheadBehind returns how many commits HEAD is ahead of / behind ref; ok is
// false when ref cannot be resolved.
func AheadBehind(repo, ref string) (ahead, behind int, ok bool) {
	res, err := Attempt(repo, "rev-list", "--left-right", "--count", "HEAD..."+ref)
	if err != nil || !res.OK() {
		return 0, 0, false
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) != 2 {
		return 0, 0, false
	}
	ahead, errA := strconv.Atoi(fields[0])
	behind, errB := strconv.Atoi(fields[1])
	return ahead, behind, errA == nil && errB == nil
}

func Upstream(repo string) string {
	return output(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
}

func OriginDefaultBranch(repo string) string {
	if head := output(repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); strings.HasPrefix(head, "origin/") {
		return strings.TrimPrefix(head, "origin/")
	}
	for _, candidate := range []string{"main", "master"} {
		if RefExists(repo, "origin/"+candidate) {
			return candidate
		}
	}
	return ""
}

func ValidBranchName(name string) bool {
	return succeeds("", "check-ref-format", "--branch", name)
}

type Worktree struct {
	Path     string
	Branch   string
	Prunable bool
	Bare     bool
}

// Worktrees lists all worktrees of a repo; the first entry is the main checkout.
func Worktrees(repo string) ([]Worktree, error) {
	out, err := Run(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var found []Worktree
	for _, block := range strings.Split(out, "\n\n") {
		var tree Worktree
		hasPath := false
		for _, line := range strings.Split(block, "\n") {
			key, value, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				tree.Path, hasPath = RealPath(value), true
			case "branch":
				tree.Branch = strings.TrimPrefix(value, "refs/heads/")
			case "prunable":
				tree.Prunable = true
			case "bare":
				tree.Bare = true
			}
		}
		if hasPath {
			found = append(found, tree)
		}
	}
	return found, nil
}

// RealPath returns an absolute, symlink-free path where possible.
func RealPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}
