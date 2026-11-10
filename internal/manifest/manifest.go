// Package manifest persists a worktree group as <root>/.worktrees/<slug>/.wb-worktree.json.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/canonical/wb/internal/names"
)

const (
	WorktreesDir = ".worktrees"
	ManifestName = ".wb-worktree.json"
)

type RepoEntry struct {
	Repo     string
	Checkout string
	Source   string
	Base     string
}

// Group is a named set of same-branch worktrees across repos, tracked by one manifest.
type Group struct {
	Root      string
	Slug      string
	Branch    string
	Workbench string
	Created   string
	Repos     []*RepoEntry
}

func NewGroup(root, slug, branch, workbench string) *Group {
	return &Group{
		Root: root, Slug: slug, Branch: branch, Workbench: workbench,
		Created: time.Now().UTC().Format("2006-01-02T15:04:05+00:00"),
	}
}

func (g *Group) Directory() string    { return filepath.Join(g.Root, WorktreesDir, g.Slug) }
func (g *Group) ManifestPath() string { return filepath.Join(g.Directory(), ManifestName) }

func (g *Group) Entry(repo string) *RepoEntry {
	for _, entry := range g.Repos {
		if entry.Repo == repo {
			return entry
		}
	}
	return nil
}

// Find matches owner/repo, a unique repo name, or a checkout folder name.
func (g *Group) Find(arg string) (*RepoEntry, error) {
	var matches []*RepoEntry
	for _, entry := range g.Repos {
		_, name := names.Split(entry.Repo)
		if arg == entry.Repo || arg == name || arg == filepath.Base(entry.Checkout) {
			matches = append(matches, entry)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, fmt.Errorf("%q is not part of worktree %s", arg, g.Slug)
	}
	repos := make([]string, len(matches))
	for i, entry := range matches {
		repos[i] = entry.Repo
	}
	return nil, fmt.Errorf("%q is ambiguous in %s (%s)", arg, g.Slug, strings.Join(repos, ", "))
}

type repoJSON struct {
	Repo     string `json:"repo"`
	Checkout string `json:"checkout"`
	Source   string `json:"source"`
	Base     string `json:"base"`
}

type groupJSON struct {
	Slug      string     `json:"slug"`
	Branch    string     `json:"branch"`
	Workbench string     `json:"workbench"`
	Created   string     `json:"created"`
	Repos     []repoJSON `json:"repos"`
}

func relative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}

func Save(g *Group) error {
	if err := os.MkdirAll(g.Directory(), 0o755); err != nil {
		return err
	}
	data := groupJSON{Slug: g.Slug, Branch: g.Branch, Workbench: g.Workbench, Created: g.Created, Repos: []repoJSON{}}
	for _, entry := range g.Repos {
		data.Repos = append(data.Repos, repoJSON{
			Repo:     entry.Repo,
			Checkout: relative(g.Root, entry.Checkout),
			Source:   relative(g.Root, entry.Source),
			Base:     entry.Base,
		})
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(g.ManifestPath(), append(encoded, '\n'), 0o644)
}

func absolute(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

// Load returns the group with this slug, or nil if it has no manifest.
func Load(root, slug string) (*Group, error) {
	raw, err := os.ReadFile(filepath.Join(root, WorktreesDir, slug, ManifestName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var data groupJSON
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", slug, ManifestName, err)
	}
	group := &Group{Root: root, Slug: data.Slug, Branch: data.Branch, Workbench: data.Workbench, Created: data.Created}
	for _, r := range data.Repos {
		group.Repos = append(group.Repos, &RepoEntry{
			Repo: r.Repo, Checkout: absolute(root, r.Checkout), Source: absolute(root, r.Source), Base: r.Base,
		})
	}
	return group, nil
}

func All(root string) ([]*Group, error) {
	dirs, err := os.ReadDir(filepath.Join(root, WorktreesDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })
	var groups []*Group
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		group, err := Load(root, dir.Name())
		if err != nil {
			return nil, err
		}
		if group != nil {
			groups = append(groups, group)
		}
	}
	return groups, nil
}

// Delete removes the manifest and the group folder; it returns false if the
// folder still has other content.
func Delete(g *Group) bool {
	_ = os.Remove(g.ManifestPath())
	return os.Remove(g.Directory()) == nil
}
