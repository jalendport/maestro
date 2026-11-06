package dispatch

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/jalendport/maestro/internal/verify"
	"github.com/jalendport/maestro/internal/worktree"
)

// Standalone keeps the steps needed to retry landing without a new Lane.
type Standalone struct {
	ModeReason   string        `json:"mode_reason,omitempty"`
	Merged       bool          `json:"merged,omitempty"`
	MergedHead   string        `json:"merged_head,omitempty"`
	Outcome      *Outcome      `json:"outcome,omitempty"`
	LandingCheck *LandingCheck `json:"landing_check,omitempty"`
}

// LandingCheck is the result at the merged Run branch head.
type LandingCheck struct {
	State    string               `json:"state"`
	Reason   string               `json:"reason,omitempty"`
	Head     string               `json:"head,omitempty"`
	Checks   []verify.CheckResult `json:"checks,omitempty"`
	Duration string               `json:"duration,omitempty"`
}

func (c *LandingCheck) Line() string {
	if c == nil {
		return ""
	}
	switch c.State {
	case "skipped":
		return "landing check: skipped (" + c.Reason + ")"
	case "passed":
		return fmt.Sprintf("landing check: passed (%d checks, %s)", len(c.Checks), c.Duration)
	default:
		return "landing check: " + c.State
	}
}

// CheckLanding is shared by Runs and standalone Dispatches. prior is set
// only when exactly one edit Task was merged into the Run branch.
func CheckLanding(r worktree.Run, cmds []string, idle, budget time.Duration, prior *Verification) (*LandingCheck, error) {
	c := &LandingCheck{State: "skipped", Reason: "no project checks"}
	if len(cmds) == 0 {
		return c, nil
	}
	c.State, c.Reason = "failed", ""
	dir, cleanup, err := r.LandingWorktree()
	if err != nil {
		return c, fmt.Errorf("landing check failed: prepare Worktree: %w", err)
	}
	defer cleanup()
	c.Head = verify.Head(dir)
	if prior != nil && prior.CommitRange == c.Head && prior.StartCommit != "" && prior.StartCommit == verify.Head(r.Root) {
		allPassed := true
		for _, cmd := range cmds {
			passed := false
			for _, check := range prior.Checks {
				if check.Command == cmd && check.OK && !check.Skipped {
					passed = true
				}
			}
			allPassed = allPassed && passed
		}
		if allPassed {
			c.State, c.Reason = "skipped", "one merged Task checked on the same base"
			return c, nil
		}
	}
	start := time.Now()
	c.Checks = verify.RunChecks(dir, cmds, idle, budget)
	c.Duration = time.Since(start).Round(time.Millisecond).String()
	for _, check := range c.Checks {
		if !check.OK {
			line := ui.FirstLine(strings.TrimSpace(check.Output), 0)
			if line == "" {
				line = check.Reason
			}
			if line == "" {
				line = "command failed"
			}
			return c, fmt.Errorf("landing check failed: %s: %s", check.Command, line)
		}
	}
	c.State = "passed"
	return c, nil
}

func standaloneReason(env *Env, t *Task) string {
	if !env.Roster.Worktrees() || (env.Project != nil && env.Project.Worktrees != nil && !*env.Project.Worktrees) {
		return "worktrees off"
	}
	checks := append([]string{}, t.Checks...)
	var links []string
	if env.Project != nil {
		checks = append(checks, env.Project.Checks...)
		links = env.Project.WorktreeLinks
	}
	_, reason := worktree.Eligible(t.Project, [][]string{t.Touch}, checks, links)
	return reason
}

// StandaloneModeLine reports why an edit Dispatch will use the project tree.
func StandaloneModeLine(env *Env, t *Task) string {
	if reason := standaloneReason(env, t); reason != "" {
		return "worktrees: off (" + reason + ")"
	}
	return "worktrees: on"
}

func openStandalone(env *Env, t *Task) error {
	if t.LedgerByRun || t.Council || t.Access != brief.AccessEdit || (t.Workdir != "" && t.Standalone == nil) {
		return nil
	}
	if t.Standalone == nil {
		t.Standalone = &Standalone{}
	}
	t.Standalone.ModeReason = standaloneReason(env, t)
	if t.Standalone.ModeReason != "" {
		return t.Save()
	}
	r, err := worktree.Open(t.Project, StandaloneRunID(t))
	if err != nil {
		return err
	}
	if t.Workdir != "" {
		return r.CheckTask(t.Task, t.Workdir)
	}
	w, err := r.Add(t.Task, t.Touch)
	if w.Dir != "" {
		t.Workdir = w.Dir
		if saveErr := t.Save(); saveErr != nil {
			return saveErr
		}
	}
	return err
}

func landStandalone(env *Env, t *Task, rec *Record) {
	_, found := rec.Report()
	if rec.Outcome.Outcome == OutcomeUnverified && (!found || len(rec.Verification.Changed) == 0) {
		return
	}
	outcome := rec.Outcome
	t.Standalone.Outcome = &outcome
	if err := t.Save(); err != nil {
		blockStandalone(t, rec, err)
		return
	}
	if err := finishStandalone(env, t, rec); err != nil {
		blockStandalone(t, rec, err)
	}
}

func finishStandalone(env *Env, t *Task, rec *Record) error {
	r, err := worktree.Open(t.Project, StandaloneRunID(t))
	if err != nil {
		return err
	}
	rec.RunBranch = r.Branch
	prior := &rec.Verification
	if t.Standalone.Merged {
		prior = nil
		if _, err := os.Stat(t.Workdir + "/.git"); err == nil {
			if err := r.CheckTask(t.Task, t.Workdir); err != nil {
				return err
			}
			dirty, err := worktree.Dirty(t.Workdir)
			if err != nil {
				return err
			}
			if len(dirty) > 0 || (t.Standalone.MergedHead != "" && verify.Head(t.Workdir) != t.Standalone.MergedHead) {
				fix := *rec
				fix.Outcome = *t.Standalone.Outcome
				fix.Verification.Changed = dirty
				if err := CommitChanges(env, t, &fix); err != nil {
					return err
				}
				t.Standalone.Merged = false
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if !t.Standalone.Merged {
		if err := CommitChanges(env, t, rec); err != nil {
			return err
		}
		merged, err := r.Merge(t.Task)
		if err != nil {
			return err
		}
		if len(merged.Conflicts) > 0 {
			return fmt.Errorf("merge conflict in %s", strings.Join(merged.Conflicts, ", "))
		}
		t.Standalone.Merged = true
		t.Standalone.MergedHead = merged.Head
		if err := t.Save(); err != nil {
			return err
		}
	}
	if err := r.FinishChecked(t.Commit, func() error {
		var cmds []string
		if env.Project != nil {
			cmds = env.Project.Checks
		}
		idle, _ := time.ParseDuration(t.IdleLimit)
		budget, _ := time.ParseDuration(t.WallLimit)
		result, err := CheckLanding(r, cmds, idle, budget, prior)
		t.Standalone.LandingCheck = result
		rec.Events = append(rec.Events, Event{At: time.Now(), Kind: "landing check", Text: result.Line()})
		if saveErr := t.Save(); saveErr != nil {
			return saveErr
		}
		return err
	}); err != nil {
		// Merge removes the Task Worktree. Keep one for a hand fix when landing stops.
		w, addErr := r.Add(t.Task, t.Touch)
		if addErr == nil {
			t.Workdir, rec.Workdir = w.Dir, w.Dir
			t.Standalone.MergedHead = verify.Head(w.Dir)
			addErr = t.Save()
		}
		if addErr != nil {
			return fmt.Errorf("%w; keep Worktree: %v", err, addErr)
		}
		return err
	}
	rec.Outcome = *t.Standalone.Outcome
	rec.FinishPending = false
	return nil
}

func blockStandalone(t *Task, rec *Record, err error) {
	reason := strings.ReplaceAll(err.Error(), "maestro resume "+StandaloneRunID(t)+" --finish", "maestro dispatch --finish "+t.Task)
	rec.RunBranch = "maestro/" + StandaloneRunID(t) + "/run"
	rec.Outcome.Outcome, rec.Outcome.Decision = OutcomeBlocked, "blocked"
	rec.Outcome.ErrorText = reason + "; Run branch " + rec.RunBranch
	rec.FinishPending = true
}

// FinishStandalone retries a stopped landing. It does not start a Lane.
func FinishStandalone(env *Env, t *Task) (*Record, error) {
	rec, err := LoadRecord(t.Folder)
	if err != nil {
		return nil, err
	}
	if t.Standalone == nil || t.LedgerByRun || t.Standalone.Outcome == nil || !rec.FinishPending || rec.State != "ended" {
		return nil, fmt.Errorf("task %s has no standalone landing to finish", t.Task)
	}
	rec.State, rec.Pid = "running", os.Getpid()
	if err := rec.Save(); err != nil {
		return nil, err
	}
	if err := finishStandalone(env, t, rec); err != nil {
		blockStandalone(t, rec, err)
	}
	rec.State = "ended"
	if err := rec.Save(); err != nil {
		return rec, err
	}
	if err := ledger.Append(rec); err != nil {
		ui.Warnf("ledger: %v", err)
	}
	return rec, nil
}
