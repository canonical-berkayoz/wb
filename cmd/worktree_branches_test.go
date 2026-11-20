package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pushBranch adds a commit on a new branch of the bare remote owner/repo.
func (w *world) pushBranch(full, branch string) {
	w.t.Helper()
	work := w.t.TempDir()
	git(w.t, work, "clone", filepath.Join(w.remotes, full+".git"), "w")
	dir := filepath.Join(work, "w")
	git(w.t, dir, "checkout", "-b", branch)
	write(w.t, filepath.Join(dir, "change.txt"), branch)
	git(w.t, dir, "add", ".")
	git(w.t, dir, "commit", "-m", branch)
	git(w.t, dir, "push", "origin", branch)
}

func TestWorktreeAddContinuesExistingBranchesPerRepo(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.recordAllHooks()
	w.pushBranch("canonical/k8s-snap", "Fix/Mixed-Case")
	w.pushBranch("canonical/cilium-rocks", "feature/other")

	out := w.wb("-w", "ck8s", "worktree", "add", "pr-work", "k8s-snap@Fix/Mixed-Case", "cilium-rocks@feature/other", "a/shared")
	contains(t, out, "worktree pr-work [ck8s]")

	for repo, want := range map[string]string{"k8s-snap": "Fix/Mixed-Case", "cilium-rocks": "feature/other", "shared": "me/pr-work"} {
		if got := git(t, w.checkout("pr-work", repo), "branch", "--show-current"); got != want {
			t.Errorf("%s on %q, want %q", repo, got, want)
		}
	}
	if got := git(t, w.checkout("pr-work", "k8s-snap"), "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/Fix/Mixed-Case" {
		t.Errorf("upstream = %q", got)
	}

	m := w.manifest("pr-work")
	branches := map[string]string{}
	for _, repo := range m.Repos {
		branches[repo.Repo] = repo.Branch
	}
	if branches["canonical/k8s-snap"] != "Fix/Mixed-Case" || branches["canonical/cilium-rocks"] != "feature/other" || branches["a/shared"] != "me/pr-work" {
		t.Errorf("manifest branches = %v", branches)
	}

	_, _, p := w.hookRan("post_worktree_repo_add")
	got := map[string]string{}
	for _, repo := range p.Repos {
		got[repo.Name] = repo.Branch
	}
	if got["canonical/k8s-snap"] != "Fix/Mixed-Case" || got["a/shared"] != "me/pr-work" {
		t.Errorf("hook branches = %v", got)
	}

	list := w.wb("worktree", "list")
	contains(t, list, "pr-work  [ck8s]", "(branch Fix/Mixed-Case)", "(branch feature/other)")

	// the same repo again on another branch is refused, on the same branch it is a no-op
	contains(t, w.wbFails("worktree", "add", "pr-work", "k8s-snap@other"), "already in this worktree on branch Fix/Mixed-Case")
	contains(t, w.wb("worktree", "add", "pr-work", "k8s-snap@Fix/Mixed-Case"), "already present")
}

func TestWorktreeAddMissingBranchNeedsCreate(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()

	out := w.wbFails("-w", "ck8s", "worktree", "add", "typo", "k8s-snap@fix/tpyo")
	contains(t, out, `branch "fix/tpyo" does not exist`, "--create")
	if exists(w.checkout("typo", "k8s-snap")) {
		t.Error("checkout must not exist after a failed add")
	}

	w.wb("-w", "ck8s", "worktree", "add", "typo", "k8s-snap@fix/typo", "--create")
	if got := git(t, w.checkout("typo", "k8s-snap"), "branch", "--show-current"); got != "fix/typo" {
		t.Errorf("branch = %q", got)
	}
	if got := w.manifest("typo").Repos[0].Branch; got != "fix/typo" {
		t.Errorf("manifest branch = %q", got)
	}
	contains(t, w.wbFails("worktree", "add", "typo", "a/shared@bad..name"), "not a valid branch name")
}

func TestWorktreeAddBranchFromForkRemote(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.remote("fork/k8s-snap")
	w.pushBranch("fork/k8s-snap", "contrib/patch")
	w.wb("clone", "canonical/k8s-snap")

	contains(t, w.wbFails("-w", "ck8s", "worktree", "add", "fork-pr", "k8s-snap@fork:contrib/patch"), `no remote "fork"`)

	pool := w.path("canonical", "k8s-snap")
	git(t, pool, "remote", "add", "fork", "file://"+filepath.Join(w.remotes, "fork", "k8s-snap.git"))
	w.wb("-w", "ck8s", "worktree", "add", "fork-pr", "k8s-snap@fork:contrib/patch")

	checkout := w.checkout("fork-pr", "k8s-snap")
	if got := git(t, checkout, "branch", "--show-current"); got != "contrib/patch" {
		t.Errorf("branch = %q", got)
	}
	if got := git(t, checkout, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "fork/contrib/patch" {
		t.Errorf("upstream = %q", got)
	}
	if _, err := os.Stat(filepath.Join(checkout, "change.txt")); err != nil {
		t.Errorf("fork content missing: %v", err)
	}
	if out := git(t, checkout, "push", "--dry-run"); strings.Contains(out, "origin") {
		t.Errorf("push would go to origin: %s", out)
	}
}
