package cli

import (
	"fmt"
	"os"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/spf13/cobra"
)

func (a *app) watchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch <task|folder>",
		Short: "Follow a Dispatch's events, model text, and tool names",
		Long:  "Print plain lines from one Dispatch until it ends. Accepts a Task id or a Run folder; works in any terminal.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			folder := args[0]
			if info, err := os.Stat(folder); err != nil || !info.IsDir() {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				var ok bool
				folder, ok = dispatch.FindTask(config.FindProjectRoot(cwd), args[0])
				if !ok {
					return fmt.Errorf("no Task or Run folder %q", args[0])
				}
			}
			return dispatch.Watch(cmd.Context(), folder, cmd.OutOrStdout())
		},
	}
}
