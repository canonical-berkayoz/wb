package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// git runs git in dir and returns its trimmed output, failing the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// world is a throwaway root with local bare repos standing in for GitHub.
type world struct {
	t       *testing.T
	root    string
	remotes string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitconfig := filepath.Join(tmp, "gitconfig")
	write(t, gitconfig, "[init]\n\tdefaultBranch = main\n[commit]\n\tgpgsign = false\n")
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, who := range []string{"AUTHOR", "COMMITTER"} {
		t.Setenv("GIT_"+who+"_NAME", "Test")
		t.Setenv("GIT_"+who+"_EMAIL", "test@example.com")
	}
	t.Setenv("WB_ROOT", "")
	t.Setenv("WB_WORKBENCH", "")

	w := &world{t: t, root: filepath.Join(tmp, "root"), remotes: filepath.Join(tmp, "remotes")}
	if err := os.MkdirAll(w.root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WB_CLONE_URL", "file://"+w.remotes+"/{owner}/{repo}.git")
	return w
}

// remote creates a bare remote owner/repo with one commit on main.
func (w *world) remote(full string) {
	w.t.Helper()
	bare := filepath.Join(w.remotes, full+".git")
	work := filepath.Join(w.remotes, "_work", full)
	if err := os.MkdirAll(work, 0o755); err != nil {
		w.t.Fatal(err)
	}
	git(w.t, work, "init")
	write(w.t, filepath.Join(work, "README.md"), full)
	git(w.t, work, "add", ".")
	git(w.t, work, "commit", "-m", "init")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		w.t.Fatal(err)
	}
	git(w.t, w.remotes, "clone", "--bare", work, bare)
}

func (w *world) workbench(name, text string) {
	w.t.Helper()
	write(w.t, filepath.Join(w.root, "workbenches", name+".yaml"), text)
}

func (w *world) checkout(slug, name string) string {
	return filepath.Join(w.root, ".worktrees", slug, name)
}

func (w *world) path(parts ...string) string {
	return filepath.Join(append([]string{w.root}, parts...)...)
}

// run invokes wb with an explicit -R root and returns the combined output and exit code.
func (w *world) run(input string, args ...string) (string, int) {
	return w.runRaw(input, append([]string{"-R", w.root}, args...)...)
}

// runRaw invokes wb exactly as given, so the root is discovered from the cwd.
func (w *world) runRaw(input string, args ...string) (string, int) {
	var out bytes.Buffer
	resetFlags(rootCmd)
	code := run(args, strings.NewReader(input), &out, &out)
	return out.String(), code
}

// resetFlags restores every flag to its default, because rootCmd is a process-wide
// global that keeps flag values between invocations.
func resetFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

func (w *world) wb(args ...string) string {
	w.t.Helper()
	out, code := w.run("", args...)
	if code != 0 {
		w.t.Fatalf("wb %s: exit %d\n%s", strings.Join(args, " "), code, out)
	}
	return out
}

func (w *world) wbFails(args ...string) string {
	w.t.Helper()
	out, code := w.run("", args...)
	if code != 1 {
		w.t.Fatalf("wb %s: expected exit 1, got %d\n%s", strings.Join(args, " "), code, out)
	}
	return out
}

func (w *world) twoWorkbenches() {
	for _, repo := range []string{"canonical/k8s-snap", "canonical/cilium-rocks", "a/shared", "b/shared"} {
		w.remote(repo)
	}
	w.workbench("ck8s", "description: CK8s\nbranch_prefix: 'me/'\nrepositories:\n  - canonical/k8s-snap\n  - a/shared\n")
	w.workbench("rocks", "repositories:\n  - canonical/cilium-rocks\n  - canonical/k8s-snap\n  - b/shared\n")
}

func contains(t *testing.T, haystack string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			t.Errorf("output does not contain %q:\n%s", needle, haystack)
		}
	}
}

func lineWith(t *testing.T, text, needle string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line with %q in:\n%s", needle, text)
	return ""
}
