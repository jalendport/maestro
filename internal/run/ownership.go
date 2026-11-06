package run

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/worktree"
)

// withControlLock keeps the resume writer and the ownership decision in order.
func withControlLock(folder string, fn func() error) error {
	lock, err := os.OpenFile(filepath.Join(folder, "control", "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return fn()
}

// QueueResume saves an answer and reports whether the supervisor will take it.
func QueueResume(folder string, answer Resume) (bool, error) {
	live := false
	err := withControlLock(folder, func() error {
		s, err := Load(folder)
		if err != nil {
			return err
		}
		task, ok := s.Task(answer.Task)
		if answer.Finish {
			if !s.FinishPending || s.State != StatePaused {
				return fmt.Errorf("Run %s has no pending Finish", s.Run)
			}
		} else if !ok {
			return fmt.Errorf("Run %s has no Task %q", s.Run, answer.Task)
		}
		if !answer.Finish && (task.State == TaskRunning || task.State == TaskDone || task.State == TaskSkipped) {
			return fmt.Errorf("Task %s is %s", task.ID, task.State)
		}
		if answer.Merge && (s.Mode != "worktree" || task.Workdir == "" || len(task.Conflicts) == 0) {
			return fmt.Errorf("Task %s has no merge conflict", task.ID)
		}
		if answer.Merge && (worktree.Worktree{Root: s.Project, Dir: task.Workdir}).Rebasing() {
			return fmt.Errorf("a rebase is in progress in %s; finish it with git rebase --continue, then resume with --merge", task.Workdir)
		}
		b, err := json.Marshal(answer)
		if err != nil {
			return err
		}
		ref := answer.Task
		if answer.Finish {
			ref = "finish"
		}
		path := filepath.Join(folder, "control", fmt.Sprintf("resume-%s-%d.json", ref, time.Now().UnixNano()))
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return err
		}
		live = SupervisorAlive(s)
		return nil
	})
	return live, err
}

// finishOwnership checks for an answer before it gives up the live index.
func (sup *Supervisor) finishOwnership(state string) (bool, error) {
	resumed := false
	err := withControlLock(sup.State.Folder, func() error {
		s := sup.State
		if state == StatePaused && PendingResume(s.Folder, "") {
			sup.consumeControl()
			if !sup.paused {
				s.State, s.Pid, s.EndedAt = StateLive, os.Getpid(), time.Time{}
				if err := s.Save(); err != nil {
					return err
				}
				resumed = true
				return nil
			}
		}
		RemoveIndex(s.Project, s.Run)
		sup.ownsRun = false
		return nil
	})
	return resumed, err
}
