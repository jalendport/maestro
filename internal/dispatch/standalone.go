package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/jalendport/maestro/internal/worktree"
)

// Standalone keeps the steps needed to retry landing without a new Lane.
type Standalone struct {
	ModeReason string   `json:"mode_reason,omitempty"`
	Merged     bool     `json:"merged,omitempty"`
	Outcome    *Outcome `json:"outcome,omitempty"`
}

func standaloneReason(env *Env, t *Task) string {
	if !env.Roster.Worktrees() || (env.Project != nil && env.Project.Worktrees != nil && !*env.Project.Worktrees) {
		return "worktrees off"
	}
	if err := exec.Command("git", "-C", t.Project, "rev-parse", "--verify", "HEAD").Run(); err != nil {
		return "tree has no git history"
	}
	if err := exec.Command("git", "-C", t.Project, "symbolic-ref", "--quiet", "HEAD").Run(); err != nil {
		return "HEAD is not on a branch"
	}
	links, err := worktree.Link(t.Project, "", t.Touch)
	if err != nil {
		return "cannot read dependencies: " + err.Error()
	}
	if len(links.Refused) > 0 {
		return "Task touches " + strings.Join(links.Refused, ", ")
	}
	return ""
}

// StandaloneModeLine reports why an edit Dispatch will use the project tree.
func StandaloneModeLine(env *Env, t *Task) string {
	if reason := standaloneReason(env, t); reason != "" {
		return "worktrees: off (" + reason + ")"
	}
	return "worktrees: on"
}

func openStandalone(env *Env, t *Task) error {
	if t.LedgerByRun || t.Council || t.Access != brief.AccessEdit || t.Workdir != "" {
		return nil
	}
	if t.Standalone == nil {
		t.Standalone = &Standalone{}
	}
	t.Standalone.ModeReason = standaloneReason(env, t)
	if t.Standalone.ModeReason != "" {
		return t.Save()
	}
	r, err := worktree.Open(t.Project, t.Task)
	if err != nil {
		return err
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
	r, err := worktree.Open(t.Project, t.Task)
	if err != nil {
		return err
	}
	rec.RunBranch = r.Branch
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
		if err := t.Save(); err != nil {
			return err
		}
	}
	if err := r.Finish(t.Commit); err != nil {
		// Merge removes the Task Worktree. Keep one for a hand fix when landing stops.
		w, addErr := r.Add(t.Task, t.Touch)
		if addErr == nil {
			t.Workdir, rec.Workdir = w.Dir, w.Dir
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
	reason := strings.ReplaceAll(err.Error(), "maestro resume "+t.Task+" --finish", "maestro dispatch --finish "+t.Task)
	rec.RunBranch = "maestro/" + t.Task + "/run"
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
