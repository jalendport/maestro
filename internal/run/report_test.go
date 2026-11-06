package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/dispatch"
)

func TestStatusTaskElapsed(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		name, state, want string
		started           time.Time
	}{
		{"cancelled before start", TaskCancelled, "0    —", time.Time{}},
		{"skipped before start", TaskSkipped, "0    —", time.Time{}},
		{"running without start", TaskRunning, "1    —   idle", time.Time{}},
		{"finished after start", TaskDone, "0    1m00s", now.Add(-time.Minute)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &State{Folder: t.TempDir(), StartedAt: now, Tasks: []*TaskState{{
				Number: 1, ID: "task", Title: "Task elapsed", State: tt.state,
				StartedAt: tt.started, EndedAt: now, LastOutput: now,
			}}}
			got := Status(s)
			if !strings.Contains(got, tt.want+"\n") && !strings.Contains(got, tt.want+" ") {
				t.Fatalf("missing %q in status: %s", tt.want, got)
			}
		})
	}
}

func TestReportNamesTheFilesJudgedForAResumedTask(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	folder := t.TempDir()
	taskFolder := filepath.Join(folder, "abcd")
	if err := os.MkdirAll(taskFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := &dispatch.Record{Path: taskFolder, Attempt: 3, Verification: dispatch.Verification{Changed: []string{"own.go", "shared.go"}}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	s := &State{Folder: folder, Project: t.TempDir(), State: StatePaused, Tasks: []*TaskState{{Number: 1, ID: "abcd", State: TaskBlocked, Attempt: 3, Folder: taskFolder}}}
	if report := Report(s); !strings.Contains(report, "judged files: own.go, shared.go\n") {
		t.Fatalf("Run report lacks judged files: %s", report)
	}
}
