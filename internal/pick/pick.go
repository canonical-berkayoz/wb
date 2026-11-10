// Package pick selects items with fzf on a terminal, or a numbered prompt otherwise.
package pick

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

// Separator splits preferred items from the rest of a list; it is never selectable.
const Separator = "──────── rest of the pool ────────"

type Picker struct {
	In  *bufio.Reader
	Err io.Writer
	// UseFzf enables fzf when it is installed; callers set it only on a terminal.
	UseFzf bool
}

// IsTerminal reports whether f is an interactive terminal.
func IsTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (p *Picker) readLine() (string, error) {
	line, err := p.In.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// Prompt asks for a non-empty line of text.
func (p *Picker) Prompt(label string) (string, error) {
	fmt.Fprintf(p.Err, "%s: ", label)
	answer, err := p.readLine()
	if err != nil {
		return "", err
	}
	if answer == "" {
		return "", errors.New("nothing entered")
	}
	return answer, nil
}

func parseSelection(answer string, count int) ([]int, error) {
	var picked []int
	seen := map[int]bool{}
	tokens := strings.FieldsFunc(answer, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	for _, token := range tokens {
		lowText, highText, isRange := strings.Cut(token, "-")
		low, errLow := strconv.Atoi(lowText)
		high := low
		var errHigh error
		if isRange {
			high, errHigh = strconv.Atoi(highText)
		}
		if errLow != nil || errHigh != nil {
			return nil, fmt.Errorf("not a number or range: %q", token)
		}
		if low < 1 || low > high || high > count {
			return nil, fmt.Errorf("selection %q is out of range 1-%d", token, count)
		}
		for i := low; i <= high; i++ {
			if !seen[i] {
				seen[i] = true
				picked = append(picked, i)
			}
		}
	}
	return picked, nil
}

func (p *Picker) numbered(items []string, multi bool, prompt string) ([]string, error) {
	var selectable []string
	for _, item := range items {
		if item == Separator {
			fmt.Fprintf(p.Err, "      %s\n", item)
			continue
		}
		selectable = append(selectable, item)
		fmt.Fprintf(p.Err, "  %3d. %s\n", len(selectable), item)
	}
	hint := "number"
	if multi {
		hint = "numbers, Enter = all"
	}
	fmt.Fprintf(p.Err, "%s (%s): ", prompt, hint)
	answer, err := p.readLine()
	if err != nil {
		return nil, err
	}
	if answer == "" {
		if multi {
			return selectable, nil
		}
		return nil, errors.New("nothing selected")
	}
	indexes, err := parseSelection(answer, len(selectable))
	if err != nil {
		return nil, err
	}
	var chosen []string
	for _, index := range indexes {
		chosen = append(chosen, selectable[index-1])
	}
	if !multi {
		chosen = chosen[:1]
	}
	return chosen, nil
}

func (p *Picker) fzf(items []string, multi bool, prompt string) ([]string, error) {
	args := []string{"--prompt", prompt + "> ", "--bind", "ctrl-a:select-all"}
	if multi {
		args = append(args, "--multi")
	}
	cmd := exec.Command("fzf", args...)
	cmd.Stdin = strings.NewReader(strings.Join(items, "\n"))
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("selection cancelled")
	}
	var chosen []string
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" && line != Separator {
			chosen = append(chosen, line)
		}
	}
	return chosen, nil
}

// Pick returns the chosen items (one unless multi). An empty list yields no items.
func (p *Picker) Pick(items []string, multi bool, prompt string) ([]string, error) {
	hasSelectable := false
	for _, item := range items {
		if item != Separator {
			hasSelectable = true
		}
	}
	if !hasSelectable {
		return nil, nil
	}
	var chosen []string
	var err error
	if _, lookErr := exec.LookPath("fzf"); p.UseFzf && lookErr == nil {
		chosen, err = p.fzf(items, multi, prompt)
	} else {
		chosen, err = p.numbered(items, multi, prompt)
	}
	if err != nil {
		return nil, err
	}
	if len(chosen) == 0 {
		return nil, errors.New("nothing selected")
	}
	return chosen, nil
}
