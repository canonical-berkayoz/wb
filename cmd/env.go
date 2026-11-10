package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/canonical/wb/internal/config"
	"github.com/canonical/wb/internal/gitx"
	"github.com/canonical/wb/internal/manifest"
	"github.com/canonical/wb/internal/names"
	"github.com/canonical/wb/internal/pick"
	"github.com/canonical/wb/internal/pool"
	"github.com/canonical/wb/internal/root"
)

// Env is the per-invocation context. It lazily resolves the root, the
// configuration and the active workbench, so commands only pay for what they use.
type Env struct {
	workbenchFlag string
	rootFlag      string

	out, err io.Writer
	picker   *pick.Picker

	rootDir string
	cfg     *config.Config
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (e *Env) workbenchOpt() string { return firstNonEmpty(e.workbenchFlag, os.Getenv("WB_WORKBENCH")) }

func (e *Env) Root() (string, error) {
	if e.rootDir == "" {
		dir, err := root.Find(firstNonEmpty(e.rootFlag, os.Getenv("WB_ROOT")))
		if err != nil {
			return "", err
		}
		e.rootDir = dir
	}
	return e.rootDir, nil
}

func (e *Env) Config() (*config.Config, error) {
	if e.cfg == nil {
		dir, err := e.Root()
		if err != nil {
			return nil, err
		}
		if e.cfg, err = config.Load(dir); err != nil {
			return nil, err
		}
	}
	return e.cfg, nil
}

func (e *Env) namedWorkbench(name string) (*config.Workbench, error) {
	cfg, err := e.Config()
	if err != nil {
		return nil, err
	}
	if workbench, ok := cfg.Workbenches[name]; ok {
		return workbench, nil
	}
	known := strings.Join(cfg.WorkbenchNames(), ", ")
	if known == "" {
		known = "none"
	}
	return nil, fmt.Errorf("unknown workbench %q (known: %s)", name, known)
}

func (e *Env) workbenchFromCwd(cfg *config.Config, rootDir string) (*config.Workbench, error) {
	rel, err := filepath.Rel(gitx.RealPath(rootDir), gitx.RealPath(root.Cwd()))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, nil
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return nil, nil
	}
	if parts[0] == manifest.WorktreesDir {
		group, err := manifest.Load(rootDir, parts[1])
		if err != nil || group == nil {
			return nil, err
		}
		return cfg.Workbenches[group.Workbench], nil
	}
	if candidates := cfg.WorkbenchesFor(parts[0] + "/" + parts[1]); len(candidates) == 1 {
		return candidates[0], nil
	}
	return nil, nil
}

// Workbench returns the active workbench: -w/$WB_WORKBENCH, else the cwd's, else a pick.
func (e *Env) Workbench() (*config.Workbench, error) {
	if opt := e.workbenchOpt(); opt != "" {
		return e.namedWorkbench(opt)
	}
	cfg, err := e.Config()
	if err != nil {
		return nil, err
	}
	found, err := e.workbenchFromCwd(cfg, e.rootDir)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return found, nil
	}
	available := cfg.WorkbenchNames()
	switch len(available) {
	case 0:
		return nil, fmt.Errorf("no workbenches found in %s; create one with `wb new`",
			filepath.Join(e.rootDir, config.WorkbenchesDir))
	case 1:
		return cfg.Workbenches[available[0]], nil
	}
	chosen, err := e.picker.Pick(available, false, "workbench")
	if err != nil {
		return nil, err
	}
	return cfg.Workbenches[chosen[0]], nil
}

func (e *Env) Pool() ([]string, error) {
	cfg, err := e.Config()
	if err != nil {
		return nil, err
	}
	return pool.Repos(e.rootDir, cfg), nil
}

func (e *Env) Resolve(arg string, add bool) (string, error) {
	known, err := e.Pool()
	if err != nil {
		return "", err
	}
	return pool.Resolve(arg, known, add)
}

// SelectRepos returns the REPO arguments, else the whole pool (--all), else the
// active workbench's repos.
func (e *Env) SelectRepos(args []string, everything, add bool) ([]string, error) {
	if len(args) > 0 {
		var selected []string
		seen := map[string]bool{}
		for _, arg := range args {
			full, err := e.Resolve(arg, add)
			if err != nil {
				return nil, err
			}
			if !seen[full] {
				seen[full] = true
				selected = append(selected, full)
			}
		}
		return selected, nil
	}
	if everything {
		return e.Pool()
	}
	workbench, err := e.Workbench()
	if err != nil {
		return nil, err
	}
	return workbench.Repos, nil
}

// PickGroup loads the named worktree, or lets the user pick one.
func (e *Env) PickGroup(name string) (*manifest.Group, error) {
	rootDir, err := e.Root()
	if err != nil {
		return nil, err
	}
	if name != "" {
		slug, err := names.Slugify(name)
		if err != nil {
			return nil, err
		}
		group, err := manifest.Load(rootDir, slug)
		if err != nil {
			return nil, err
		}
		if group == nil {
			return nil, fmt.Errorf("no worktree named %q", name)
		}
		return group, nil
	}
	groups, err := manifest.All(rootDir)
	if err != nil {
		return nil, err
	}
	switch len(groups) {
	case 0:
		return nil, errors.New("there are no worktrees")
	case 1:
		return groups[0], nil
	}
	labels := make([]string, len(groups))
	for i, group := range groups {
		repos := make([]string, len(group.Repos))
		for j, entry := range group.Repos {
			repos[j] = entry.Repo
		}
		labels[i] = fmt.Sprintf("%s  [%s]  %s", group.Slug, group.Workbench, strings.Join(repos, ", "))
	}
	chosen, err := e.picker.Pick(labels, false, "worktree")
	if err != nil {
		return nil, err
	}
	for i, label := range labels {
		if label == chosen[0] {
			return groups[i], nil
		}
	}
	return nil, errors.New("no worktree selected")
}
