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
	t.Setenv("MAESTRO_HOME", t.TempDir())
	for _, mode := range []string{"worktree", "tree"} {
		t.Run(mode, func(t *testing.T) {
			s := &run.State{Title: "Plan", Mode: mode, Tasks: []*run.TaskState{
				{Number: 1, ID: "a", Title: "First", Access: "edit"},
				{Number: 2, ID: "b", Title: "Second", Access: "edit", After: []int{1}},
			}}
			want := "worktrees: on"
			if mode == "tree" {
				s.ModeReason = "worktrees off"
				want = "worktrees: off (worktrees off: Tasks run one at a time)"
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

func TestRunPrintsDirtyBaseAndAnotherLiveRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "user@example.test")
	t.Setenv("GIT_COMMITTER_NAME", "Test User")
	t.Setenv("GIT_COMMITTER_EMAIL", "user@example.test")
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "master")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "Start test repository")
	r, err := worktree.Open(root, "r-new")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = worktree.Remove(root, r.ID) })
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("user work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := &run.State{Run: "r-other", Project: root}
	if err := run.WriteIndex(other); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() error {
		printPlan(&run.State{Run: r.ID, Project: root, Mode: "worktree"})
		return nil
	})
	for _, want := range []string{"worktrees: on", "tree has uncommitted changes: Tasks start from " + git("rev-parse", "--short", r.Base) + "; your uncommitted work is not in them", "another Run is live: r-other"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
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
	for _, status := range []string{run.StateFinished, run.StateCancelled, run.StatePaused, "finish-pending", "blocked"} {
		r, err := worktree.Open(root, "r-"+status)
		if err != nil {
			t.Fatal(err)
		}
		merged, err := r.Add("merged")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(merged.Dir, "change.txt"), []byte(status), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := merged.Commit("Change file", []string{"change.txt"}); err != nil {
			t.Fatal(err)
		}
		if result, err := r.Merge("merged"); err != nil || len(result.Conflicts) != 0 {
			t.Fatalf("Merge: %+v, %v", result, err)
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
		protected := status == "finish-pending" || status == "blocked"
		if protected {
			s.State = run.StatePaused
			s.FinishPending = status == "finish-pending"
			if status == "blocked" {
				s.Tasks = []*run.TaskState{{ID: "task", State: run.TaskBlocked}}
			}
		}
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(folder, old, old); err != nil {
			t.Fatal(err)
		}
		output, err := waitCommand(t, "clean", "--older-than", "1d")
		if err != nil {
			t.Fatalf("clean: %s, %v", output, err)
		}
		if protected {
			if _, err := os.Stat(w.Dir); err != nil {
				t.Fatalf("protected Worktree removed: %v", err)
			}
			if branches := strings.TrimSpace(git("branch", "--list", "maestro/"+r.ID+"/*")); !strings.Contains(branches, w.Branch) || !strings.Contains(branches, r.Branch) {
				t.Fatalf("protected branches removed: %s", branches)
			}
			continue
		}
		if !strings.Contains(output, "removed 1 folder(s)") {
			t.Fatalf("clean count: %s", output)
		}
		if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
			t.Fatalf("Worktree remains: %s, %v", w.Dir, err)
		}
		branches := strings.TrimSpace(git("branch", "--list", "maestro/"+r.ID+"/*"))
		if status == run.StateFinished {
			if branches != "" {
				t.Fatalf("finished branches remain: %s", branches)
			}
		} else {
			for _, branch := range []string{w.Branch, r.Branch} {
				if !strings.Contains(branches, branch) || !strings.Contains(output, "kept branch "+branch) {
					t.Fatalf("missing kept branch %s: %s, %s", branch, branches, output)
				}
			}
		}
	}
}
