package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/jalendport/maestro/internal/verify"
	"github.com/jalendport/maestro/internal/worktree"
)

func (sup *Supervisor) checkLanding(r worktree.Run) error {
	var cmds []string
	if sup.Env.Project != nil {
		cmds = sup.Env.Project.Checks
	}
	idle := sup.Env.Roster.Failure.IdleLimit.Duration
	budget := sup.Env.Roster.WallLimit(roster.TierDaily).Duration
	var prior *dispatch.Verification
	merged := 0
	for _, task := range sup.State.Tasks {
		if task.Access != brief.AccessEdit || task.State != TaskDone {
			continue
		}
		if saved, err := dispatch.LoadTask(task.Folder); err == nil {
			if wall, err := time.ParseDuration(saved.WallLimit); err == nil && wall > budget {
				budget = wall
			}
		} else if task.Route != nil {
			if wall := sup.Env.Roster.WallLimit(task.Route.Tier).Duration; wall > budget {
				budget = wall
			}
		}
		if rec, err := dispatch.LoadRecord(task.Folder); err == nil && rec.Verification.CommitRange != "" {
			merged++
			v := rec.Verification
			prior = &v
		}
	}
	if merged != 1 {
		prior = nil
	}
	result, err := dispatch.CheckLanding(r, cmds, idle, budget, prior)
	sup.State.LandingCheck = result
	ui.Eventf("landing check", "%s  %s", sup.State.Run, result.Line())
	if saveErr := sup.State.Save(); saveErr != nil {
		return saveErr
	}
	return err
}

func chooseMode(env *dispatch.Env, s *State) {
	s.Mode, s.ModeReason = "tree", ""
	s.RunBranch = ""
	enabled := env.Roster.Worktrees()
	if env.Project != nil && env.Project.Worktrees != nil {
		enabled = enabled && *env.Project.Worktrees
	}
	switch {
	case !enabled:
		s.ModeReason = "worktrees off"
	default:
		var touch [][]string
		var checks, links []string
		if env.Project != nil {
			checks = append(checks, env.Project.Checks...)
			links = env.Project.WorktreeLinks
		}
		for _, task := range s.Tasks {
			touch = append(touch, task.Touch)
			if task.BriefPath != "" {
				b, err := brief.Parse(task.BriefPath)
				if err != nil {
					s.ModeReason = "cannot read Task checks: " + err.Error()
					break
				}
				checks = append(checks, b.Checks...)
			}
		}
		if s.ModeReason == "" {
			ok, reason := worktree.Eligible(s.Project, touch, checks, links)
			s.ModeReason = reason
			if ok {
				r, err := worktree.Open(s.Project, s.Run)
				if err != nil {
					s.ModeReason = "cannot open Worktrees: " + err.Error()
				} else {
					s.Mode, s.RunBranch = "worktree", r.Branch
				}
			}
		}
	}
	setOrder(s)
}

func ModeLine(s *State) string {
	if s.Mode == "worktree" {
		return "worktrees: on"
	}
	reason := s.ModeReason
	if reason == "" {
		reason = "tree mode"
	}
	return "worktrees: off (" + reason + ": Tasks run one at a time)"
}

// StartNotice names the commit that Tasks use when the project has dirty files.
func StartNotice(s *State) (string, error) {
	if s.Mode != "worktree" {
		return "", nil
	}
	dirty, err := worktree.Dirty(s.Project)
	if err != nil || len(dirty) == 0 {
		return "", err
	}
	r, err := worktree.Open(s.Project, s.Run)
	if err != nil {
		return "", err
	}
	out, err := exec.Command("git", "-C", s.Project, "rev-parse", "--short", r.Base).Output()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("tree has uncommitted changes: Tasks start from %s; your uncommitted work is not in them", strings.TrimSpace(string(out))), nil
}

func depends(s *State, task, target *TaskState, seen map[*TaskState]bool) bool {
	if seen[task] {
		return false
	}
	seen[task] = true
	for _, ref := range task.BlockedBy {
		if dep, ok := s.Task(ref); ok && (dep == target || depends(s, dep, target, seen)) {
			return true
		}
	}
	for _, n := range task.After {
		if dep, ok := s.Task(fmt.Sprint(n)); ok && (dep == target || depends(s, dep, target, seen)) {
			return true
		}
	}
	return false
}

func shareFiles(a, b *TaskState) bool {
	for _, pair := range [][2]*TaskState{{a, b}, {b, a}} {
		for _, file := range pair[1].Files {
			for _, pattern := range pair[0].Files {
				if !strings.HasPrefix(pattern, "!") && verify.Match(pattern, file) {
					return true
				}
			}
			if len(pair[0].Touch) > 0 && verify.InTouch(file, pair[0].Touch) {
				return true
			}
		}
	}
	return false
}

func setOrder(s *State) {
	for _, task := range s.Tasks {
		task.After = nil
	}
	for i, task := range s.Tasks {
		if s.Mode != "worktree" || task.Access != brief.AccessEdit {
			continue
		}
		for _, prior := range s.Tasks[:i] {
			if prior.Access == brief.AccessEdit && shareFiles(prior, task) &&
				!depends(s, task, prior, map[*TaskState]bool{}) && !depends(s, prior, task, map[*TaskState]bool{}) {
				task.After = append(task.After, prior.Number)
			}
		}
	}
}

func OrderLine(task *TaskState) string {
	var lines []string
	for _, n := range task.After {
		lines = append(lines, fmt.Sprintf("after %d (files overlap)", n))
	}
	return strings.Join(lines, "; ")
}

func (sup *Supervisor) overlapReason(task *TaskState) string {
	for _, n := range task.After {
		if prior, ok := sup.State.Task(fmt.Sprint(n)); ok && prior.State != TaskDone {
			return fmt.Sprintf("files overlap with %d", n)
		}
	}
	return ""
}

func (sup *Supervisor) taskWorkdir(task *TaskState, b *brief.Brief) error {
	if sup.State.Mode != "worktree" {
		return nil
	}
	if task.ConflictRebase && task.Workdir != "" && (worktree.Worktree{Root: sup.State.Project, Dir: task.Workdir}).Rebasing() {
		// A stopped rebase has no branch checked out; keep the Worktree as it is.
		return nil
	}
	r, err := worktree.Open(sup.State.Project, sup.State.Run)
	if err != nil {
		return err
	}
	sup.State.RunBranch = r.Branch
	if task.Access == brief.AccessRead {
		w, err := r.Read(task.ID)
		if w.Dir != "" {
			task.Workdir = w.Dir
		}
		return err
	}
	w, err := r.Add(task.ID, b.Touch)
	if err == nil {
		task.Workdir = w.Dir
	}
	return err
}

func (sup *Supervisor) removeReadWorktree(task *TaskState) error {
	if task.Access != brief.AccessRead || task.Workdir == "" {
		return nil
	}
	if out, err := exec.Command("git", "-C", sup.State.Project, "worktree", "remove", "--force", task.Workdir).CombinedOutput(); err != nil {
		return fmt.Errorf("remove read Worktree: %w: %s", err, strings.TrimSpace(string(out)))
	}
	task.Workdir = ""
	return nil
}

// prepareConflict starts the rebase for a conflict attempt. A Lane cannot
// write the git folder, so the supervisor starts and continues the rebase and
// the Lane edits the conflicted files. It returns false when the rebase is
// complete with no conflict and the Task needs no attempt.
func (sup *Supervisor) prepareConflict(task *TaskState) (bool, error) {
	r, err := worktree.Open(sup.State.Project, sup.State.Run)
	if err != nil {
		return false, err
	}
	conflicts, err := r.Rebase(worktree.Worktree{Root: r.Root, Dir: task.Workdir})
	if err != nil {
		return false, err
	}
	if len(conflicts) == 0 {
		return false, nil
	}
	task.Conflicts, task.ConflictRebase = conflicts, true
	return true, nil
}

// continueConflict adds the attempt's changes to the stopped rebase and
// continues it.
func (sup *Supervisor) continueConflict(task *TaskState, rec *dispatch.Record) error {
	w := worktree.Worktree{Root: sup.State.Project, Dir: task.Workdir}
	if !task.ConflictRebase || task.Workdir == "" || !w.Rebasing() {
		return nil
	}
	var marked []string
	for _, path := range task.Conflicts {
		if hasConflictMarkers(filepath.Join(task.Workdir, path)) {
			marked = append(marked, path)
		}
	}
	if len(marked) > 0 {
		task.Conflicts = marked
		return fmt.Errorf("merge conflict in %s", strings.Join(marked, ", "))
	}
	paths := append(append([]string{}, task.Conflicts...), rec.Verification.Changed...)
	conflicts, err := w.Continue(paths)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		task.Conflicts = conflicts
		return fmt.Errorf("merge conflict in %s", strings.Join(conflicts, ", "))
	}
	task.ConflictRebase = false
	if head := verify.Head(task.Workdir); head != "" {
		rec.Verification.CommitRange = head
	}
	return nil
}

func (sup *Supervisor) commitTask(task *TaskState, rec *dispatch.Record) error {
	if task.Access != brief.AccessEdit {
		return nil
	}
	b, err := brief.Parse(task.BriefPath)
	if err != nil {
		return err
	}
	if sup.State.Mode != "worktree" && !b.WantsCommit() {
		return nil
	}
	if len(task.Conflicts) > 0 {
		if err := sup.continueConflict(task, rec); err != nil {
			return err
		}
	}
	_, found := rec.Report()
	if rec.Outcome.Outcome == dispatch.OutcomeUnverified && (!found || len(rec.Verification.Changed) == 0) && len(task.Conflicts) == 0 {
		return nil
	}
	input := &dispatch.Task{Project: sup.State.Project, Workdir: task.Workdir, Title: task.Title, Run: sup.State.Run, Task: task.ID, Route: task.Route}
	if err := dispatch.CommitChanges(sup.Env, input, rec); err != nil {
		return err
	}
	if sup.State.Mode == "worktree" {
		return sup.mergeTask(task)
	}
	return nil
}

func (sup *Supervisor) mergeTask(task *TaskState) error {
	r, err := worktree.Open(sup.State.Project, sup.State.Run)
	if err != nil {
		return err
	}
	merged, err := r.Merge(task.ID)
	if err != nil {
		return err
	}
	if len(merged.Conflicts) > 0 {
		task.Conflicts = merged.Conflicts
		if task.ConflictAttempt {
			return fmt.Errorf("merge conflict in %s", strings.Join(task.Conflicts, ", "))
		}
		task.ConflictAttempt = true
		task.State = TaskWaiting
		task.Waiting = "merge conflict attempt"
		return nil
	}
	task.Conflicts = nil
	task.State = TaskDone
	task.Error, task.Cause, task.Waiting = "", "", ""
	return nil
}

// hasConflictMarkers finds the start and end lines that git writes around a
// conflict.
func hasConflictMarkers(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	start, end := false, false
	for _, line := range strings.Split(string(data), "\n") {
		start = start || strings.HasPrefix(line, "<<<<<<< ")
		end = end || strings.HasPrefix(line, ">>>>>>> ")
	}
	return start && end
}

// blockMerge stops a rebase that the supervisor started, so the Worktree is
// on its Task branch for a hand fix.
func (sup *Supervisor) blockMerge(task *TaskState, err error) {
	if task.ConflictRebase && task.Workdir != "" {
		if abortErr := (worktree.Worktree{Root: sup.State.Project, Dir: task.Workdir}).Abort(); abortErr != nil {
			ui.Warnf("task %d: %v", task.Number, abortErr)
		}
		task.ConflictRebase = false
	}
	task.State, task.Cause, task.Waiting = TaskBlocked, "", ""
	task.Error = err.Error()
	if len(task.Conflicts) > 0 {
		task.Error = "merge conflict in " + strings.Join(task.Conflicts, ", ")
	}
}
