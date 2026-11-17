// Package hooks loads the lifecycle hooks from the user and project config files and runs them.
package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/canonical/wb/internal/manifest"
)

const (
	PostWorktreeCreate     = "post_worktree_create"
	PostWorktreeDelete     = "post_worktree_delete"
	PostWorktreeRepoAdd    = "post_worktree_repo_add"
	PostWorktreeRepoRemove = "post_worktree_repo_remove"

	ProjectFile = ".wb/config.yaml"
)

var known = map[string]bool{
	PostWorktreeCreate: true, PostWorktreeDelete: true, PostWorktreeRepoAdd: true, PostWorktreeRepoRemove: true,
}

// Config maps a hook name to its shell command.
type Config map[string]string

// UserConfigPath is $XDG_CONFIG_HOME/wb/config.yaml, or ~/.config/wb/config.yaml.
func UserConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "wb", "config.yaml")
}

// Load merges the user and project config files; the project value wins per hook.
// Either file may be missing. An empty project value switches a user hook off.
func Load(root string) (Config, error) {
	merged := Config{}
	for _, path := range []string{UserConfigPath(), filepath.Join(root, ProjectFile)} {
		if path == "" {
			continue
		}
		file, err := loadFile(path)
		if err != nil {
			return nil, err
		}
		for hook, command := range file {
			merged[hook] = command
		}
	}
	return merged, nil
}

func loadFile(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("%s: invalid YAML: %v", path, err)
	}
	var problems []string
	for key := range data {
		if key != "hooks" {
			problems = append(problems, fmt.Sprintf("unknown key %q", key))
		}
	}
	section, ok := data["hooks"].(map[string]any)
	if data["hooks"] != nil && !ok {
		problems = append(problems, "'hooks' must be a mapping")
	}
	file := Config{}
	for hook, value := range section {
		command, isString := value.(string)
		switch {
		case !known[hook]:
			problems = append(problems, fmt.Sprintf("unknown hook %q", hook))
		case value != nil && !isString:
			problems = append(problems, fmt.Sprintf("hook %q must be a shell command string", hook))
		default:
			file[hook] = command
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("%s: %s", path, strings.Join(problems, "; "))
	}
	return file, nil
}

// Repo is one repo a hook call is about. For post_worktree_repo_remove, Checkout is the removed path.
type Repo struct {
	Name     string `json:"name"`
	Checkout string `json:"checkout"`
	Base     string `json:"base"`
	Created  bool   `json:"created"`
}

// Event describes one hook call.
type Event struct {
	Hook  string
	Group *manifest.Group
	Repos []Repo
}

type payload struct {
	Hook        string `json:"hook"`
	Root        string `json:"root"`
	Workbench   string `json:"workbench"`
	Worktree    string `json:"worktree"`
	WorktreeDir string `json:"worktree_dir"`
	Branch      string `json:"branch"`
	Repos       []Repo `json:"repos"`
}

// affected lists the repos for WB_REPOS: for post_worktree_repo_add only those newly checked out.
func (e Event) affected() []string {
	var repos []string
	for _, repo := range e.Repos {
		if e.Hook != PostWorktreeRepoAdd || repo.Created {
			repos = append(repos, repo.Name)
		}
	}
	return repos
}

func (e Event) payload() payload {
	root, err := filepath.Abs(e.Group.Root)
	if err != nil {
		root = e.Group.Root
	}
	repos := e.Repos
	if repos == nil {
		repos = []Repo{}
	}
	return payload{
		Hook: e.Hook, Root: root, Workbench: e.Group.Workbench, Worktree: e.Group.Name,
		WorktreeDir: filepath.Join(root, manifest.WorktreesDir, e.Group.Slug), Branch: e.Group.Branch, Repos: repos,
	}
}

// Run executes the configured command for the event with `sh -c`, and does nothing
// when the hook is not configured. It runs in the worktree folder, or in the root
// once the folder is gone (the last repos were removed, or post_worktree_delete).
func (c Config) Run(e Event, stdout, stderr io.Writer) error {
	command := c[e.Hook]
	if command == "" {
		return nil
	}
	data := e.payload()
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = data.WorktreeDir
	if info, err := os.Stat(cmd.Dir); err != nil || !info.IsDir() {
		cmd.Dir = data.Root
	}
	cmd.Env = append(os.Environ(),
		"WB_HOOK="+data.Hook,
		"WB_ROOT="+data.Root,
		"WB_WORKBENCH="+data.Workbench,
		"WB_WORKTREE="+data.Worktree,
		"WB_WORKTREE_DIR="+data.WorktreeDir,
		"WB_BRANCH="+data.Branch,
		"WB_REPOS="+strings.Join(e.affected(), " "),
	)
	cmd.Stdin = bytes.NewReader(append(encoded, '\n'))
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s hook failed: %v", e.Hook, err)
	}
	return nil
}
