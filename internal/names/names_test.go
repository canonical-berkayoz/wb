package names_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/canonical/wb/internal/names"
)

func TestSlugify(t *testing.T) {
	for input, want := range map[string]string{"Bump-Cilium!": "bump-cilium", "a..b": "a.b"} {
		got, err := names.Slugify(input)
		if err != nil || got != want {
			t.Errorf("Slugify(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := names.Slugify("///"); err == nil {
		t.Error("expected an error for a name without usable characters")
	}
}

func TestBranchNameKeepsSlashes(t *testing.T) {
	for input, want := range map[string]string{
		"feat/Add-Rate-Limiting": "feat/add-rate-limiting",
		"fix//dns-timeout/":      "fix/dns-timeout",
		"plain":                  "plain",
	} {
		got, err := names.BranchName(input)
		if err != nil || got != want {
			t.Errorf("BranchName(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := names.BranchName("//"); err == nil {
		t.Error("expected an error for a name without usable characters")
	}
}

func TestSpacesInNamesAreRejected(t *testing.T) {
	for _, bad := range []string{"add rate limiting", "feat/add rate", " lead", "tab\tbed"} {
		if _, err := names.Slugify(bad); err == nil {
			t.Errorf("Slugify(%q) should fail", bad)
		}
		if _, err := names.BranchName(bad); err == nil || !strings.Contains(err.Error(), "must not contain spaces") {
			t.Errorf("BranchName(%q) = %v; want a spaces error", bad, err)
		}
	}
}

func TestCheckoutNamesDisambiguateSharedNames(t *testing.T) {
	got := names.CheckoutNames([]string{"a/x", "b/x", "a/y"})
	want := map[string]string{"a/x": "a-x", "b/x": "b-x", "a/y": "y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
