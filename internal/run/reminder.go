package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/council"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/ui"
)

// ReminderItems reads the project's live items from disk. It makes no Solo or
// network call and leaves control files unchanged.
func ReminderItems(root string) string {
	var items []string
	folders, _ := dispatch.RunFolders(root)
	for _, entry := range folders {
		folder := filepath.Join(config.RunsDir(root), entry.Name())
		s, err := Load(folder)
		if err == nil {
			// Use the project of the prompt, not paths saved in an old Run.
			s.Folder = folder
			if s.State != StateLive && s.State != StatePaused {
				continue
			}
			if s.State == StateLive && !SupervisorAlive(s) {
				continue
			}
			if (s.State == StatePaused || s.PauseReason != "") && !PendingResume(folder, "") {
				reason := PauseReason(s)
				if reason == "" {
					reason = "Run needs an answer from the Orchestrator"
				}
				items = append(items, fmt.Sprintf("paused  run %s  %s  %s", s.Run, reason, ResumeCommand(s, ResumeTask(s))))
			}
			for _, task := range s.Tasks {
				if task == nil {
					continue
				}
				task.Folder = filepath.Join(folder, task.ID)
				if task.State == TaskRunning {
					// The Dispatch has the start time of the current attempt.
					if rec, err := dispatch.LoadRecord(task.Folder); err == nil {
						if rec.State == "running" && dispatch.ProcessAlive(rec) {
							items = append(items, reminderRelay(task.Folder, task.ID)...)
							items = append(items, reminderDispatch(task.ID, task.Title, rec))
						}
					} else if SupervisorAlive(s) {
						items = append(items, reminderLane(task.ID, task.Title, task.Route, task.StartedAt))
					}
				}
				if s.State == StateLive && !task.DependencyBlocked && (task.State == TaskBlocked || task.State == TaskFailed) && !PendingResume(folder, task.ID) {
					reason := blockedText(task)
					if reason == "" {
						reason = "Task needs an answer from the Orchestrator"
					}
					items = append(items, fmt.Sprintf("%s  run %s  task %s  %s  %s", task.State, s.Run, task.ID, reason, ResumeCommand(s, task)))
				}
			}
			continue
		}
		// A malformed Run state does not make its Tasks standalone Dispatches.
		if !os.IsNotExist(err) {
			continue
		}
		tasks, _ := os.ReadDir(folder)
		for _, task := range tasks {
			if !task.IsDir() {
				continue
			}
			path := filepath.Join(folder, task.Name())
			rec, err := dispatch.LoadRecord(path)
			if err != nil || rec.State != "running" || !dispatch.ProcessAlive(rec) {
				continue
			}
			items = append(items, reminderRelay(path, rec.Task)...)
			items = append(items, reminderDispatch(rec.Task, rec.TaskInfo.Title, rec))
		}
	}
	// Council Seats are Lanes too. Only the current Round can still run.
	for _, c := range council.List(root) {
		if c.State != "running" {
			continue
		}
		folder := filepath.Join(config.CouncilsDir(root), c.Slug)
		if c.RoundsRun > 1 {
			folder = filepath.Join(folder, fmt.Sprintf("round-%d", c.RoundsRun))
		}
		for _, seat := range c.Seats {
			if seat == nil {
				continue
			}
			path := filepath.Join(folder, seat.Family)
			if rec, err := dispatch.LoadRecord(path); err == nil && rec.State == "running" && dispatch.ProcessAlive(rec) {
				items = append(items, reminderRelay(path, rec.Task)...)
				items = append(items, reminderDispatch(rec.Task, rec.TaskInfo.Title, rec))
			}
		}
	}
	// Reasons and tool input can contain newlines; each item stays on one line.
	for i := range items {
		items[i] = strings.NewReplacer("\r", " ", "\n", " ").Replace(items[i])
	}
	return strings.Join(items, "\n")
}

func reminderRelay(folder, task string) []string {
	request, err := dispatch.PendingRelay(folder)
	if err != nil || request == nil {
		return nil
	}
	return []string{fmt.Sprintf("relay  task %s  %s %s  maestro answer %s allow|deny --relay %s", task, request.ToolName, dispatch.RelayInput(request.ToolInput), task, request.ID)}
}

func reminderDispatch(task, title string, rec *dispatch.Record) string {
	line := reminderLane(task, title, rec.Route, rec.StartedAt)
	if dispatch.ChecksRunning(rec) {
		line += "  checks running"
	}
	return line
}

func reminderLane(task, title string, route *router.Route, started time.Time) string {
	lane, elapsed := "—", "—"
	if route != nil {
		lane = strings.TrimSpace(route.FamilyID + " " + route.Effort)
	}
	if !started.IsZero() {
		elapsed = ui.Duration(time.Since(started))
	}
	return fmt.Sprintf("running  task %s  %s  %s  %s", task, title, lane, elapsed)
}
