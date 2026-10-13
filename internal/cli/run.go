package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/run"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/spf13/cobra"
)

func (a *app) runCmd() *cobra.Command {
	var lane, level string
	var wait bool
	c := &cobra.Command{
		Use:     "run <plan.md>",
		Short:   "Run every Task of a Plan file (one live Run per project)",
		Long:    "Parse a Plan file, print the provisional Routes, hold for approval when a trigger fires, else start the per-Run supervisor. --wait keeps the supervisor in this process.",
		Args:    cobra.ExactArgs(1),
		PreRunE: refreshSkill,
		RunE: func(_ *cobra.Command, args []string) error {
			env, err := a.env(false)
			if err != nil {
				return err
			}
			lv, err := roster.ParseLevel(level)
			if err != nil {
				return err
			}
			s, err := run.Create(env, args[0], run.CreateOpts{Lane: lane, Level: lv})
			if err != nil {
				return err
			}
			printPlan(s)
			if s.State == run.StateHeld {
				for _, ap := range s.Approval {
					ui.Outf("approval needed: %s", ap)
				}
				ui.Outf("run %s held.  Approve with: maestro approve %s   (edit the Plan file and run again to change it)", s.Run, s.Run)
				return &proc.ExitError{Code: proc.CodeOrchestrator}
			}
			return a.startRun(env, s, wait)
		},
	}
	c.Flags().StringVar(&lane, "lane", "", "request a Lane for every Task: family[:effort]")
	c.Flags().StringVar(&level, "level", "", "Judgment level estimate for every Task")
	c.Flags().BoolVar(&wait, "wait", false, "keep the supervisor in this process and print the report at the end")
	return c
}

func printPlan(s *run.State) {
	edit, read := 0, 0
	for _, t := range s.Tasks {
		if t.Access == brief.AccessEdit {
			edit++
		} else {
			read++
		}
	}
	ui.Outf("plan     %s  %d tasks  (edit %d, read %d)", s.Title, len(s.Tasks), edit, read)
	for _, t := range s.Tasks {
		lane, level := "—", ""
		if t.Route != nil {
			lane = t.Route.FamilyID
			level = string(t.Route.Level)
		}
		blocked := ""
		if len(t.BlockedBy) > 0 {
			var nums []string
			for _, ref := range t.BlockedBy {
				if b, ok := s.Task(ref); ok {
					nums = append(nums, fmt.Sprint(b.Number))
				}
			}
			blocked = "blocked by " + strings.Join(nums, ", ")
		}
		note := "provisional"
		if t.Waiting != "" {
			note = t.Waiting
		}
		ui.Outf("  %-2d %-34s %-11s %-12s %-14s %-5s %s  %s", t.Number, cutRunTitle(t.Title, 34), t.Category, level, lane, t.Access, note, blocked)
	}
}

func cutRunTitle(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// startRun launches the supervisor inline or detached.
func (a *app) startRun(env *dispatch.Env, s *run.State, wait bool) error {
	if wait {
		if err := run.Start(env, s.Folder); err != nil {
			return err
		}
		return a.printRunReport(s.Folder)
	}
	if err := detachRun(s.Folder); err != nil {
		return err
	}
	ui.Outf("run %s started  (maestro status %s; maestro wait %s)", s.Run, s.Run, s.Run)
	return nil
}

func detachRun(folder string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(folder, "supervisor.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(self, "_run", folder)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (a *app) runSupervisorCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_run <folder>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, err := run.Load(args[0])
			if err != nil {
				return err
			}
			if err := os.Chdir(s.Project); err != nil {
				return err
			}
			env, err := a.env(false)
			if err != nil {
				return err
			}
			return run.Start(env, args[0])
		},
	}
}

func (a *app) approveCmd() *cobra.Command {
	var wait bool
	c := &cobra.Command{
		Use:   "approve <run>",
		Short: "Approve a breakdown that was held for approval",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			env, err := a.env(false)
			if err != nil {
				return err
			}
			folder, s, err := findRun(env.Root, args[0])
			if err != nil {
				return err
			}
			if s.State != run.StateHeld {
				return fmt.Errorf("run %s is %s, not held", s.Run, s.State)
			}
			s.State = run.StateLive
			s.Approval = nil
			if err := s.Save(); err != nil {
				return err
			}
			_ = folder
			return a.startRun(env, s, wait)
		},
	}
	c.Flags().BoolVar(&wait, "wait", false, "keep the supervisor in this process")
	return c
}

func (a *app) resumeCmd() *cobra.Command {
	var task, lane, briefPath string
	var skip, wait bool
	c := &cobra.Command{
		Use:   "resume <run> --task <n> (--lane F[:E] | --brief <file> | --skip)",
		Short: "Continue a paused Run with one answer for the stuck Task",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if task == "" {
				return fmt.Errorf("--task is required")
			}
			n := 0
			for _, set := range []bool{lane != "", briefPath != "", skip} {
				if set {
					n++
				}
			}
			if n != 1 {
				return fmt.Errorf("give exactly one of --lane, --brief, --skip")
			}
			env, err := a.env(false)
			if err != nil {
				return err
			}
			folder, s, err := findRun(env.Root, args[0])
			if err != nil {
				return err
			}
			t, ok := s.Task(task)
			if !ok {
				return fmt.Errorf("run %s has no task %q", s.Run, task)
			}
			if briefPath != "" {
				if briefPath, err = filepath.Abs(briefPath); err != nil {
					return err
				}
				if _, err := brief.Parse(briefPath); err != nil {
					return err
				}
			}
			ans := run.Resume{Task: t.ID, Lane: lane, Brief: briefPath, Skip: skip}
			b, _ := json.Marshal(ans)
			if err := os.WriteFile(filepath.Join(folder, "control", fmt.Sprintf("resume-%s-%d.json", t.ID, time.Now().UnixNano())), b, 0o644); err != nil {
				return err
			}
			switch {
			case skip:
				ui.Outf("task %s skipped", t.ID)
			case lane != "":
				ui.Outf("task %s re-routed on %s  requested   attempt %d of %d", t.ID, lane, t.Attempt+1, env.Roster.Failure.MaxDispatches)
			default:
				ui.Outf("task %s will restart with the brief %s", t.ID, briefPath)
			}
			if _, live := run.LiveRunFor(env.Root); live {
				ui.Outf("run %s is live; the supervisor picks the answer up", s.Run)
				return nil
			}
			ui.Outf("run %s resumed", s.Run)
			return a.startRun(env, s, wait)
		},
	}
	c.Flags().StringVar(&task, "task", "", "task number or id")
	c.Flags().StringVar(&lane, "lane", "", "re-route on this Lane")
	c.Flags().StringVar(&briefPath, "brief", "", "restart with a corrected Brief file")
	c.Flags().BoolVar(&skip, "skip", false, "skip the Task; the Run continues")
	c.Flags().BoolVar(&wait, "wait", false, "keep the supervisor in this process")
	return c
}

func (a *app) addCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <brief.md>",
		Short: "Add a Task to the live Run",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			env, err := a.env(false)
			if err != nil {
				return err
			}
			if _, err := brief.Parse(args[0]); err != nil {
				return err
			}
			idx, live := run.LiveRunFor(env.Root)
			if !live {
				return fmt.Errorf("no live run in %s; use maestro run or maestro dispatch", env.Root)
			}
			src, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			dst := filepath.Join(idx.Folder, "control", fmt.Sprintf("add-%d.md", time.Now().UnixNano()))
			if err := os.WriteFile(dst, src, 0o644); err != nil {
				return err
			}
			ui.Outf("task queued on run %s", idx.Run)
			return nil
		},
	}
}

func (a *app) cancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <task|run>",
		Short: "Cancel a Task or a Run (running Dispatches are stopped)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			env, err := a.env(false)
			if err != nil {
				return err
			}
			idx, live := run.LiveRunFor(env.Root)
			if live {
				s, err := run.Load(idx.Folder)
				if err != nil {
					return err
				}
				if t, ok := s.Task(args[0]); ok {
					if err := os.WriteFile(filepath.Join(idx.Folder, "control", "cancel-"+t.ID), nil, 0o644); err != nil {
						return err
					}
					ui.Outf("task %s (%d) cancel requested on run %s", t.ID, t.Number, s.Run)
					return nil
				}
			}
			folder, s, err := findRun(env.Root, args[0])
			if err != nil {
				return fmt.Errorf("no live task or run %q under %s", args[0], env.Root)
			}
			if s.State == run.StateFinished || s.State == run.StateCancelled {
				ui.Outf("run %s already finished (%s)", s.Run, s.State)
				return nil
			}
			if err := os.MkdirAll(filepath.Join(folder, "control"), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(folder, "control", "cancel"), nil, 0o644); err != nil {
				return err
			}
			ui.Outf("run %s cancel requested", s.Run)
			if live && idx.Run == s.Run {
				return nil
			}
			return run.Start(env, folder)
		},
	}
}

func (a *app) statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [run]",
		Short: "Show the live state of a Run",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			root := config.FindProjectRoot(cwd)
			var folder string
			var s *run.State
			var err error
			if len(args) == 1 {
				folder, s, err = findRun(root, args[0])
			} else {
				folder, err = run.NewestRun(root)
				if err == nil {
					s, err = run.Load(folder)
				}
			}
			if err != nil {
				return err
			}
			if a.json {
				b, _ := json.MarshalIndent(s, "", "  ")
				fmt.Fprintln(os.Stdout, string(b))
				return nil
			}
			fmt.Fprint(os.Stdout, run.Status(s))
			return nil
		},
	}
}

func (a *app) cleanCmd() *cobra.Command {
	var olderThan string
	c := &cobra.Command{
		Use:   "clean",
		Short: "Remove old Run and Council folders (never the ledger)",
		RunE: func(_ *cobra.Command, _ []string) error {
			d, err := time.ParseDuration(strings.Replace(olderThan, "d", "h", 1))
			if err != nil {
				return fmt.Errorf("--older-than: %w", err)
			}
			if strings.HasSuffix(olderThan, "d") {
				n := strings.TrimSuffix(olderThan, "d")
				var days int
				fmt.Sscanf(n, "%d", &days)
				d = time.Duration(days) * 24 * time.Hour
			}
			cwd, _ := os.Getwd()
			root := config.FindProjectRoot(cwd)
			live := map[string]bool{}
			for _, idx := range run.LiveRuns() {
				live[idx.Folder] = true
			}
			removed := 0
			for _, dir := range []string{config.RunsDir(root), config.CouncilsDir(root)} {
				entries, _ := os.ReadDir(dir)
				for _, e := range entries {
					p := filepath.Join(dir, e.Name())
					st, err := os.Stat(p)
					if err != nil || !st.IsDir() || live[p] || time.Since(st.ModTime()) < d {
						continue
					}
					if err := os.RemoveAll(p); err == nil {
						removed++
					}
				}
			}
			ui.Outf("removed %d folder(s) older than %s", removed, olderThan)
			return nil
		},
	}
	c.Flags().StringVar(&olderThan, "older-than", "30d", "age threshold, for example 30d or 12h")
	return c
}

// findRun resolves a Run id to its folder and state.
func findRun(root, id string) (string, *run.State, error) {
	folder := filepath.Join(config.RunsDir(root), id)
	s, err := run.Load(folder)
	if err != nil {
		return "", nil, fmt.Errorf("no run %q under %s", id, root)
	}
	return folder, s, nil
}

func (a *app) printRunReport(folder string) error {
	s, err := run.Load(folder)
	if err != nil {
		return err
	}
	if a.json {
		b, _ := json.MarshalIndent(s, "", "  ")
		fmt.Fprintln(os.Stdout, string(b))
	} else {
		b, err := os.ReadFile(filepath.Join(folder, run.FileReport))
		if err != nil {
			fmt.Fprint(os.Stdout, run.Report(s))
		} else {
			fmt.Fprint(os.Stdout, string(b))
		}
	}
	_, blocked, failed, _, _, _ := s.Counts()
	if s.State == run.StatePaused || blocked > 0 || failed > 0 {
		return &proc.ExitError{Code: proc.CodeOrchestrator}
	}
	return nil
}

// waitRun blocks until a Run ends or pauses, then prints its report.
func (a *app) waitRun(folder string) error {
	started := time.Now()
	for {
		s, err := run.Load(folder)
		if err != nil {
			return err
		}
		switch s.State {
		case run.StateFinished, run.StatePaused, run.StateCancelled:
			return a.printRunReport(folder)
		case run.StateHeld:
			return &proc.ExitError{Code: proc.CodeOrchestrator, Message: fmt.Sprintf("run %s is held for approval (maestro approve %s)", s.Run, s.Run)}
		}
		if time.Since(started) > 5*time.Second {
			if _, live := run.LiveRunFor(s.Project); !live {
				return &proc.ExitError{Code: proc.CodeFailed, Message: fmt.Sprintf("run %s: its supervisor is gone before the run ended; see %s", s.Run, filepath.Join(folder, "supervisor.log"))}
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func (a *app) answerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "answer <task> allow|deny",
		Short: "Answer a relayed permission prompt",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if args[1] != "allow" && args[1] != "deny" {
				return fmt.Errorf("the answer is allow or deny")
			}
			cwd, _ := os.Getwd()
			root := config.FindProjectRoot(cwd)
			folder, ok := dispatch.FindTask(root, args[0])
			if !ok {
				return fmt.Errorf("no task %q under %s", args[0], root)
			}
			if err := os.WriteFile(filepath.Join(folder, "answer-"+args[1]), nil, 0o644); err != nil {
				return err
			}
			ui.Outf("task %s: %s", args[0], args[1])
			return nil
		},
	}
}

func (a *app) tellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tell <task> <text>",
		Short: "Send a note into a running Lane (Solo tab only)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			env, err := a.env(false)
			if err != nil {
				return err
			}
			if env.Solo == nil {
				return fmt.Errorf("tell works only inside Solo")
			}
			folder, ok := dispatch.FindTask(env.Root, args[0])
			if !ok {
				return fmt.Errorf("no task %q under %s", args[0], env.Root)
			}
			rec, err := dispatch.LoadRecord(folder)
			if err != nil || rec.SoloProcess == 0 || rec.State != "running" {
				return fmt.Errorf("task %s has no running Solo tab", args[0])
			}
			return env.Solo.Client.SendInput(rec.SoloProcess, strings.Join(args[1:], " ")+"\n")
		},
	}
}
