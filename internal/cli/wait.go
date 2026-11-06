package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/run"
)

var (
	waitPollInterval = 2 * time.Second
	waitProcessGrace = 5 * time.Second
	waitSleep        = time.Sleep
)

// waitTaskRun follows the Task state when the Task belongs to a Run. A Dispatch
// result can be from an earlier attempt that the Orchestrator answered.
func (a *app) waitTaskRun(folder, id string) (bool, error) {
	s, err := run.Load(filepath.Dir(folder))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, ok := s.Task(id)
	if !ok {
		return false, nil
	}
	var stop func()
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	started := time.Now()
	for {
		task, ok := s.Task(id)
		if !ok {
			return true, fmt.Errorf("run %s no longer has task %s", s.Run, id)
		}
		switch task.State {
		case run.TaskDone:
			rec, err := dispatch.LoadRecord(folder)
			if err != nil {
				return true, err
			}
			return true, exit(dispatch.Print(rec, a.json))
		case run.TaskSkipped, run.TaskCancelled:
			return true, a.result(task, "task %s  %s", id, task.State)
		}
		if item, err := taskRunWaitItem(s, task); err != nil {
			return true, err
		} else if item != nil {
			return true, a.printWaitItem(item)
		}
		if s.State == run.StateFinished || s.State == run.StateCancelled {
			return true, a.result(task, "task %s  %s", id, task.State)
		}
		if stop == nil && run.SupervisorAlive(s) {
			stop = waitAwake()
		}
		if time.Since(started) > waitProcessGrace && !run.SupervisorAlive(s) {
			return true, &proc.ExitError{Code: proc.CodeFailed, Message: fmt.Sprintf("run %s: its supervisor is gone before task %s ended; see %s", s.Run, id, filepath.Join(s.Folder, "supervisor.log"))}
		}
		waitSleep(waitPollInterval)
		s, err = run.Load(filepath.Dir(folder))
		if err != nil {
			return true, err
		}
	}
}

func taskRunWaitItem(s *run.State, task *run.TaskState) (*waitItem, error) {
	if (task.State == run.TaskWaiting || task.DependencyBlocked) &&
		(s.State == run.StatePaused || s.State == run.StateHeld) && !run.SupervisorAlive(s) {
		return runWaitItem(s)
	}
	if task.State == run.TaskRunning {
		item, err := relayItem(filepath.Join(s.Folder, task.ID), task.ID)
		if item != nil {
			item.Run = s.Run
		}
		return item, err
	}
	if s.State == run.StateLive && !run.SupervisorAlive(s) {
		return nil, nil
	}
	if task.DependencyBlocked || (task.State != run.TaskBlocked && task.State != run.TaskFailed) || run.PendingResume(s.Folder, task.ID) {
		return nil, nil
	}
	item := &waitItem{Kind: "task_blocked", Run: s.Run, Task: task.ID, Reason: taskWaitReason(task), NextCommand: run.ResumeCommand(s, task), JudgedFiles: taskJudgedFiles(task)}
	if task.State == run.TaskFailed {
		item.Kind = "task_failed"
	}
	if s.State == run.StatePaused || s.PauseReason != "" {
		item.Kind = "run_paused"
		item.ReportPath = filepath.Join(s.Folder, run.FileReport)
		if s.PauseReason != "" {
			item.Reason = s.PauseReason + "; " + item.Reason
		}
	}
	return item, nil
}

// waitAwake loads only the sleep switch; it does not refresh discovery or Budget.
func waitAwake() func() {
	r, err := roster.Load()
	if err == nil {
		if stop := proc.KeepAwake(r.KeepAwake()); stop != nil {
			return stop
		}
	}
	return func() {}
}

// waitItem is the single JSON object returned when wait needs an answer.
type waitItem struct {
	Kind        string   `json:"kind"`
	Run         string   `json:"run,omitempty"`
	Task        string   `json:"task,omitempty"`
	RelayID     string   `json:"relay_id,omitempty"`
	Reason      string   `json:"reason"`
	NextCommand string   `json:"next_command"`
	ReportPath  string   `json:"report_path,omitempty"`
	JudgedFiles []string `json:"judged_files,omitempty"`
}

func relayItem(folder, task string) (*waitItem, error) {
	if rec, err := dispatch.LoadRecord(folder); err != nil || rec.State != "running" || !dispatch.ProcessAlive(rec) {
		return nil, nil
	}
	request, err := dispatch.PendingRelay(folder)
	if err != nil || request == nil {
		return nil, err
	}
	return &waitItem{
		Kind: "relay", Task: task, RelayID: request.ID,
		Reason:      request.ToolName + " " + dispatch.RelayInput(request.ToolInput),
		NextCommand: fmt.Sprintf("maestro answer %s allow|deny --relay %s", task, request.ID),
	}, nil
}

func (a *app) printWaitItem(item *waitItem) error {
	label := item.Kind
	if item.Run != "" {
		label += "  run " + item.Run
	}
	if item.Task != "" {
		label += "  task " + item.Task
	}
	report := ""
	if item.ReportPath != "" {
		report = "\nreport: " + item.ReportPath
	}
	if item.JudgedFiles != nil {
		files := strings.Join(item.JudgedFiles, ", ")
		if files == "" {
			files = "none"
		}
		report += "\njudged files: " + files
	}
	if err := a.result(item, "%s  %s\nnext: %s%s", label, item.Reason, item.NextCommand, report); err != nil {
		return err
	}
	return exit(proc.CodeOrchestrator)
}

func runWaitItem(s *run.State) (*waitItem, error) {
	if s.State == run.StateLive && !run.SupervisorAlive(s) {
		return nil, nil
	}
	if s.State == run.StateHeld {
		return &waitItem{Kind: "run_held", Run: s.Run, Reason: "held for approval", NextCommand: "maestro approve " + s.Run}, nil
	}
	stuck := run.ResumeTask(s)
	for _, task := range s.Tasks {
		if task.State == run.TaskRunning {
			item, err := relayItem(filepath.Join(s.Folder, task.ID), task.ID)
			if err != nil || item != nil {
				if item != nil {
					item.Run = s.Run
				}
				return item, err
			}
		}
	}
	if (s.State == run.StatePaused || s.PauseReason != "") && !run.PendingResume(s.Folder, "") {
		item := &waitItem{Kind: "run_paused", Run: s.Run, Reason: run.PauseReason(s), NextCommand: run.ResumeCommand(s, stuck), ReportPath: filepath.Join(s.Folder, run.FileReport), JudgedFiles: taskJudgedFiles(stuck)}
		if stuck != nil {
			item.Task = stuck.ID
			if item.Reason == "" {
				item.Reason = taskWaitReason(stuck)
			}
		}
		if item.Reason == "" {
			item.Reason = "Run needs an answer from the Orchestrator"
		}
		return item, nil
	}
	if stuck != nil {
		kind := "task_blocked"
		if stuck.State == run.TaskFailed {
			kind = "task_failed"
		}
		return &waitItem{Kind: kind, Run: s.Run, Task: stuck.ID, Reason: taskWaitReason(stuck), NextCommand: run.ResumeCommand(s, stuck), JudgedFiles: taskJudgedFiles(stuck)}, nil
	}
	return nil, nil
}

func taskJudgedFiles(task *run.TaskState) []string {
	if task != nil && task.Attempt > 1 {
		if task.JudgedFiles != nil {
			return task.JudgedFiles
		}
		if rec, err := dispatch.LoadRecord(task.Folder); err == nil {
			return append([]string{}, rec.Verification.Changed...)
		}
	}
	return nil
}

func taskWaitReason(task *run.TaskState) string {
	if len(task.Conflicts) > 0 {
		return "merge conflict in " + strings.Join(task.Conflicts, ", ") + "; Worktree: " + task.Workdir
	}
	if rec, err := dispatch.LoadRecord(task.Folder); err == nil && rec.BlockedReason() != "" {
		return rec.BlockedReason()
	}
	if task.Error != "" {
		return task.Error
	}
	return "Task needs an answer from the Orchestrator"
}
