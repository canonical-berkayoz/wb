package cmd

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type manifestFile struct {
	Branch    string `json:"branch"`
	Workbench string `json:"workbench"`
	Repos     []struct {
		Repo string `json:"repo"`
	} `json:"repos"`
}

func (w *world) manifest(slug string) manifestFile {
	w.t.Helper()
	var m manifestFile
	if err := json.Unmarshal([]byte(read(w.t, w.path(".worktrees", slug, ".wb-worktree.json"))), &m); err != nil {
		w.t.Fatal(err)
	}
	return m
}

func TestCloneLayoutAndFailureIsolation(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.workbench("broken", "repositories:\n  - canonical/missing\n  - canonical/cilium-rocks\n")

	out := w.wbFails("-w", "broken", "clone")
	contains(t, out, "✗ canonical/missing", "✓ canonical/cilium-rocks")
	if !exists(w.path("canonical", "cilium-rocks", ".git")) {
		t.Error("cilium-rocks was not cloned")
	}
	if got := read(t, w.path("canonical", ".gitignore")); got != "*\n" {
		t.Errorf("owner .gitignore = %q", got)
	}

	out = w.wb("-w", "rocks", "clone")
	contains(t, out, "canonical/cilium-rocks (already cloned)")
	if !exists(w.path("canonical", "k8s-snap", ".git")) || !exists(w.path("b", "shared", ".git")) {
		t.Error("rocks repos were not cloned")
	}
	if exists(w.path("a", "shared")) {
		t.Error("a/shared is not in rocks and must not be cloned")
	}
}

func TestCloneAllAndAdd(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.remote("extra/tool")
	w.wb("clone", "--all")
	if !exists(w.path("a", "shared", ".git")) {
		t.Error("--all did not clone a/shared")
	}
	contains(t, w.wbFails("clone", "extra/tool"), "--add")
	w.wb("clone", "extra/tool", "--add")
	if !exists(w.path("extra", "tool", ".git")) {
		t.Error("--add did not clone extra/tool")
	}
}

func TestWorktreeAddAcrossWorkbenchesWithSharedNames(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.wb("-w", "ck8s", "worktree", "add", "Bump-Cilium", "k8s-snap", "canonical/cilium-rocks", "a/shared")
	slug := "bump-cilium"
	for _, name := range []string{"k8s-snap", "cilium-rocks"} {
		if !exists(w.path(".worktrees", slug, name, ".git")) {
			t.Errorf("missing checkout %s", name)
		}
	}
	if !exists(w.checkout(slug, "shared")) {
		t.Error("missing checkout shared")
	}
	if got := read(t, w.path(".worktrees", ".gitignore")); got != "*\n" {
		t.Errorf(".worktrees/.gitignore = %q", got)
	}

	m := w.manifest(slug)
	if m.Branch != "me/bump-cilium" || m.Workbench != "ck8s" || len(m.Repos) != 3 {
		t.Errorf("unexpected manifest: %+v", m)
	}
	if git(t, w.path("canonical", "k8s-snap"), "branch", "--list", "me/bump-cilium") == "" {
		t.Error("branch me/bump-cilium was not created")
	}

	again := w.wb("-w", "ck8s", "worktree", "add", "bump-cilium")
	if got := strings.Count(again, "already present"); got != 3 {
		t.Errorf("already present count = %d\n%s", got, again)
	}

	// a second `shared` forces both folders to <owner>-<repo>
	w.wb("worktree", "add", "bump-cilium", "b/shared")
	if !exists(w.checkout(slug, "a-shared")) || !exists(w.checkout(slug, "b-shared")) || exists(w.checkout(slug, "shared")) {
		t.Error("shared checkouts were not renamed to <owner>-<repo>")
	}
}

func TestWorktreeAddNumberedPicker(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	out, code := w.run("1 4\n", "-w", "ck8s", "worktree", "add", "pick-me")
	if code != 0 {
		t.Fatal(out)
	}
	contains(t, out, "canonical/k8s-snap")
	if !exists(w.checkout("pick-me", "k8s-snap")) || !exists(w.checkout("pick-me", "cilium-rocks")) {
		t.Error("picked repos were not checked out")
	}
	if exists(w.checkout("pick-me", "shared")) {
		t.Error("unpicked repo was checked out")
	}
}

func TestSetupFailureDoesNotBlockOthers(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.workbench("ck8s", "branch_prefix: 'me/'\nrepositories:\n"+
		"  - repo: canonical/k8s-snap\n    setup: 'echo boom; exit 3'\n"+
		"  - repo: a/shared\n    setup: 'echo $WB_REPO $WB_WORKTREE $WB_WORKBENCH > setup.out'\n")
	out := w.wbFails("-w", "ck8s", "worktree", "add", "setup-test", "canonical/k8s-snap", "a/shared")
	contains(t, out, "✗ canonical/k8s-snap setup failed (exit 3)", "boom", "✓ a/shared setup")
	got := strings.TrimSpace(read(t, w.path(".worktrees", "setup-test", "shared", "setup.out")))
	if got != "a/shared setup-test ck8s" {
		t.Errorf("setup.out = %q", got)
	}
}

func TestListReportsDirtyAheadAndUntracked(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.wb("-w", "ck8s", "worktree", "add", "st", "k8s-snap", "a/shared")
	checkout := w.checkout("st", "k8s-snap")
	write(t, checkout+"/new.txt", "x")
	write(t, checkout+"/committed.txt", "y")
	git(t, checkout, "add", "committed.txt")
	git(t, checkout, "commit", "-m", "work")
	git(t, w.path("canonical", "k8s-snap"), "worktree", "add", "-b", "side", w.path("side"))

	out := w.wb("worktree", "list")
	contains(t, out, "st  [ck8s]  branch me/st", "dirty 1", "↑1 ↓0 vs origin/main", "clean",
		"untracked worktrees:", "(side)")
	contains(t, w.wb("-w", "ck8s", "worktree", "list"), "st  [ck8s]")
	scoped := w.wb("-w", "rocks", "worktree", "list")
	contains(t, strings.SplitN(scoped, "untracked", 2)[0], "no worktrees")
}

func TestRemoveRefusesDirtyThenForce(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.wb("-w", "ck8s", "worktree", "add", "fin", "k8s-snap", "a/shared")
	write(t, w.checkout("fin", "k8s-snap")+"/dirty.txt", "x")

	out := w.wbFails("worktree", "remove", "fin")
	contains(t, out, "✗ canonical/k8s-snap kept", "✓ a/shared removed")
	if !exists(w.checkout("fin", "k8s-snap")) || exists(w.checkout("fin", "shared")) {
		t.Error("only the dirty checkout should remain")
	}
	if m := w.manifest("fin"); len(m.Repos) != 1 || m.Repos[0].Repo != "canonical/k8s-snap" {
		t.Errorf("unexpected manifest: %+v", m)
	}

	contains(t, w.wb("worktree", "remove", "fin", "--force"), "worktree fin removed")
	if exists(w.path(".worktrees", "fin")) {
		t.Error("worktree folder should be gone")
	}
	snap := w.path("canonical", "k8s-snap")
	if git(t, snap, "branch", "--list", "me/fin") == "" {
		t.Error("branch must be kept")
	}
	if strings.Contains(git(t, snap, "worktree", "list", "--porcelain"), "prunable") {
		t.Error("stale worktree registrations were left behind")
	}
}

func TestWorktreeNameWithSlashGivesConventionalBranchAndFlatFolder(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	out := w.wb("-w", "ck8s", "worktree", "add", "feat/Add-Rate-Limiting", "k8s-snap")
	contains(t, out, "worktree feat-add-rate-limiting on branch me/feat/add-rate-limiting [ck8s]")
	if !exists(w.checkout("feat-add-rate-limiting", "k8s-snap")) {
		t.Error("folder should be flat: .worktrees/feat-add-rate-limiting/k8s-snap")
	}
	if m := w.manifest("feat-add-rate-limiting"); m.Branch != "me/feat/add-rate-limiting" {
		t.Errorf("branch = %q", m.Branch)
	}
	if git(t, w.path("canonical", "k8s-snap"), "branch", "--list", "me/feat/add-rate-limiting") == "" {
		t.Error("branch was not created")
	}
	// the same name finds the existing group instead of creating another
	contains(t, w.wb("worktree", "add", "feat/add-rate-limiting"), "(already present)")
	if got := strings.TrimSpace(w.wb("worktree", "path", "feat/add-rate-limiting")); got != w.path(".worktrees", "feat-add-rate-limiting") {
		t.Errorf("path = %q", got)
	}
	w.wb("worktree", "remove", "feat/add-rate-limiting")
}

func TestPathAndDiscoveryFromCheckoutAndDeletedCwd(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.wb("-w", "ck8s", "worktree", "add", "nav", "k8s-snap", "a/shared")
	checkout := w.checkout("nav", "k8s-snap")
	if got := strings.TrimSpace(w.wb("worktree", "path", "nav", "k8s-snap")); got != checkout {
		t.Errorf("path = %q, want %q", got, checkout)
	}
	if got := strings.TrimSpace(w.wb("worktree", "path", "nav")); got != w.path(".worktrees", "nav") {
		t.Errorf("path = %q", got)
	}

	// root and workbench are discovered from the cwd alone
	t.Chdir(checkout)
	out, code := w.runRaw("", "worktree", "list")
	if code != 0 || !strings.Contains(out, "nav  [ck8s]") {
		t.Errorf("worktree list from checkout: %d\n%s", code, out)
	}
	out, code = w.runRaw("", "list")
	if code != 0 || !strings.Contains(out, "ck8s") {
		t.Errorf("list from checkout: %d\n%s", code, out)
	}
	out, code = w.runRaw("", "worktree", "add", "nav", "a/shared")
	if code != 0 || !strings.Contains(out, "[ck8s]") {
		t.Errorf("add from checkout: %d\n%s", code, out)
	}

	// removing the directory we stand in must not break later commands
	if err := os.RemoveAll(checkout); err != nil {
		t.Fatal(err)
	}
	contains(t, w.wb("worktree", "remove", "nav", "--force"), "worktree nav removed")
}

func TestWorkbenchInferredFromPoolRepoCwd(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.wb("-w", "ck8s", "clone")
	t.Chdir(w.path("a", "shared"))
	out, code := w.runRaw("", "worktree", "add", "inferred", "a/shared")
	if code != 0 {
		t.Fatal(out)
	}
	contains(t, out, "[ck8s]")
}

func TestSyncFastForwardsCleanAndSkipsDirty(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.wb("-w", "ck8s", "clone")

	advance := func(full string) {
		work := w.remotes + "/_work/" + full
		write(t, work+"/more.txt", "more")
		git(t, work, "add", ".")
		git(t, work, "commit", "-m", "more")
		git(t, work, "push", w.remotes+"/"+full+".git", "HEAD:main")
	}
	advance("canonical/k8s-snap")
	advance("a/shared")
	write(t, w.path("a", "shared", "scratch.txt"), "dirty")

	out := w.wb("-w", "ck8s", "sync")
	snapLine := lineWith(t, out, "canonical/k8s-snap")
	sharedLine := lineWith(t, out, "a/shared")
	contains(t, snapLine, "fast-forwarded", "↑0 ↓0")
	contains(t, sharedLine, "not fast-forwarded: working tree is dirty")
	if !exists(w.path("canonical", "k8s-snap", "more.txt")) {
		t.Error("clean checkout was not fast-forwarded")
	}
	if exists(w.path("a", "shared", "more.txt")) {
		t.Error("dirty checkout must not be fast-forwarded")
	}
}

func TestListAndNew(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.wb("-w", "ck8s", "clone")
	out := w.wb("list")
	contains(t, out, "ck8s", "2/2 cloned", "CK8s", "rocks", "1/3 cloned")

	w.wb("new", "fresh", "x/y")
	contains(t, read(t, w.path("workbenches", "fresh.yaml")), "- x/y")
	w.wbFails("new", "fresh")
	w.wb("new", "fresh", "--force")
	contains(t, w.wb("list"), "fresh")
}

func TestEveryCommandRegisteredAndDocumented(t *testing.T) {
	root := rootCmd
	want := map[string][]string{
		"":         {"clone", "list", "new", "sync", "worktree"},
		"worktree": {"add", "list", "path", "remove"},
	}
	for parent, commands := range want {
		cmd := root
		if parent != "" {
			var found bool
			for _, c := range root.Commands() {
				if c.Name() == parent {
					cmd, found = c, true
				}
			}
			if !found {
				t.Fatalf("group %q is not registered", parent)
			}
		}
		have := map[string]*cobra.Command{}
		for _, c := range cmd.Commands() {
			have[c.Name()] = c
		}
		for _, name := range commands {
			c, ok := have[name]
			if !ok {
				t.Errorf("%s: command %q is not registered", parent, name)
			} else if c.Short == "" {
				t.Errorf("%s %s has no help text", parent, name)
			}
		}
	}
}

func TestErrorsExitOneWithMessage(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	contains(t, w.wbFails("-w", "ck8s", "worktree", "add", "add rate limiting", "k8s-snap"),
		"Error: worktree name", "must not contain spaces")
	if exists(w.path(".worktrees")) {
		t.Error("nothing should be created for a rejected name")
	}
	out := w.wbFails("-w", "nope", "worktree", "add", "x", "k8s-snap")
	contains(t, out, "Error: unknown workbench \"nope\"", "ck8s")
	contains(t, w.wbFails("worktree", "path", "missing"), "Error: no worktree named")
}
