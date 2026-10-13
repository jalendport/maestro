package run

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func quietRunEnv(t *testing.T) *dispatch.Env {
	t.Helper()
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	off := false
	r := &roster.Roster{}
	r.Failure.KeepAwake = &off
	r.Concurrency.MaxParallel = 4
	r.Concurrency.MaxTotal = 6
	r.Concurrency.MaxPerAccount = 2
	return &dispatch.Env{Root: t.TempDir(), Roster: r}
}

func saveRun(t *testing.T, env *dispatch.Env, tasks ...*TaskState) *State {
	t.Helper()
	folder := filepath.Join(config.RunsDir(env.Root), "r-0001")
	if err := os.MkdirAll(filepath.Join(folder, "control"), 0755); err != nil {
		t.Fatal(err)
	}
	s := &State{Run: "r-0001", Project: env.Root, Folder: folder, State: StateLive, StartedAt: time.Now(), Tasks: tasks}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	return s
}

func resumeFile(t *testing.T, folder string, r Resume) string {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "control", "resume-"+r.Task+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFailedChainEndsPaused(t *testing.T) {
	for _, terminal := range []string{TaskFailed, TaskBlocked, TaskSkipped, TaskCancelled} {
		t.Run(terminal, func(t *testing.T) {
			env := quietRunEnv(t)
			s := saveRun(t, env,
				&TaskState{Number: 1, ID: "first", Title: "First", State: terminal},
				&TaskState{Number: 2, ID: "second", Title: "Second", State: TaskWaiting, Access: brief.AccessEdit, BlockedBy: []string{"First"}},
				&TaskState{Number: 3, ID: "third", Title: "Third", State: TaskWaiting, Access: brief.AccessRead, BlockedBy: []string{"Second"}},
			)
			if err := Start(env, s.Folder); err != nil {
				t.Fatal(err)
			}
			got, err := Load(s.Folder)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != StatePaused || got.Pid != 0 {
				t.Fatalf("Run: %+v", got)
			}
			for _, task := range got.Tasks[1:] {
				if task.State != TaskBlocked || task.Error != "blocked by 1 ("+terminal+")" {
					t.Fatalf("Task: %+v", task)
				}
				if !strings.Contains(got.PauseReason, fmt.Sprintf("task %d (blocked)", task.Number)) {
					t.Fatalf("pause: %s", got.PauseReason)
				}
			}
			report, err := os.ReadFile(filepath.Join(s.Folder, FileReport))
			if err != nil || !strings.Contains(string(report), "maestro resume r-0001 --task") {
				t.Fatalf("report: %s, %v", report, err)
			}
			if _, live := LiveRunFor(env.Root); live {
				t.Fatal("Run still has a live index")
			}
		})
	}
}

func TestFailedRunCanResume(t *testing.T) {
	env := quietRunEnv(t)
	s := saveRun(t, env, &TaskState{Number: 1, ID: "abcd", State: TaskFailed, Error: "failed check"})
	if err := Start(env, s.Folder); err != nil {
		t.Fatal(err)
	}
	got, err := Load(s.Folder)
	if err != nil || got.State != StatePaused || !strings.Contains(got.PauseReason, "task 1 (failed)") {
		t.Fatalf("paused Run: %+v, %v", got, err)
	}
	resumeFile(t, s.Folder, Resume{Task: "abcd", Skip: true})
	if err := Start(env, s.Folder); err != nil {
		t.Fatal(err)
	}
	got, err = Load(s.Folder)
	if err != nil || got.State != StateFinished || got.Tasks[0].State != TaskSkipped || got.PauseReason != "" {
		t.Fatalf("resumed Run: %+v, %v", got, err)
	}
}

func TestDependenciesNeedCompletedTasks(t *testing.T) {
	for _, state := range []string{TaskWaiting, TaskRunning, TaskFailed, TaskBlocked, TaskSkipped, TaskCancelled, TaskDone} {
		t.Run(state, func(t *testing.T) {
			s := &State{Tasks: []*TaskState{
				{Number: 1, ID: "first", State: state, Outcome: dispatch.OutcomeUnverified},
				{Number: 2, ID: "second", State: TaskWaiting, BlockedBy: []string{"first"}},
			}}
			sup := &Supervisor{State: s}
			if ready := sup.blockedReason(s.Tasks[1]) == ""; ready != (state == TaskDone) {
				t.Fatalf("ready = %v", ready)
			}
		})
	}
	// A dead dependency must be found even after a live dependency in the list.
	s := &State{Tasks: []*TaskState{{Number: 1, ID: "live", State: TaskRunning}, {Number: 2, ID: "dead", State: TaskFailed}, {Number: 3, ID: "wait", State: TaskWaiting, BlockedBy: []string{"live", "dead"}}}}
	sup := &Supervisor{State: s}
	sup.updateDependencies()
	if s.Tasks[2].State != TaskBlocked || s.Tasks[2].Error != "blocked by 2 (failed)" {
		t.Fatalf("Task: %+v", s.Tasks[2])
	}
	if sup.anyStartable() {
		t.Fatal("no Task can start")
	}
}

func TestResumeReleasesDependencyBlocks(t *testing.T) {
	env := quietRunEnv(t)
	s := saveRun(t, env,
		&TaskState{Number: 1, ID: "first", State: TaskFailed},
		&TaskState{Number: 2, ID: "second", State: TaskWaiting, BlockedBy: []string{"first"}},
		&TaskState{Number: 3, ID: "third", State: TaskWaiting, BlockedBy: []string{"second"}},
	)
	sup := &Supervisor{Env: env, State: s}
	sup.updateDependencies()
	sup.applyResume(resumeFile(t, s.Folder, Resume{Task: "first", Lane: "codex-sol"}))
	for _, task := range s.Tasks {
		if task.State != TaskWaiting || task.DependencyBlocked {
			t.Fatalf("Task was not released: %+v", task)
		}
	}
	if !sup.anyStartable() {
		t.Fatal("resumed first Task must be ready")
	}
	if sup.blockedReason(s.Tasks[2]) == "" {
		t.Fatal("third Task must wait for second")
	}
}

func TestResumeBriefRefreshesSchedulerAccess(t *testing.T) {
	env := quietRunEnv(t)
	task := &TaskState{Number: 1, ID: "first", Title: "Stable dependency title", State: TaskBlocked, Access: brief.AccessRead, Category: "review"}
	s := saveRun(t, env, task, &TaskState{Number: 2, ID: "editing", State: TaskRunning, Access: brief.AccessEdit})
	path := filepath.Join(t.TempDir(), "corrected.md")
	corrected := &brief.Brief{Fields: brief.Fields{Title: "New Brief title", Category: "fix", Access: brief.AccessEdit, Touch: []string{"fixed.go"}, Checks: []string{"go test ./..."}, WallLimit: "2h"}, Goal: "Fix the defect.", Done: "Checks pass."}
	if err := brief.Write(corrected, path); err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{Env: env, State: s, running: map[string]chan struct{}{"editing": make(chan struct{})}}
	sup.applyResume(resumeFile(t, s.Folder, Resume{Task: "first", Brief: path}))
	if task.Access != brief.AccessEdit || task.Category != "fix" || task.Title != "Stable dependency title" {
		t.Fatalf("Task: %+v", task)
	}
	b, err := brief.Parse(task.BriefPath)
	if err != nil || len(b.Touch) != 1 || b.Touch[0] != "fixed.go" || len(b.Checks) != 1 || b.Checks[0] != "go test ./..." || b.WallLimitDuration() != 2*time.Hour {
		t.Fatalf("Dispatch Brief: %+v, %v", b, err)
	}
	sup.schedule()
	if task.State != TaskWaiting || task.Waiting != "tree busy" {
		t.Fatalf("second edit was not held: %+v", task)
	}
}

func TestAlwaysCouncilSkipsNoChangedFiles(t *testing.T) {
	env := quietRunEnv(t)
	env.Project = &config.Project{Councils: "always"}
	s := saveRun(t, env, &TaskState{Number: 1, ID: "done", State: TaskDone, Outcome: dispatch.OutcomeUnverified, Route: &router.Route{Tier: roster.TierHeavy}})
	if err := Start(env, s.Folder); err != nil {
		t.Fatal(err)
	}
	got, err := Load(s.Folder)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateFinished || len(got.Councils) != 0 || !strings.Contains(Report(got), "not run (no changed files)") {
		t.Fatalf("Run: %+v\n%s", got, Report(got))
	}
}
