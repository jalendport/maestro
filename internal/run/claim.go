package run

import (
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/router"
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
		return false, err
	}
	return true, nil
}
