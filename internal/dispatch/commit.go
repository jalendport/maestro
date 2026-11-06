package dispatch

import (
	"strings"

	"github.com/jalendport/maestro/internal/worktree"
)

// CommitChanges records only the changed paths after verification.
func CommitChanges(env *Env, task *Task, rec *Record) error {
	report, found := rec.Report()
	if rec.Outcome.Outcome == OutcomeUnverified && (!found || len(rec.Verification.Changed) == 0) {
		return nil
	}
	v := worktree.Vars{Title: task.Title, Run: task.Run, Task: task.Task}
	if report != nil {
		v.Summary = strings.TrimSpace(report.Sections["Summary"])
	}
	if task.Route != nil {
		v.Family, v.Model = task.Route.FamilyID, task.Route.Model
	}
	v.Lane = env.Roster.Identity(v.Family, v.Model)
	template := env.Roster.CommitTemplate()
	if env.Project != nil && env.Project.CommitMessage != nil {
		template = *env.Project.CommitMessage
	}
	dirty, err := worktree.Dirty(task.workdir())
	if err != nil {
		return err
	}
	var changed []string
	for _, path := range rec.Verification.Changed {
		for _, d := range dirty {
			if d == path {
				changed = append(changed, path)
				break
			}
		}
	}
	w := worktree.Worktree{Root: task.Project, Dir: task.workdir()}
	hash, err := w.Commit(worktree.Message(template, v), changed)
	if err == nil && hash != "" {
		rec.Verification.CommitRange = hash
	}
	return err
}
