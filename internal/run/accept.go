package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/ui"
)

const filePendingAcceptance = "accept-pending.json"

// completeAcceptance keeps the original result separate from the Dispatch
// that resolves its merge conflict. The saved result survives a restart.
func (sup *Supervisor) completeAcceptance(task *TaskState) error {
	if task.State != TaskDone {
		return nil
	}
	path := filepath.Join(task.Folder, filePendingAcceptance)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var rec dispatch.Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return err
	}
	if current, err := dispatch.LoadRecord(task.Folder); err != nil || current.Attempt != rec.Attempt {
		rec.Path = filepath.Join(task.Folder, dispatch.FileAttempts, strconv.Itoa(rec.Attempt))
	}
	rec.Outcome.Decision = "accepted"
	if err := rec.Save(); err != nil {
		return err
	}
	accepted := rec
	accepted.Kind = "acceptance"
	if err := ledger.Append(&accepted); err != nil {
		ui.Warnf("ledger: %v", err)
	}
	task.Decision = "accepted"
	task.Error, task.Cause, task.Waiting = "", "", ""
	task.EndedAt = time.Now()
	if err := os.Remove(path); err != nil {
		ui.Warnf("acceptance: %v", err)
	}
	return nil
}
