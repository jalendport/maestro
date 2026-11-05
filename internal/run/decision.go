package run

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

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
		if !task.DependencyBlocked && (task.State == TaskBlocked || task.State == TaskFailed) && !PendingResume(s.Folder, task.ID) {
			return task
		}
	}
	return nil
}

// ResumeCommand is shared by wait and the Solo wake-up.
func ResumeCommand(s *State, task *TaskState) string {
	ref := "<n>"
	if task != nil {
		ref = fmt.Sprint(task.Number)
	}
	return fmt.Sprintf("maestro resume %s --task %s (--lane <family[:effort]> | --brief <file> | --skip)", s.Run, ref)
}
