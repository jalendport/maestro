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
	"github.com/jalendport/maestro/internal/router"
)

func TestSameErrorTwiceBlocksTaskAndPausesRun(t *testing.T) {
	for _, archived := range []bool{false, true} {
		name := "separate dispatches"
		if archived {
			name = "same lane retry"
		}
		t.Run(name, func(t *testing.T) {
			folder := t.TempDir()
			task := &TaskState{Number: 1, ID: "abcd", Folder: folder, State: TaskRunning,
				Route: &router.Route{FamilyID: "first", Tier: roster.TierDaily}}
			r := &roster.Roster{}
			r.Failure.MaxDispatches = 6
			sup := &Supervisor{Env: &dispatch.Env{Roster: r},
				State:   &State{Folder: folder, Tasks: []*TaskState{task}},
				running: map[string]chan struct{}{}, errorTexts: map[string]int{}}
			first := &dispatch.Record{Attempt: 1,
				Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, ErrorText: "compiler failed\nmissing symbol"}}
			if archived {
				first.Path = filepath.Join(folder, dispatch.FileAttempts, "1")
				if err := os.MkdirAll(first.Path, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := first.Save(); err != nil {
					t.Fatal(err)
				}
			} else {
				sup.handle(result{task: task, rec: first})
				if task.State != TaskWaiting || task.Escalations != 1 || sup.paused {
					t.Fatalf("first failure: task=%+v paused=%v", task, sup.paused)
				}
				task.Route = &router.Route{FamilyID: "second", Tier: roster.TierHeavy}
			}
			escalations := task.Escalations
			excluded := len(task.Excluded)
			sup.handle(result{task: task, rec: &dispatch.Record{Attempt: 2,
				Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, ErrorText: "compiler failed\nmissing symbol"}}})
			want := "same error twice: compiler failed\nmissing symbol"
			if task.State != TaskBlocked || task.Error != want || task.Attempt != 2 || task.Waiting != "" {
				t.Fatalf("second failure: %+v", task)
			}
			if task.Escalations != escalations || len(task.Excluded) != excluded || sup.escalationsInARow != 0 {
				t.Fatal("repeated failure escalated")
			}
			if !sup.paused || sup.State.PauseReason != want {
				t.Fatalf("Run pause: paused=%v reason=%q", sup.paused, sup.State.PauseReason)
			}
			saved, err := Load(folder)
			if err != nil || saved.Tasks[0].State != TaskBlocked || saved.PauseReason != want {
				t.Fatalf("saved Run: %+v, %v", saved, err)
			}
		})
	}
}

func TestRepeatedErrorIgnoresFailureClassPrefix(t *testing.T) {
	task := &TaskState{Attempt: 1, Outcome: dispatch.OutcomeFailed, Error: "lane error: compile: failed"}
	rec := &dispatch.Record{Attempt: 2, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed,
		ErrorText: "transient error: compile: failed"}}
	if got := repeatedError(task, rec); got != "compile: failed" {
		t.Fatalf("repeated error = %q", got)
	}
}

func TestRepeatedErrorRequiresPreviousFailedAttemptOfSameTask(t *testing.T) {
	for _, tc := range []struct {
		name     string
		attempt  int
		outcome  string
		previous string
		current  string
	}{
		{"different error", 1, dispatch.OutcomeFailed, "compile: first", "compile: second"},
		{"different second line", 1, dispatch.OutcomeFailed, "compile failed\nfirst", "compile failed\nsecond"},
		{"different trailing space", 1, dispatch.OutcomeFailed, "lane error: compile failed ", "lane error: compile failed"},
		{"long different error", 1, dispatch.OutcomeFailed, strings.Repeat("x", 100) + "first", strings.Repeat("x", 100) + "second"},
		{"no previous attempt", 0, "", "", "compile failed"},
		{"nonconsecutive attempt", 0, dispatch.OutcomeFailed, "compile failed", "compile failed"},
		{"successful previous attempt", 1, dispatch.OutcomeUnverified, "compile failed", "compile failed"},
		{"empty error", 1, dispatch.OutcomeFailed, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &TaskState{Attempt: tc.attempt, Outcome: tc.outcome, Error: tc.previous}
			rec := &dispatch.Record{Attempt: 2, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, ErrorText: tc.current}}
			if got := repeatedError(task, rec); got != "" {
				t.Fatalf("blocked on %q", got)
			}
		})
	}
}

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
