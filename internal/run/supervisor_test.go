package run

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/roster"
)

func TestRunCancelEndsThroughNormalPath(t *testing.T) {
	for _, state := range []string{StateLive, StateHeld, StatePaused} {
		t.Run(state, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("PATH", t.TempDir())
			folder := t.TempDir()
			if err := os.Mkdir(filepath.Join(folder, "control"), 0o755); err != nil {
				t.Fatal(err)
			}
			s := &State{Run: "r-test", Project: t.TempDir(), Folder: folder, State: state, StartedAt: time.Now(),
				Tasks: []*TaskState{
					{Number: 1, ID: "done", State: TaskDone},
					{Number: 2, ID: "wait", State: TaskWaiting, Access: "edit"},
					{Number: 3, ID: "dependent", State: TaskWaiting, Access: "read", BlockedBy: []string{"wait"}, Waiting: "blocked by 2"},
				}}
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(folder, "control", "cancel"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			r := &roster.Roster{}
			off := false
			r.Failure.KeepAwake = &off
			if err := Start(&dispatch.Env{Roster: r}, folder); err != nil {
				t.Fatal(err)
			}
			got, err := Load(folder)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != StateCancelled || got.EndedAt.IsZero() || got.Pid != 0 {
				t.Fatalf("run did not end cancelled: %+v", got)
			}
			if got.Tasks[0].State != TaskDone {
				t.Fatal("cancel changed a finished Task")
			}
			for _, task := range got.Tasks[1:] {
				if task.State != TaskCancelled || task.Waiting != "" {
					t.Fatalf("waiting Task not cancelled: %+v", task)
				}
			}
			for _, path := range []string{filepath.Join(folder, FileReport), config.LedgerFile()} {
				b, err := os.ReadFile(path)
				if err != nil || !strings.Contains(string(b), StateCancelled) {
					t.Fatalf("missing cancelled result in %s: %q, %v", path, b, err)
				}
			}
			if !strings.Contains(Status(got), StateCancelled) {
				t.Fatal("status does not show cancelled")
			}
		})
	}
}

func TestCancelWaitsForRunningTaskAndPreventsEscalation(t *testing.T) {
	for _, resultError := range []error{nil, errors.New("lane failed during cancel")} {
		folder := t.TempDir()
		if err := os.Mkdir(filepath.Join(folder, "control"), 0o755); err != nil {
			t.Fatal(err)
		}
		task := &TaskState{ID: "abcd", State: TaskRunning}
		ch := make(chan struct{})
		sup := &Supervisor{State: &State{Folder: folder, Tasks: []*TaskState{task}}, running: map[string]chan struct{}{task.ID: ch}}
		for _, marker := range []string{"cancel-abcd", "cancel-abcd", "cancel"} {
			if err := os.WriteFile(filepath.Join(folder, "control", marker), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			sup.consumeControl()
			select {
			case <-ch:
			default:
				t.Fatal("task cancel channel is still open")
			}
			if len(sup.running) != 1 {
				t.Fatal("running Task removed before its result arrived")
			}
		}
		sup.handle(result{task: task, err: resultError, rec: &dispatch.Record{Attempt: 2}})
		if len(sup.running) != 0 || task.State != TaskCancelled || task.Escalations != 0 || !sup.cancelled {
			t.Fatalf("cancel result: %+v, running %d", task, len(sup.running))
		}
	}
}
