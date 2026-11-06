package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jalendport/maestro/internal/config"
	"github.com/spf13/cobra"
)

const projectTemplate = `# Maestro project file. Its presence puts this project in hook scope.
# forbid = []   # Lists the Families that Maestro never routes to in this project.
# checks = []   # Lists the commands that Maestro runs after every edit Dispatch, and that the Lane may run.
# allow = []   # Lists extra permission rules that Maestro gives to every edit Dispatch in this project.
# router = ""   # When it is rules, Maestro never calls Jev for this project, so no text of the project leaves the machine.
# redact = []   # Lists strings that Maestro replaces before it sends text to Jev.
# councils = ""   # When it is always, every Run in this project ends with a Council, except a Run in which every Task is read-only and no file changed; that Run ends without a Council.
# claude_auto = false   # Lets Maestro use the Claude Code auto permission mode for Dispatches in this project.
# worktrees = false   # Turns Worktrees off for this project.
# commit_message = "{title}\n\nCo-Authored-By: {lane}"   # Replaces the commit message template for this project.
# dirs = []   # Lists extra writable directories for every edit Dispatch in this project, for example a toolchain cache; a leading ~ is the home directory, and a Brief's dirs: adds to the list. The key exists because a Harness sandbox can deny a build cache outside the project, which fails every check until the cache folder is writable.
`

func (a *app) initCmd() *cobra.Command {
	var write bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Write the project file .agents/maestro.toml, which puts the project in hook scope",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root := config.FindProjectRoot(cwd)
			if root == "" {
				return fmt.Errorf("maestro init needs a project directory")
			}
			path := config.ProjectFile(root)
			info, err := os.Stat(path)
			if os.IsNotExist(err) {
				info, err = os.Lstat(path)
			}
			if err == nil && !info.Mode().IsRegular() {
				return fmt.Errorf("%s exists and is not a regular file", path)
			}
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			existing := err == nil
			source := projectTemplate
			if existing {
				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				source = string(content)
			}
			defaults := detectProjectStack(root)
			if !existing {
				// The empty checks example must not hide the stack's suggestions.
				if len(defaults.checks) > 0 {
					source = strings.Replace(source, "# checks = []", "# checks = "+projectArray(defaults.checks), 1)
				}
			}
			merged, err := mergeProjectDefaults(source, defaults)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if existing {
				state := "unchanged"
				if write && merged != source {
					if err := os.WriteFile(path, []byte(merged), info.Mode().Perm()); err != nil {
						return err
					}
					state = "updated"
				}
				if !write && merged != source {
					return a.result(map[string]string{"path": path, "state": state, "proposed": merged}, "%s %s\nProposed Project file (use --write to merge):\n%s", state, path, merged)
				}
				return a.result(map[string]string{"path": path, "state": state}, "%s %s", state, path)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			state := "unchanged"
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if !os.IsExist(err) {
				if err != nil {
					return err
				}
				_, err = f.WriteString(merged)
				closeErr := f.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
				state = "created"
			}
			return a.result(map[string]string{"path": path, "state": state}, "%s %s", state, path)
		},
	}
	c.Flags().BoolVar(&write, "write", false, "Merge detected stack values into an existing Project file")
	return c
}
