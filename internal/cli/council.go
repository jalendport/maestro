package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/council"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/spf13/cobra"
)

func (a *app) councilCmd() *cobra.Command {
	var seats, rounds int
	var lanes []string
	var level, slug, task string
	var challenge, wait bool
	c := &cobra.Command{
		Use:     "council <question>",
		Short:   "Ask several Lanes the same question in parallel",
		Long:    "Pick one read Seat per vendor at the question's Tier, run them in parallel (one or more Rounds), and return a mechanical Merge with Jev's marks on every finding. Returns at once; --wait blocks.",
		Args:    cobra.MaximumNArgs(1),
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
			if challenge && rounds < 2 {
				rounds = 2
			}
			o := council.Opts{Seats: seats, Lanes: lanes, Level: lv, Rounds: rounds, Slug: slug}
			if len(args) == 1 {
				o.Question = args[0]
			}
			if task != "" {
				if err := reviewTaskOpts(env, task, &o); err != nil {
					return err
				}
			}
			c, err := council.Create(env, o)
			if err != nil {
				return err
			}
			printSeats(env, c)
			if wait {
				if _, err := council.Start(env, c.Path); err != nil {
					return err
				}
				done, _ := council.Load(c.Path)
				council.PrintMerge(done, a.json)
				return nil
			}
			if err := detachCouncil(c.Path); err != nil {
				return err
			}
			ui.Outf("started.  Collect with: maestro wait %s", c.Slug)
			return nil
		},
	}
	c.Flags().IntVar(&seats, "seats", 3, "number of Seats, 2 to 4")
	c.Flags().StringArrayVar(&lanes, "lane", nil, "request a Seat: family[:effort] (repeatable)")
	c.Flags().StringVar(&level, "level", "", "Judgment level of the question")
	c.Flags().IntVar(&rounds, "rounds", 1, "rounds, 1 to 4; rounds after the first are challenge rounds")
	c.Flags().BoolVar(&challenge, "challenge", false, "short for --rounds 2")
	c.Flags().StringVar(&slug, "slug", "", "folder name under .agents/councils/")
	c.Flags().StringVar(&task, "task", "", "review one Task's result (its Brief and diff become the context)")
	c.Flags().BoolVar(&wait, "wait", false, "block until the Council ends and print the Merge")
	return c
}

func printSeats(env *dispatch.Env, c *council.Council) {
	var seats []string
	for _, s := range c.Seats {
		seats = append(seats, fmt.Sprintf("seat  %s %s", s.Family, s.Effort))
	}
	ui.Outf("council %s  %s  %d seats  %d rounds  → %s", c.Slug, strings.ToUpper(string(c.Tier[:1]))+string(c.Tier[1:]), len(c.Seats), c.RoundsAsked, relPath(env.Root, c.Path))
	ui.Outf("  %s", strings.Join(seats, "   "))
	if len(c.Relaxed) > 0 {
		ui.Warnf("relaxed: %s", strings.Join(c.Relaxed, "; "))
	}
}

// reviewTaskOpts fills a Council from one Task's Brief and diff.
func reviewTaskOpts(env *dispatch.Env, id string, o *council.Opts) error {
	folder, ok := dispatch.FindTask(env.Root, id)
	if !ok {
		return fmt.Errorf("no task %q under %s", id, env.Root)
	}
	rec, err := dispatch.LoadRecord(folder)
	if err != nil {
		return fmt.Errorf("task %s has no dispatch record yet", id)
	}
	if o.Question == "" {
		o.Question = "Review this result: does the work meet the brief, and what is wrong with it?"
	}
	o.Trigger = council.TriggerTask
	o.Task = rec.Task
	o.Run = rec.Run
	o.Authors = []string{rec.Route.FamilyID}
	o.Tier = rec.Route.Tier
	briefRel, _ := filepath.Rel(env.Root, filepath.Join(folder, dispatch.FileBrief))
	reportRel, _ := filepath.Rel(env.Root, filepath.Join(folder, dispatch.FileReport))
	o.Files = []string{briefRel, reportRel}
	if diff := dispatch.DiffFor(env.Root, rec); diff != "" {
		p := filepath.Join(folder, "diff.patch")
		if err := os.WriteFile(p, []byte(diff), 0o644); err == nil {
			rel, _ := filepath.Rel(env.Root, p)
			o.Files = append(o.Files, rel)
		}
	}
	o.Context = fmt.Sprintf("The task under review is %q. Its brief, its report, and its diff are listed under Files.", rec.TaskInfo.Title)
	return nil
}

func detachCouncil(folder string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := os.Create(filepath.Join(folder, "supervisor.log"))
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(self, "_council", folder)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(folder, "supervisor.pid"), []byte(fmt.Sprint(cmd.Process.Pid)), 0o644)
	return cmd.Process.Release()
}

func (a *app) councilSupervisorCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_council <folder>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := council.Load(args[0])
			if err != nil {
				return err
			}
			if err := os.Chdir(c.Project); err != nil {
				return err
			}
			env, err := a.env(false)
			if err != nil {
				return err
			}
			_, err = council.Start(env, args[0])
			return err
		},
	}
}

// waitCouncil blocks until a Council ends, then prints the Merge.
func (a *app) waitCouncil(folder string) error {
	started := time.Now()
	for {
		c, err := council.Load(folder)
		if err != nil {
			return err
		}
		if c.State == "finished" {
			council.PrintMerge(c, a.json)
			if c.SingleOpinion {
				return &proc.ExitError{Code: proc.CodeOrchestrator}
			}
			return nil
		}
		if time.Since(started) > 5*time.Second && c.Pid > 0 && syscall.Kill(c.Pid, 0) != nil {
			return &proc.ExitError{Code: proc.CodeFailed, Message: fmt.Sprintf("council %s: its supervisor is gone; see %s", c.Slug, filepath.Join(folder, "supervisor.log"))}
		}
		time.Sleep(2 * time.Second)
	}
}

func relPath(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}
