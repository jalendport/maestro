package run

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// pauseNoticeChanged keeps the last set across supervisor restarts without
// changing run.json. Queued answers do not change the set until applied.
func pauseNoticeChanged(s *State) (bool, error) {
	tasks := []string{}
	for _, task := range sortedTasks(s) {
		if !task.DependencyBlocked && (task.State == TaskBlocked || task.State == TaskFailed) {
			tasks = append(tasks, task.ID)
		}
	}
	data, err := json.Marshal(tasks)
	if err != nil {
		return false, err
	}
	path := filepath.Join(s.Folder, "pause-notice.json")
	previous, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if bytes.Equal(previous, data) {
		return false, nil
	}
	return true, os.WriteFile(path, data, 0o644)
}

// PendingResume includes answers queued before the supervisor updates run.json.
func PendingResume(folder, task string) bool {
	paths, _ := filepath.Glob(filepath.Join(folder, "control", "resume-*.json"))
	for _, path := range paths {
		b, err := os.ReadFile(path)
		var answer Resume
		if err == nil && json.Unmarshal(b, &answer) == nil && (task == "" || answer.Task == task) {
			return true
		}
	}
	return false
}

// ResumeTask excludes dependency blocks and Tasks with a queued answer.
func ResumeTask(s *State) *TaskState {
	for _, task := range s.Tasks {
		if NeedsAnswer(s, task) {
			return task
		}
	}
	return nil
}

// PauseReason includes each Blocked item that needs the Orchestrator.
func PauseReason(s *State) string {
	reason := s.PauseReason
	for _, task := range sortedTasks(s) {
		if !NeedsAnswer(s, task) {
			continue
		}
		if text := blockedText(task); text != "" && !strings.Contains(reason, text) {
			if reason != "" {
				reason += "; "
			}
			reason += text
		}
	}
	return reason
}

// NeedsAnswer reports whether a Task needs an Orchestrator answer.
func NeedsAnswer(s *State, task *TaskState) bool {
	return !task.DependencyBlocked && (task.State == TaskBlocked || task.State == TaskFailed) && !PendingResume(s.Folder, task.ID)
}

// ResumeCommand is shared by wait and the Solo wake-up.
func ResumeCommand(s *State, task *TaskState) string {
	if s.FinishPending {
		return "maestro resume " + s.Run + " --finish"
	}
	if task != nil && len(task.Conflicts) > 0 {
		return fmt.Sprintf("maestro resume %s --task %d --merge", s.Run, task.Number)
	}
	ref := "<n>"
	if task != nil {
		ref = fmt.Sprint(task.Number)
	}
	return fmt.Sprintf("maestro resume %s --task %s (--lane <family[:effort]> | --brief <file> | --skip)", s.Run, ref)
}
