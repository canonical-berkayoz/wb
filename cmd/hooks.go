package cmd

import (
	"fmt"

	"github.com/canonical/wb/internal/hooks"
	"github.com/canonical/wb/internal/manifest"
	"github.com/canonical/wb/internal/worktree"
)

// Hooks returns the hooks merged from the user and project config files.
func (e *Env) Hooks() (hooks.Config, error) {
	if e.hooks == nil {
		dir, err := e.Root()
		if err != nil {
			return nil, err
		}
		if e.hooks, err = hooks.Load(dir); err != nil {
			return nil, err
		}
	}
	return e.hooks, nil
}

// runHooks runs each event's hook, even after one failed, and reports failures on
// stderr. It returns false if any failed. State is never rolled back.
func (e *Env) runHooks(events ...hooks.Event) bool {
	config, err := e.Hooks()
	if err != nil {
		fmt.Fprintf(e.err, "Error: %v\n", err)
		return false
	}
	ok := true
	for _, event := range events {
		if err := config.Run(event, e.out, e.err); err != nil {
			fmt.Fprintf(e.err, "Error: %v\n", err)
			ok = false
		}
	}
	return ok
}

func addedRepos(added []worktree.Added) []hooks.Repo {
	repos := make([]hooks.Repo, len(added))
	for i, item := range added {
		repos[i] = hooks.Repo{Name: item.Entry.Repo, Checkout: item.Entry.Checkout, Base: item.Entry.Base, Created: item.Created}
	}
	return repos
}

func removedRepos(entries []*manifest.RepoEntry) []hooks.Repo {
	repos := make([]hooks.Repo, len(entries))
	for i, entry := range entries {
		repos[i] = hooks.Repo{Name: entry.Repo, Checkout: entry.Checkout, Base: entry.Base}
	}
	return repos
}
