package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
)

func TestTestsOutsideTouchInRunViews(t *testing.T) {
	for _, taskState := range []string{TaskDone, TaskBlocked, TaskFailed} {
		t.Run(taskState, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			root := t.TempDir()
			folder := filepath.Join(config.RunsDir(root), "r-test")
			taskFolder := filepath.Join(folder, "abcd")
			if err := os.MkdirAll(taskFolder, 0o755); err != nil {
				t.Fatal(err)
			}
			path := "internal/other/x_test.go"
			rec := &dispatch.Record{Path: taskFolder, Verification: dispatch.Verification{Changed: []string{"internal/hook/x.go", path}, TestsOutsideTouch: []string{path}}}
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			task := &TaskState{Number: 1, ID: "abcd", Title: "Fix the hook test", State: taskState, Attempt: 1, Folder: taskFolder}
			s := &State{Run: "r-test", Project: root, Pid: os.Getpid(), Folder: folder, State: StateLive, Tasks: []*TaskState{task}}
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{Report(s), Status(s)} {
				if !strings.Contains(text, "tests outside touch: "+path) || !strings.Contains(text, "changed: internal/hook/x.go, "+path+"\n") {
					t.Fatalf("Run view: %s", text)
				}
				if strings.Index(text, "changed:") > strings.Index(text, "tests outside touch:") {
					t.Fatalf("output order: %s", text)
				}
			}
			if taskState == TaskDone {
				question := endOfRunQuestion(s)
				if !strings.Contains(question, "Task 1 (abcd, Fix the hook test), tests outside touch: "+path) || !strings.Contains(question, "weakened or removed assertions") {
					t.Fatalf("Council question: %s", question)
				}
			} else if reminder := ReminderItems(root); !strings.Contains(reminder, "tests outside touch: "+path) || !strings.Contains(reminder, taskState+"  run r-test  task abcd") {
				t.Fatalf("Reminder: %s", reminder)
			}
			if taskState != TaskDone {
				s.State = StatePaused
				if err := s.Save(); err != nil {
					t.Fatal(err)
				}
				if reminder := ReminderItems(root); !strings.Contains(reminder, "tests outside touch: "+path) {
					t.Fatalf("paused Reminder: %s", reminder)
				}
			}
		})
	}
}
