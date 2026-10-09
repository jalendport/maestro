package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/jev"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (a *app) keyCmd() *cobra.Command {
	c := &cobra.Command{Use: "key", Short: "Manage the Jev key"}
	c.AddCommand(&cobra.Command{
		Use:   "set",
		Short: "Store the Jev key in ~/.maestro/env (no echo)",
		Long:  "Read the TypeSafe key from the terminal without echo (or from stdin when piped) and write it to ~/.maestro/env with mode 0600. TYPESAFE_API_KEY in the environment still wins.",
		RunE: func(_ *cobra.Command, _ []string) error {
			var key string
			if term.IsTerminal(int(syscall.Stdin)) {
				fmt.Fprint(os.Stderr, "Jev key: ")
				b, err := term.ReadPassword(int(syscall.Stdin))
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return err
				}
				key = string(b)
			} else {
				line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				key = line
			}
			key = strings.TrimSpace(key)
			if key == "" {
				return fmt.Errorf("no key given")
			}
			if err := jev.SaveKey(key); err != nil {
				return err
			}
			ui.Successf("key written to %s", config.EnvFile())
			return nil
		},
	})
	return c
}
