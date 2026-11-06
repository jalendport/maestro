package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/run"
)

func liveRunsFixture(t *testing.T) (*run.State, *run.State) {
	t.Helper()
	_, first := waitFixture(t)
	first.Mode = "worktree"
	second := &run.State{
		Run: "r-0002", Project: first.Project,
		Folder: filepath.Join(config.RunsDir(first.Project), "r-0002"),
		State:  run.StateLive, Mode: "worktree", Pid: os.Getpid(),
		Tasks: []*run.TaskState{{Number: 1, ID: "ef01", State: run.TaskRunning}},
	}
	for _, s := range []*run.State{first, second} {
		if err := os.MkdirAll(filepath.Join(s.Folder, "control"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		if err := run.WriteIndex(s); err != nil {
			t.Fatal(err)
		}
	}
	return first, second
}

func TestAddSelectsLiveRun(t *testing.T) {
	for _, selection := range []string{"ambiguous", "first", "second", "single", "missing"} {
		t.Run(selection, func(t *testing.T) {
			first, second := liveRunsFixture(t)
			path := filepath.Join(t.TempDir(), "brief.md")
			b := &brief.Brief{Fields: brief.Fields{Title: "Change a file", Access: brief.AccessEdit}, Goal: "Change the file.", Done: "The file is changed."}
			if err := brief.Write(b, path); err != nil {
				t.Fatal(err)
			}
			args := []string{"add", path}
			var target *run.State
			switch selection {
			case "first":
				target = first
				args = append(args, "--run", first.Run)
			case "second":
				target = second
				args = append(args, "--run", second.Run)
			case "single":
				run.RemoveIndex(first.Project, first.Run)
				target = second
			case "missing":
				args = append(args, "--run", "r-missing")
			}
			output, err := waitCommand(t, args...)
			if target == nil {
				want := "--run <id>"
				if selection == "missing" {
					want = "Run r-missing is not live or paused"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("add: %s, %v; want %s", output, err, want)
				}
			} else if err != nil || !strings.Contains(output, target.Run) {
				t.Fatalf("add: %s, %v", output, err)
			}
			for _, s := range []*run.State{first, second} {
				markers, err := filepath.Glob(filepath.Join(s.Folder, "control", "add-*.md"))
				want := 0
				if s == target {
					want = 1
				}
				if err != nil || len(markers) != want {
					t.Fatalf("Run %s markers: %v, %v; want %d", s.Run, markers, err, want)
				}
				if want == 1 {
					queued, err := brief.Parse(markers[0])
					if err != nil || queued.Title != b.Title || queued.Goal != b.Goal {
						t.Fatalf("queued Brief: %+v, %v", queued, err)
					}
				}
			}
		})
	}
}

func TestAddSelectsPausedRun(t *testing.T) {
	for _, selection := range []string{"finish", "blocked", "explicit", "ambiguous", "finished", "paused without work"} {
		t.Run(selection, func(t *testing.T) {
			first, second := liveRunsFixture(t)
			for _, s := range []*run.State{first, second} {
				run.RemoveIndex(s.Project, s.Run)
				s.State, s.Pid = run.StatePaused, 0
				s.FinishPending = true
			}
			second.State = run.StateFinished
			args := []string{"add", filepath.Join(t.TempDir(), "fix.md")}
			switch selection {
			case "blocked":
				first.FinishPending, first.Tasks[0].State = false, run.TaskBlocked
			case "explicit":
				second.State = run.StatePaused
				args = append(args, "--run", first.Run)
			case "ambiguous":
				second.State = run.StatePaused
			case "finished":
				first.State = run.StateFinished
			case "paused without work":
				first.FinishPending = false
			}
			for _, s := range []*run.State{first, second} {
				if err := s.Save(); err != nil {
					t.Fatal(err)
				}
			}
			b := &brief.Brief{Fields: brief.Fields{Title: "Fix the failure", Access: brief.AccessEdit}, Goal: "Fix the failure.", Done: "The checks pass."}
			if err := brief.Write(b, args[1]); err != nil {
				t.Fatal(err)
			}
			output, err := waitCommand(t, args...)
			if selection == "ambiguous" || selection == "finished" || selection == "paused without work" {
				if err == nil {
					t.Fatalf("accepted unavailable Run: %s", output)
				}
				return
			}
			if err != nil || !strings.Contains(output, "task queued on run "+first.Run) {
				t.Fatalf("add paused Run: %s, %v", output, err)
			}
			markers, err := filepath.Glob(filepath.Join(first.Folder, "control", "add-*.md"))
			if err != nil || len(markers) != 1 {
				t.Fatalf("queued Brief: %v, %v", markers, err)
			}
		})
	}
}

func TestCancelFindsSecondLiveRun(t *testing.T) {
	for _, wholeRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "Task", true: "Run"}[wholeRun], func(t *testing.T) {
			first, second := liveRunsFixture(t)
			ref, marker := second.Tasks[0].ID, "cancel-"+second.Tasks[0].ID
			if wholeRun {
				ref, marker = second.Run, "cancel"
			}
			output, err := waitCommand(t, "cancel", ref)
			if err != nil || !strings.Contains(output, second.Run) {
				t.Fatalf("cancel: %s, %v", output, err)
			}
			if _, err := os.Stat(filepath.Join(second.Folder, "control", marker)); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(first.Folder, "control"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("first Run control: %v, %v", entries, err)
			}
		})
	}
}
