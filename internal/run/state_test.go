package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
)

func TestStateTask(t *testing.T) {
	first := &TaskState{Number: 1, ID: "abcd", Title: "Add the command"}
	second := &TaskState{Number: 2, ID: "ef01", Title: "Test the command"}
	s := &State{Tasks: []*TaskState{first, second}}
	for _, tt := range []struct {
		name string
		ref  string
		want *TaskState
		ok   bool
	}{
		{name: "number", ref: "2", want: second, ok: true},
		{name: "id", ref: "abcd", want: first, ok: true},
		{name: "title", ref: "Test the command", want: second, ok: true},
		{name: "unknown reference", ref: "unknown"},
		{name: "unknown number", ref: "3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := s.Task(tt.ref)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("Task(%q) = (%+v, %t), want (%+v, %t)", tt.ref, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestStateCounts(t *testing.T) {
	s := &State{Tasks: []*TaskState{
		{State: TaskWaiting},
		{State: TaskRunning},
		{State: TaskDone},
		{State: TaskBlocked},
		{State: TaskFailed},
		{State: TaskSkipped},
		{State: TaskCancelled},
	}}
	done, blocked, failed, running, waiting, skipped := s.Counts()
	got := [6]int{done, blocked, failed, running, waiting, skipped}
	want := [6]int{1, 1, 1, 1, 1, 2}
	if got != want {
		t.Fatalf("Counts() = %v, want %v (done, blocked, failed, running, waiting, skipped)", got, want)
	}
}

func TestStateSaveLoad(t *testing.T) {
	folder := t.TempDir()
	started := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	s := &State{
		Run:       "r-0009",
		Project:   filepath.Join(folder, "project"),
		Plan:      "plan.md",
		Title:     "Add the command",
		State:     StateLive,
		Pid:       os.Getpid(),
		StartedAt: started,
		Awake:     true,
		Folder:    folder,
		Tasks: []*TaskState{
			{
				Number: 1, ID: "abcd", Title: "Add the command",
				BriefPath: "abcd/brief.md", Access: "edit", Category: "implement",
				State: TaskDone, Attempt: 1, Outcome: "verified",
				StartedAt: started, EndedAt: started.Add(time.Minute),
			},
			{
				Number: 2, ID: "ef01", Title: "Test the command",
				BriefPath: "ef01/brief.md", Access: "edit", Category: "implement",
				BlockedBy: []string{"abcd"}, State: TaskWaiting,
				Provisional: true, Excluded: []string{"codex-sol"},
			},
		},
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(folder, FileState+".tmp")); !os.IsNotExist(err) {
		t.Fatalf("Save() left a temporary file; stat error: %v", err)
	}
	got, err := Load(folder)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, s) {
		t.Fatalf("Load() = %+v with Tasks %+v, want %+v with Tasks %+v", got, got.Tasks, s, s.Tasks)
	}
}

func TestLiveRuns(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	if err := os.MkdirAll(config.RunsIndexDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	live := Index{
		Run: "r-0009", Project: project, Folder: filepath.Join(config.RunsDir(project), "r-0009"),
		Pid: os.Getpid(), StartedAt: time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC),
	}
	dead := Index{Run: "r-0008", Project: project, Pid: 2147483000}
	for _, idx := range []Index{live, dead} {
		b, err := json.Marshal(idx)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(config.RunsIndexDir(), idx.Run+".json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := LiveRuns(); !reflect.DeepEqual(got, []Index{live}) {
		t.Fatalf("LiveRuns() = %+v, want %+v", got, []Index{live})
	}
	if _, err := os.Stat(filepath.Join(config.RunsIndexDir(), dead.Run+".json")); !os.IsNotExist(err) {
		t.Fatalf("LiveRuns() did not remove the dead entry; stat error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.RunsIndexDir(), live.Run+".json")); err != nil {
		t.Fatalf("LiveRuns() did not retain the live entry: %v", err)
	}
	if got, ok := LiveRunFor(project); !ok || got != live {
		t.Fatalf("LiveRunFor(%q) = (%+v, %t), want (%+v, true)", project, got, ok, live)
	}
}

func TestRunIndexSeparatesProjectsWithSameRunID(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	projects := []string{t.TempDir(), t.TempDir()}
	var states []*State
	for _, project := range projects {
		folder := filepath.Join(config.RunsDir(project), "r-0001")
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		state := &State{Run: "r-0001", Project: project, Folder: folder, StartedAt: time.Now()}
		if err := state.Save(); err != nil {
			t.Fatal(err)
		}
		if err := WriteIndex(state); err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
	}
	if got := LiveRuns(); len(got) != 2 {
		t.Fatalf("live Runs: %+v", got)
	}
	for _, state := range states {
		idx, ok := LiveRunFor(state.Project)
		if !ok || idx.Run != state.Run || idx.Folder != state.Folder {
			t.Fatalf("index: %+v", idx)
		}
		newest, err := NewestRun(state.Project)
		if err != nil || newest != state.Folder {
			t.Fatalf("newest: %s %v", newest, err)
		}
	}
	RemoveIndex(states[0].Project, states[0].Run)
	if _, ok := LiveRunFor(states[0].Project); ok {
		t.Fatal("removed Run remains")
	}
	if idx, ok := LiveRunFor(states[1].Project); !ok || idx.Folder != states[1].Folder {
		t.Fatalf("other project's Run removed: %+v", idx)
	}
}
