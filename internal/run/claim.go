package run

import (
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/ui"
)

// claim checks the machine and Account caps and saves the Dispatch Route under
// one lock. Other supervisors must see the claim before the Lane starts.
func (sup *Supervisor) claim(t *TaskState, route *router.Route) (bool, error) {
	if err := config.EnsureHome(); err != nil {
		return false, err
	}
	lock, err := os.OpenFile(filepath.Join(config.Home(), "concurrency.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return false, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	caps := sup.Env.Roster.Concurrency
	if sup.machineRunning() >= caps.MaxTotal {
		t.Waiting = "machine cap"
		return false, nil
	}
	if acct := route.Account; caps.MaxPerAccount > 0 && acct != "" && sup.accountRunning(acct) >= caps.MaxPerAccount {
		t.Waiting = "account cap " + acct
		return false, nil
	}
	claimed := false
	err = withControlLock(sup.State.Folder, func() error {
		// QueueResume checks the saved Task state under this same lock. An
		// answer that arrived since the last poll must be applied first.
		if PendingResume(sup.State.Folder, t.ID) {
			return nil
		}
		previous := *t
		t.State = TaskRunning
		t.Waiting = ""
		t.Route = route
		t.Provisional = false
		t.StartedAt = time.Now()
		t.LastOutput = t.StartedAt
		t.ResumeLane = ""
		if err := sup.State.Save(); err != nil {
			*t = previous
			return err
		}
		// Keep an applied answer across restarts, but reset the notice only
		// once that answer takes a slot. Other Tasks can still be waiting.
		if path := resumeWorkPath(sup.State, t); fileExists(path) {
			if err := resetPauseNotice(sup.State); err != nil {
				ui.Warnf("pause notice: %v", err)
			} else if err := os.Remove(path); err != nil {
				ui.Warnf("pause notice: %v", err)
			}
		}
		claimed = true
		return nil
	})
	return claimed, err
}
