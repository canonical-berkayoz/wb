package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type hookPayload struct {
	Hook        string `json:"hook"`
	Root        string `json:"root"`
	Workbench   string `json:"workbench"`
	Worktree    string `json:"worktree"`
	WorktreeDir string `json:"worktree_dir"`
	Branch      string `json:"branch"`
	Repos       []struct {
		Name     string `json:"name"`
		Checkout string `json:"checkout"`
		Base     string `json:"base"`
		Created  bool   `json:"created"`
	} `json:"repos"`
}

// recordAllHooks writes a project .wb/config.yaml whose hooks dump their environment, cwd and stdin
// to <root>/<hook>.{env,pwd,json}.
func (w *world) recordAllHooks() {
	record := `printf '%s|%s|%s|%s|%s|%s|%s' "$WB_HOOK" "$WB_ROOT" "$WB_WORKBENCH" "$WB_WORKTREE" "$WB_WORKTREE_DIR" "$WB_BRANCH" "$WB_REPOS" > "$WB_ROOT/$WB_HOOK.env"; pwd > "$WB_ROOT/$WB_HOOK.pwd"; cat > "$WB_ROOT/$WB_HOOK.json"`
	w.t.Helper()
	text := "hooks:\n"
	for _, hook := range []string{"post_worktree_create", "post_worktree_delete", "post_worktree_repo_add", "post_worktree_repo_remove"} {
		text += "  " + hook + ": |\n    " + record + "\n"
	}
	write(w.t, w.path(".wb", "config.yaml"), text)
}

// hookRan returns the recorded env fields, cwd and stdin payload of the last run of hook,
// and deletes the records so the next call starts clean.
func (w *world) hookRan(hook string) (env []string, cwd string, payload hookPayload) {
	w.t.Helper()
	env = strings.Split(read(w.t, w.path(hook+".env")), "|")
	cwd = strings.TrimSpace(read(w.t, w.path(hook+".pwd")))
	if err := json.Unmarshal([]byte(read(w.t, w.path(hook+".json"))), &payload); err != nil {
		w.t.Fatal(err)
	}
	for _, ext := range []string{".env", ".pwd", ".json"} {
		if err := os.Remove(w.path(hook + ext)); err != nil {
			w.t.Fatal(err)
		}
	}
	return env, cwd, payload
}

func TestHooksGetEnvAndStdinPayload(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.recordAllHooks()
	dir := w.path(".worktrees", "feat-hooks")
	wantEnv := func(got []string, hook, repos string) {
		t.Helper()
		want := []string{hook, w.root, "ck8s", "feat/hooks", dir, "me/feat/hooks", repos}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s env = %q, want %q", hook, got, want)
		}
	}

	w.wb("-w", "ck8s", "worktree", "create", "feat/hooks")
	env, cwd, p := w.hookRan("post_worktree_create")
	wantEnv(env, "post_worktree_create", "")
	if cwd != dir || p.Hook != "post_worktree_create" || p.Branch != "me/feat/hooks" || p.WorktreeDir != dir || p.Repos == nil || len(p.Repos) != 0 {
		t.Errorf("post_worktree_create cwd=%q payload=%+v", cwd, p)
	}

	w.wb("worktree", "add", "feat/hooks", "k8s-snap")
	env, cwd, p = w.hookRan("post_worktree_repo_add")
	wantEnv(env, "post_worktree_repo_add", "canonical/k8s-snap")
	if cwd != dir || len(p.Repos) != 1 || p.Repos[0].Name != "canonical/k8s-snap" || !p.Repos[0].Created ||
		p.Repos[0].Checkout != w.checkout("feat-hooks", "k8s-snap") || p.Repos[0].Base != "origin/main" || p.Workbench != "ck8s" || p.Root != w.root {
		t.Errorf("post_worktree_repo_add cwd=%q payload=%+v", cwd, p)
	}

	// an already present repo is listed with created=false but is not in WB_REPOS
	w.wb("worktree", "add", "feat/hooks", "k8s-snap", "a/shared")
	env, _, p = w.hookRan("post_worktree_repo_add")
	wantEnv(env, "post_worktree_repo_add", "a/shared")
	if len(p.Repos) != 2 || p.Repos[0].Created || !p.Repos[1].Created {
		t.Errorf("post_worktree_repo_add payload = %+v", p.Repos)
	}

	// nothing new: no hook
	w.wb("worktree", "add", "feat/hooks", "k8s-snap")
	if exists(w.path("post_worktree_repo_add.env")) {
		t.Error("post_worktree_repo_add ran although no repo was added")
	}

	// removing some repos keeps the worktree: only the repo hook fires
	w.wb("worktree", "remove", "feat/hooks", "a/shared")
	env, cwd, p = w.hookRan("post_worktree_repo_remove")
	wantEnv(env, "post_worktree_repo_remove", "a/shared")
	if cwd != dir || len(p.Repos) != 1 || p.Repos[0].Created || p.Repos[0].Checkout != w.checkout("feat-hooks", "shared") {
		t.Errorf("post_worktree_repo_remove cwd=%q payload=%+v", cwd, p)
	}
	if exists(w.path("post_worktree_delete.env")) {
		t.Error("post_worktree_delete ran although the worktree still has repos")
	}

	// removing the last repo deletes the worktree: repo_remove, then delete, both from the root
	w.wb("worktree", "remove", "feat/hooks")
	_, cwd, p = w.hookRan("post_worktree_repo_remove")
	if cwd != w.root || len(p.Repos) != 1 || p.Repos[0].Name != "canonical/k8s-snap" || p.WorktreeDir != dir {
		t.Errorf("post_worktree_repo_remove cwd=%q payload=%+v", cwd, p)
	}
	env, cwd, p = w.hookRan("post_worktree_delete")
	wantEnv(env, "post_worktree_delete", "")
	if cwd != w.root || p.Hook != "post_worktree_delete" || p.Repos == nil || len(p.Repos) != 0 || p.WorktreeDir != dir {
		t.Errorf("post_worktree_delete cwd=%q payload=%+v", cwd, p)
	}

	// deleting an empty worktree fires only post_worktree_delete
	w.wb("-w", "ck8s", "worktree", "create", "feat/empty")
	w.hookRan("post_worktree_create")
	w.wb("worktree", "remove", "feat/empty")
	if _, _, p = w.hookRan("post_worktree_delete"); len(p.Repos) != 0 || p.Worktree != "feat/empty" {
		t.Errorf("post_worktree_delete payload = %+v", p)
	}
	if exists(w.path("post_worktree_repo_remove.env")) {
		t.Error("post_worktree_repo_remove ran although no repo was removed")
	}
}

func TestAddOnMissingWorktreeFiresCreateThenAdd(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	w.recordAllHooks()
	w.wb("-w", "ck8s", "worktree", "add", "feat/both", "k8s-snap")
	w.hookRan("post_worktree_create")
	if _, _, p := w.hookRan("post_worktree_repo_add"); len(p.Repos) != 1 {
		t.Errorf("post_worktree_repo_add payload = %+v", p)
	}
}

func TestProjectHooksOverrideUserHooksPerKey(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	write(t, w.userConfig(),
		"hooks:\n  post_worktree_create: 'echo user > \"$WB_ROOT/created\"'\n  post_worktree_repo_add: 'echo user > \"$WB_ROOT/added\"'\n  post_worktree_delete: 'echo user > \"$WB_ROOT/deleted\"'\n")

	w.wb("-w", "ck8s", "worktree", "create", "feat/user")
	if got := strings.TrimSpace(read(t, w.path("created"))); got != "user" {
		t.Errorf("user post_worktree_create did not run: %q", got)
	}

	write(t, w.path(".wb", "config.yaml"), "hooks:\n  post_worktree_create: 'echo project > \"$WB_ROOT/created\"'\n  post_worktree_delete: ''\n")
	w.wb("-w", "ck8s", "worktree", "create", "feat/project")
	if got := strings.TrimSpace(read(t, w.path("created"))); got != "project" {
		t.Errorf("project post_worktree_create did not override: %q", got)
	}
	w.wb("worktree", "add", "feat/project", "k8s-snap")
	if got := strings.TrimSpace(read(t, w.path("added"))); got != "user" {
		t.Errorf("user post_worktree_repo_add should still run: %q", got)
	}
	w.wb("worktree", "remove", "feat/project")
	if exists(w.path("deleted")) {
		t.Error("an empty project value should switch the user hook off")
	}
}

func TestFailingHookExitsOneAndKeepsState(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	write(t, w.path(".wb", "config.yaml"), "hooks:\n  post_worktree_repo_add: 'echo boom >&2; exit 3'\n  post_worktree_repo_remove: 'exit 4'\n")

	out := w.wbFails("-w", "ck8s", "worktree", "add", "feat/fail", "k8s-snap")
	contains(t, out, "post_worktree_repo_add hook failed", "boom")
	if !exists(w.checkout("feat-fail", "k8s-snap")) {
		t.Error("checkout was rolled back")
	}
	if m := w.manifest("feat-fail"); len(m.Repos) != 1 {
		t.Errorf("manifest = %+v", m)
	}

	contains(t, w.wbFails("worktree", "remove", "feat/fail"), "post_worktree_repo_remove hook failed")
	if exists(w.checkout("feat-fail", "k8s-snap")) || exists(w.path(".worktrees", "feat-fail")) {
		t.Error("removal was rolled back")
	}
}

func TestInvalidHookConfigIsRejectedBeforeAnyChange(t *testing.T) {
	w := newWorld(t)
	w.twoWorkbenches()
	write(t, w.path(".wb", "config.yaml"), "hooks:\n  post_typo: 'true'\n")
	contains(t, w.wbFails("-w", "ck8s", "worktree", "create", "feat/x"), "unknown hook \"post_typo\"")
	if exists(w.path(".worktrees", "feat-x")) {
		t.Error("worktree was created despite an invalid config")
	}
}

func (w *world) userConfig() string {
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "wb", "config.yaml")
}
