// Package cmd holds the cobra command tree, laid out as cobra-cli generates it:
// rootCmd here, and one file per command that registers itself in init().
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"os"
	"runtime/debug"

	"github.com/canonical/wb/internal/pick"
)

// errSilent makes a command exit with status 1 after it already reported why.
var errSilent = errors.New("")

// env is the per-invocation context shared by all commands.
var env = &Env{}

// Set at release time by GoReleaser (see .goreleaser.yaml).
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// buildVersion prefers the release ldflags, then the module version that
// `go install …@vX.Y.Z` records, then "dev".
func buildVersion() string {
	if version != "dev" {
		out := version
		if commit != "" {
			out += " (" + commit
			if date != "" {
				out += ", " + date
			}
			out += ")"
		}
		return out
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

var rootCmd = &cobra.Command{
	Use:           "wb",
	Short:         "Manage a pool of git repositories and multi-repo worktrees",
	Version:       buildVersion(),
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		env.reset(cmd)
	},
}

// Execute runs wb and exits non-zero on failure. It is called by main.main().
func Execute() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	rootCmd.SetArgs(args)
	rootCmd.SetIn(in)
	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	if err := rootCmd.Execute(); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintf(errOut, "Error: %v\n", err)
		}
		return 1
	}
	return 0
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&env.workbenchFlag, "workbench", "w", "", "workbench to use (env WB_WORKBENCH)")
	rootCmd.PersistentFlags().StringVarP(&env.rootFlag, "root", "R", "", "folder containing workbenches/ (env WB_ROOT)")
}

// reset prepares env for a new invocation: fresh streams, picker and caches.
func (e *Env) reset(cmd *cobra.Command) {
	e.out, e.err = cmd.OutOrStdout(), cmd.ErrOrStderr()
	e.picker = &pick.Picker{
		In:     bufio.NewReader(cmd.InOrStdin()),
		Err:    e.err,
		UseFzf: pick.IsTerminal(os.Stdin) && pick.IsTerminal(os.Stderr),
	}
	e.rootDir, e.cfg = "", nil
}
