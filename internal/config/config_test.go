package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canonical/wb/internal/config"
)

func writeWorkbench(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, "workbenches")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	root := t.TempDir()
	writeWorkbench(t, root, "one", "repositories:\n  - repo: a/x\n    setup: make one\n  - not-a-repo\nbogus: 1\n")
	writeWorkbench(t, root, "two", "repositories:\n  - repo: a/x\n    setup: make two\n")
	_, err := config.Load(root)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{`unknown key "bogus"`, `"not-a-repo" is not a valid`, "one.yaml", "two.yaml", "'setup'"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

func TestLoadAcceptsSameSettingInTwoWorkbenches(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"one", "two"} {
		writeWorkbench(t, root, name, "repositories:\n  - repo: a/x\n    default_branch: dev\n")
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.SettingsFor("a/x").DefaultBranch; got != "dev" {
		t.Errorf("default_branch = %q", got)
	}
	var found []string
	for _, wb := range cfg.WorkbenchesFor("a/x") {
		found = append(found, wb.Name)
	}
	if strings.Join(found, ",") != "one,two" {
		t.Errorf("workbenches = %v", found)
	}
}

func TestLoadWithoutWorkbenchesDirIsEmpty(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	if err != nil || len(cfg.Workbenches) != 0 {
		t.Errorf("got %v, %v", cfg, err)
	}
}
