package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/run"
	"github.com/jalendport/maestro/internal/worktree"
)

func TestWorktreeWaitAndResumeFlags(t *testing.T) {
	for _, finish := range []bool{false, true} {
		t.Run(map[bool]string{false: "merge", true: "finish"}[finish], func(t *testing.T) {
			_, state := waitFixture(t)
			state.Mode, state.State = "worktree", run.StatePaused
			state.FinishPending = finish
			task := state.Tasks[0]
			task.State, task.Error = run.TaskBlocked, "merge conflict in shared.go"
			task.Workdir, task.Conflicts = filepath.Join(t.TempDir(), "worktree"), []string{"shared.go"}
			args := []string{"resume", state.Run, "--task", "1", "--merge"}
			want := "--merge"
			if finish {
				state.PauseReason = "user branch moved"
				task.State, task.Conflicts = run.TaskDone, nil
				args = []string{"resume", state.Run, "--finish"}
				want = "--finish"
			}
			if err := state.Save(); err != nil {
				t.Fatal(err)
			}
			item, err := runWaitItem(state)
			if err != nil || item == nil || !strings.Contains(item.NextCommand, want) {
				t.Fatalf("wait: %+v, %v", item, err)
			}
			if !finish && !strings.Contains(item.Reason, task.Workdir) {
				t.Fatalf("wait lost Worktree: %+v", item)
			}
			if output, err := waitCommand(t, args...); err != nil {
				t.Fatalf("resume: %s, %v", output, err)
			}
			markers, err := filepath.Glob(filepath.Join(state.Folder, "control", "resume-*.json"))
			if err != nil || len(markers) != 1 {
				t.Fatalf("answer markers: %v, %v", markers, err)
			}
			body, err := os.ReadFile(markers[0])
			var answer run.Resume
			if err != nil || json.Unmarshal(body, &answer) != nil || answer.Finish != finish || answer.Merge == finish {
				t.Fatalf("answer: %s, %v", body, err)
			}
		})
	}
}

func TestRunPrintsModeAndOrder(t *testing.T) {
	for _, mode := range []string{"worktree", "tree"} {
		t.Run(mode, func(t *testing.T) {
			s := &run.State{Title: "Plan", Mode: mode, Tasks: []*run.TaskState{
				{Number: 1, ID: "a", Title: "First", Access: "edit"},
				{Number: 2, ID: "b", Title: "Second", Access: "edit", After: []int{1}},
			}}
			want := "worktrees: on"
			if mode == "tree" {
				s.ModeReason = "tree has uncommitted changes"
				want = "worktrees: off (tree has uncommitted changes: Tasks run one at a time)"
			}
			out := captureStdout(t, func() error { printPlan(s); return nil })
			routes := strings.Index(out, "Second")
			order := strings.Index(out, "after 1 (files overlap)")
			line := strings.Index(out, want)
			if routes < 0 || order < routes || line < order {
				t.Fatalf("run printout:\n%s", out)
			}
		})
	}
}

func TestCleanRemovesEndedWorktreesAndBranches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "user@example.test")
	t.Setenv("GIT_COMMITTER_NAME", "Test User")
	t.Setenv("GIT_COMMITTER_EMAIL", "user@example.test")
	root := t.TempDir()
	t.Chdir(root)
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git("init", "-b", "master")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "Start test repository")
	for _, status := range []string{run.StateFinished, run.StateCancelled, run.StatePaused} {
		r, err := worktree.Open(root, "r-"+status)
		if err != nil {
			t.Fatal(err)
		}
		w, err := r.Add("task")
		if err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-72 * time.Hour)
		folder := filepath.Join(config.RunsDir(root), r.ID)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		s := &run.State{Run: r.ID, Project: root, Mode: "worktree", State: status, Folder: folder, EndedAt: old}
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(folder, old, old); err != nil {
			t.Fatal(err)
		}
		if output, err := waitCommand(t, "clean", "--older-than", "1d"); err != nil {
			t.Fatalf("clean: %s, %v", output, err)
		} else if !strings.Contains(output, "removed 1 folder(s)") {
			t.Fatalf("clean count: %s", output)
		}
		if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
			t.Fatalf("Worktree remains: %s, %v", w.Dir, err)
		}
		if branches := strings.TrimSpace(git("branch", "--list", "maestro/"+r.ID+"/*")); branches != "" {
			t.Fatalf("branches remain: %s", branches)
		}
	}
}
