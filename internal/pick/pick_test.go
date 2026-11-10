package pick

import (
	"bufio"
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestParseSelection(t *testing.T) {
	got, err := parseSelection("1, 3-4 3", 5)
	if err != nil || !reflect.DeepEqual(got, []int{1, 3, 4}) {
		t.Errorf("got %v, %v", got, err)
	}
	for _, bad := range []string{"0", "6", "x", "4-2"} {
		if _, err := parseSelection(bad, 5); err == nil {
			t.Errorf("parseSelection(%q) should fail", bad)
		}
	}
}

func TestNumberedSkipsSeparatorAndDefaultsToAllWhenMulti(t *testing.T) {
	var shown bytes.Buffer
	p := &Picker{In: bufio.NewReader(strings.NewReader("\n")), Err: &shown}
	got, err := p.Pick([]string{"a", Separator, "b"}, true, "repos")
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("got %v, %v", got, err)
	}
	p = &Picker{In: bufio.NewReader(strings.NewReader("2\n")), Err: &shown}
	got, err = p.Pick([]string{"a", Separator, "b"}, false, "one")
	if err != nil || !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("got %v, %v", got, err)
	}
}
