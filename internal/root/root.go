// Package root finds the folder that holds workbenches/.
package root

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/canonical/wb/internal/config"
	"github.com/canonical/wb/internal/gitx"
)

const fallbackRoot = "/project"

// Cwd returns the working directory, or $HOME if it was deleted (for example
// a worktree that `wb worktree remove` just removed).
func Cwd() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	home, _ := os.UserHomeDir()
	return home
}

func hasWorkbenches(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, config.WorkbenchesDir))
	return err == nil && info.IsDir()
}

func walkUp(start string) string {
	for dir := start; ; dir = filepath.Dir(dir) {
		if hasWorkbenches(dir) {
			return dir
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// Find returns the folder containing workbenches/, found from wherever wb runs.
func Find(explicit string) (string, error) {
	if explicit != "" {
		return gitx.RealPath(expandHome(explicit)), nil
	}
	cwd := Cwd()
	if found := walkUp(cwd); found != "" {
		return found, nil
	}
	if res, err := gitx.Attempt(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && res.OK() {
		if common := strings.TrimSpace(res.Stdout); common != "" {
			if found := walkUp(filepath.Dir(common)); found != "" {
				return found, nil
			}
		}
	}
	if hasWorkbenches(fallbackRoot) {
		return fallbackRoot, nil
	}
	return "", errors.New("could not find a root (a folder containing workbenches/); use -R/--root or $WB_ROOT")
}
