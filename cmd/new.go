package cmd

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/canonical/wb/internal/config"
	"github.com/canonical/wb/internal/names"
)

//go:embed templates/workbench.yaml.tmpl
var workbenchTemplateSource string

var workbenchTemplate = template.Must(template.New("workbench").Parse(workbenchTemplateSource))

var newForce bool

var newCmd = &cobra.Command{
	Use:   "new NAME [owner/repo...]",
	Short: "Create workbenches/NAME.yaml from a commented template",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, repos := args[0], args[1:]
		if !names.ValidWorkbench(name) {
			return fmt.Errorf("%q is not a valid workbench name", name)
		}
		var invalid, listed []string
		seen := map[string]bool{}
		for _, repo := range repos {
			if !names.ValidRepo(repo) {
				invalid = append(invalid, repo)
			} else if !seen[repo] {
				seen[repo] = true
				listed = append(listed, repo)
			}
		}
		if len(invalid) > 0 {
			return fmt.Errorf("not a valid 'owner/repo': %s", strings.Join(invalid, ", "))
		}
		rootDir, err := env.Root()
		if err != nil {
			return err
		}
		target := filepath.Join(rootDir, config.WorkbenchesDir, name+".yaml")
		if _, err := os.Stat(target); err == nil && !newForce {
			return fmt.Errorf("%s already exists; use --force to overwrite", target)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		var body bytes.Buffer
		data := struct {
			Name  string
			Repos []string
		}{name, listed}
		if err := workbenchTemplate.Execute(&body, data); err != nil {
			return err
		}
		if err := os.WriteFile(target, body.Bytes(), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(env.out, "created %s\n", target)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.Flags().BoolVar(&newForce, "force", false, "overwrite an existing file")
}
