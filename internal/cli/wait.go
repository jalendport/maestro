package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/run"
)

// waitItem is the single JSON object returned when wait needs an answer.
type waitItem struct {
	Kind        string `json:"kind"`
	Run         string `json:"run,omitempty"`
	Task        string `json:"task,omitempty"`
	RelayID     string `json:"relay_id,omitempty"`
	Reason      string `json:"reason"`
	NextCommand string `json:"next_command"`
	ReportPath  string `json:"report_path,omitempty"`
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
		item := &waitItem{Kind: "run_paused", Run: s.Run, Reason: s.PauseReason, NextCommand: run.ResumeCommand(s, stuck), ReportPath: filepath.Join(s.Folder, run.FileReport)}
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
		return &waitItem{Kind: kind, Run: s.Run, Task: stuck.ID, Reason: taskWaitReason(stuck), NextCommand: run.ResumeCommand(s, stuck)}, nil
	}
	return nil, nil
}

func taskWaitReason(task *run.TaskState) string {
	if rec, err := dispatch.LoadRecord(task.Folder); err == nil && len(rec.Permission.Blocked) > 0 {
		return strings.Join(rec.Permission.Blocked, "; ")
	}
	if task.Error != "" {
		return task.Error
	}
	return "Task needs an answer from the Orchestrator"
}
