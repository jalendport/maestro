package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/run"
)

func TestResumeRefusesRunningDoneAndSkippedTasks(t *testing.T) {
	for _, state := range []string{run.TaskRunning, run.TaskDone, run.TaskSkipped} {
		for _, answer := range [][]string{{"--lane", "codex-sol"}, {"--brief", "corrected.md"}, {"--skip"}, {"--merge"}, {"--accept"}} {
			t.Run(state+answer[0], func(t *testing.T) {
				_, s := waitFixture(t)
				if err := os.WriteFile(filepath.Join(s.Project, "corrected.md"), []byte("# Correct the Task\n\n## Goal\nCorrect the work.\n\n## Done\nThe work is complete.\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				s.Tasks[0].State = state
				if err := s.Save(); err != nil {
					t.Fatal(err)
				}
				args := append([]string{"resume", s.Run, "--task", "1"}, answer...)
				if output, err := waitCommand(t, args...); err == nil || !strings.Contains(err.Error(), state) || !strings.Contains(err.Error(), "abcd") {
					t.Fatalf("resume accepted %s Task: %s, %v", state, output, err)
				}
				if run.PendingResume(s.Folder, "") {
					t.Fatal("refused answer was queued")
				}
			})
		}
	}
}
