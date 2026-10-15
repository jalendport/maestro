package cli

import (
	"fmt"
	"os"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/spf13/cobra"
)

func (a *app) relayCmd() *cobra.Command {
	return &cobra.Command{
		Use: "relay <task>", Hidden: true, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			folder := os.Getenv("MAESTRO_TASK_FOLDER")
			if folder == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				root := config.FindProjectRoot(cwd)
				if root == "" {
					return fmt.Errorf("the home directory cannot be a project root; change to a project folder")
				}
				var ok bool
				folder, ok = dispatch.FindTask(root, args[0])
				if !ok {
					return fmt.Errorf("no task %q under %s", args[0], root)
				}
			}
			control, err := config.ControlDirForFolder(folder)
			if err != nil {
				return err
			}
			return dispatch.Relay(control, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}
