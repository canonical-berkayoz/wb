// Package config loads and validates the one-file-per-workbench configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/canonical/wb/internal/names"
)

const WorkbenchesDir = "workbenches"

var (
	workbenchKeys = map[string]bool{"description": true, "branch_prefix": true, "repositories": true}
	repoKeys      = map[string]bool{"repo": true, "default_branch": true, "setup": true}
	settingKeys   = []string{"default_branch", "setup"}
)

type RepoSettings struct {
	DefaultBranch string
	Setup         string
}

type Workbench struct {
	Name         string
	Path         string
	Description  string
	BranchPrefix string
	Repos        []string
}

func (w *Workbench) Has(repo string) bool {
	for _, r := range w.Repos {
		if r == repo {
			return true
		}
	}
	return false
}

type Config struct {
	Root        string
	Workbenches map[string]*Workbench
	Settings    map[string]RepoSettings
}

// WorkbenchNames returns the workbench names, sorted.
func (c *Config) WorkbenchNames() []string {
	names := make([]string, 0, len(c.Workbenches))
	for name := range c.Workbenches {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *Config) KnownRepos() []string {
	set := map[string]bool{}
	for _, wb := range c.Workbenches {
		for _, repo := range wb.Repos {
			set[repo] = true
		}
	}
	return sortedKeys(set)
}

func (c *Config) WorkbenchesFor(repo string) []*Workbench {
	var found []*Workbench
	for _, name := range c.WorkbenchNames() {
		if wb := c.Workbenches[name]; wb.Has(repo) {
			found = append(found, wb)
		}
	}
	return found
}

func (c *Config) SettingsFor(repo string) RepoSettings { return c.Settings[repo] }

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type seenValue struct{ value, label string }

func unknownKeys(data map[string]any, allowed map[string]bool) []string {
	var unknown []string
	for key := range data {
		if !allowed[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func stringField(value any, where string, errs *[]string) (string, bool) {
	if value == nil {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		*errs = append(*errs, where+" must be a string")
		return "", false
	}
	return text, true
}

func loadWorkbench(path string, errs *[]string, merged map[string]map[string]seenValue) *Workbench {
	label := filepath.Base(path)
	fail := func(format string, args ...any) { *errs = append(*errs, label+": "+fmt.Sprintf(format, args...)) }

	raw, err := os.ReadFile(path)
	if err != nil {
		fail("%v", err)
		return nil
	}
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		fail("invalid YAML: %v", err)
		return nil
	}
	if data == nil {
		fail("must be a mapping with a 'repositories' list")
		return nil
	}
	for _, key := range unknownKeys(data, workbenchKeys) {
		fail("unknown key %q", key)
	}
	rawRepos, present := data["repositories"]
	if !present {
		fail("missing required key 'repositories'")
		return nil
	}
	var entries []any
	if rawRepos != nil {
		list, ok := rawRepos.([]any)
		if !ok {
			fail("'repositories' must be a list")
			return nil
		}
		entries = list
	}

	var repos []string
	seen := map[string]bool{}
	for i, entry := range entries {
		index := i + 1
		var full string
		settings := map[string]any{}
		switch item := entry.(type) {
		case string:
			full = item
		case map[string]any:
			for _, key := range unknownKeys(item, repoKeys) {
				fail("repositories[%d]: unknown key %q", index, key)
			}
			repo, ok := item["repo"].(string)
			if !ok {
				fail("repositories[%d]: missing 'repo'", index)
				continue
			}
			full = repo
			for _, key := range settingKeys {
				if value, ok := item[key]; ok {
					settings[key] = value
				}
			}
		default:
			fail("repositories[%d] must be 'owner/repo' or a mapping", index)
			continue
		}
		if !names.ValidRepo(full) {
			fail("%q is not a valid 'owner/repo'", full)
			continue
		}
		if !seen[full] {
			seen[full] = true
			repos = append(repos, full)
		}
		for _, key := range settingKeys {
			value, ok := stringField(settings[key], fmt.Sprintf("%s: %s: '%s'", label, full, key), errs)
			if !ok {
				continue
			}
			if merged[full] == nil {
				merged[full] = map[string]seenValue{}
			}
			if prior, exists := merged[full][key]; exists && prior.value != value {
				*errs = append(*errs, fmt.Sprintf("%s: '%s' is %q in %s but %q in %s",
					full, key, prior.value, prior.label, value, label))
			} else if !exists {
				merged[full][key] = seenValue{value, label}
			}
		}
	}

	description, _ := stringField(data["description"], label+": 'description'", errs)
	prefix, _ := stringField(data["branch_prefix"], label+": 'branch_prefix'", errs)
	return &Workbench{
		Name:         strings.TrimSuffix(label, ".yaml"),
		Path:         path,
		Description:  description,
		BranchPrefix: prefix,
		Repos:        repos,
	}
}

// Load reads every workbench file under root, reporting all problems in one error.
func Load(root string) (*Config, error) {
	config := &Config{Root: root, Workbenches: map[string]*Workbench{}, Settings: map[string]RepoSettings{}}
	var errs []string
	merged := map[string]map[string]seenValue{}

	files, _ := filepath.Glob(filepath.Join(root, WorkbenchesDir, "*.yaml"))
	sort.Strings(files)
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".yaml")
		if !names.ValidWorkbench(name) {
			errs = append(errs, fmt.Sprintf("%s: invalid workbench name %q", filepath.Base(path), name))
			continue
		}
		if workbench := loadWorkbench(path, &errs, merged); workbench != nil {
			config.Workbenches[workbench.Name] = workbench
		}
	}
	for repo, values := range merged {
		config.Settings[repo] = RepoSettings{
			DefaultBranch: values["default_branch"].value,
			Setup:         values["setup"].value,
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid workbench configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return config, nil
}
