package pool_test

import (
	"strings"
	"testing"

	"github.com/canonical/wb/internal/pool"
)

func TestResolve(t *testing.T) {
	known := []string{"a/x", "b/x", "a/y"}
	if got, err := pool.Resolve("y", known, false); err != nil || got != "a/y" {
		t.Errorf("Resolve(y) = %q, %v", got, err)
	}
	if got, err := pool.Resolve("b/x", known, false); err != nil || got != "b/x" {
		t.Errorf("Resolve(b/x) = %q, %v", got, err)
	}
	if _, err := pool.Resolve("x", known, false); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected an ambiguity error, got %v", err)
	}
	if _, err := pool.Resolve("c/z", known, false); err == nil || !strings.Contains(err.Error(), "--add") {
		t.Errorf("expected a --add hint, got %v", err)
	}
	if got, err := pool.Resolve("c/z", known, true); err != nil || got != "c/z" {
		t.Errorf("Resolve(c/z, add) = %q, %v", got, err)
	}
}

func TestCloneURLDefaultsToSSH(t *testing.T) {
	t.Setenv("WB_CLONE_URL", "")
	if got := pool.CloneURL("canonical/k8s-snap"); got != "git@github.com:canonical/k8s-snap.git" {
		t.Errorf("CloneURL = %q", got)
	}
	t.Setenv("WB_CLONE_URL", "file:///r/{owner}/{repo}.git")
	if got := pool.CloneURL("a/b"); got != "file:///r/a/b.git" {
		t.Errorf("CloneURL = %q", got)
	}
}
