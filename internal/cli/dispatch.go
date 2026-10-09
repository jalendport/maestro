package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/spf13/cobra"
)

func (a *app) dispatchCmd() *cobra.Command {
	var lane, floor, ceiling, level string
	var wait bool
	c := &cobra.Command{
		Use:   "dispatch <brief.md>",
		Short: "Route and dispatch one Task",
		Long:  "Route one Brief file to a Lane and start it. Returns at once with the Task id; --wait blocks and prints the result.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			b, err := brief.Parse(args[0])
			if err != nil {
				return err
			}
			req := router.Request{Lane: lane}
			if req.Floor, err = roster.ParseTier(floor); err != nil {
				return err
			}
			if req.Ceiling, err = roster.ParseTier(ceiling); err != nil {
				return err
			}
			if req.Level, err = roster.ParseLevel(level); err != nil {
				return err
			}
			env, err := a.env(false)
			if err != nil {
				return err
			}
			t, err := dispatch.Prepare(env, b, req, "")
			if err != nil {
				var ask *router.AskOrchestrator
				if asAsk(err, &ask) {
					for _, e := range ask.Excluded {
						ui.Eventf("excluded", "%-13s %s", e.Family, e.Rule)
					}
					return &proc.ExitError{Code: proc.CodeOrchestrator, Message: ask.Reason}
				}
				return err
			}
			if wait {
				rec, err := dispatch.Execute(env, t)
				if err != nil {
					return err
				}
				return exit(dispatch.Print(rec, a.json))
			}
			if err := detach(t.Folder); err != nil {
				return err
			}
			if a.json {
				fmt.Fprintf(os.Stdout, "{\"run\":%q,\"task\":%q,\"folder\":%q}\n", t.Run, t.Task, t.Folder)
			} else {
				ui.Outf("task %s started.  Collect with: maestro wait %s", t.Task, t.Task)
			}
			return nil
		},
	}
	c.Flags().StringVar(&lane, "lane", "", "request a Lane: family[:effort]")
	c.Flags().StringVar(&floor, "floor", "", "at least this Tier")
	c.Flags().StringVar(&ceiling, "ceiling", "", "at most this Tier")
	c.Flags().StringVar(&level, "level", "", "Judgment level estimate")
	c.Flags().BoolVar(&wait, "wait", false, "block until the Task ends and print its result")
	return c
}

// detach starts "maestro _supervise <folder>" as its own session so the
// Dispatch outlives this command. Its stderr goes to supervisor.log.
func detach(folder string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := os.Create(filepath.Join(folder, "supervisor.log"))
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(self, "_supervise", folder)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (a *app) superviseCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_supervise <folder>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			t, err := dispatch.LoadTask(args[0])
			if err != nil {
				return err
			}
			if err := os.Chdir(t.Project); err != nil {
				return err
			}
			env, err := a.env(false)
			if err != nil {
				return err
			}
			rec, err := dispatch.Execute(env, t)
			if err != nil {
				return err
			}
			return exit(dispatch.Print(rec, false))
		},
	}
}

func (a *app) waitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wait <task>",
		Short: "Block until a Task ends; print its report",
		Long:  "Wait for a Task (4-hex id) in the current project to end, then print its result. --json prints dispatch.json.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			env, err := a.env(false)
			if err != nil {
				return err
			}
			_ = cwd
			folder, ok := dispatch.FindTask(env.Root, args[0])
			if !ok {
				return fmt.Errorf("no task %q under %s", args[0], env.Root)
			}
			for {
				rec, err := dispatch.LoadRecord(folder)
				if err == nil && rec.State == "ended" {
					return exit(dispatch.Print(rec, a.json))
				}
				if err == nil && rec.Pid > 0 && syscall.Kill(rec.Pid, 0) != nil && time.Since(rec.StartedAt) > 5*time.Second {
					// The supervisor died without finishing: a Stall on the next start, per failure-handling.md section 4.
					return &proc.ExitError{Code: proc.CodeFailed, Message: fmt.Sprintf("task %s: supervisor pid %d is gone; see %s/supervisor.log", args[0], rec.Pid, folder)}
				}
				time.Sleep(2 * time.Second)
			}
		},
	}
}

func exit(code int) error {
	if code == 0 {
		return nil
	}
	return &proc.ExitError{Code: code}
}

func asAsk(err error, target **router.AskOrchestrator) bool {
	if a, ok := err.(*router.AskOrchestrator); ok {
		*target = a
		return true
	}
	return false
}
