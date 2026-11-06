package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/run"
	"github.com/jalendport/maestro/internal/worktree"
)

func sweepDispatchFixture(t *testing.T, root string, legacy bool) (*dispatch.Task, *dispatch.Record, worktree.Run, worktree.Worktree) {
	t.Helper()
	task := &dispatch.Task{Project: root, Run: "r-0001", Task: "abcd", Access: "edit", Standalone: &dispatch.Standalone{}}
	task.Folder = filepath.Join(config.RunsDir(root), task.Run, task.Task)
	id := dispatch.StandaloneRunID(task)
	if legacy {
		id = task.Task
	}
	r, err := worktree.Open(root, id)
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Add(task.Task)
	if err != nil {
		t.Fatal(err)
	}
	task.Workdir = w.Dir
	if err := os.MkdirAll(task.Folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := task.Save(); err != nil {
		t.Fatal(err)
	}
	rec := &dispatch.Record{Project: root, Run: task.Run, Task: task.Task, Path: task.Folder, Workdir: w.Dir,
		State: "ended", EndedAt: time.Now().Add(-48 * time.Hour), Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeBlocked}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	return task, rec, r, w
}

func TestSweepDispatchStatus(t *testing.T) {
	for _, mode := range []string{"hand-landed", "unlanded", "committed", "running", "supervisor", "finish-pending", "legacy", "saved-branch", "fallback-branch"} {
		t.Run(mode, func(t *testing.T) {
			root, git := worktreeConfigFixture(t)
			task, rec, r, w := sweepDispatchFixture(t, root, mode == "legacy")
			if mode != "running" && mode != "supervisor" && mode != "finish-pending" {
				writeWorktreeConfig(t, filepath.Join(w.Dir, "base.txt"), "Lane edit\n")
			}
			landed := mode == "hand-landed" || mode == "legacy" || mode == "saved-branch" || mode == "fallback-branch"
			if landed {
				writeWorktreeConfig(t, filepath.Join(root, "base.txt"), "Lane edit\n")
				git("add", "base.txt")
				git("commit", "-m", "Land the edit by hand")
			}
			if mode == "committed" {
				if _, err := w.Commit("Keep the Task commit", []string{"base.txt"}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "saved-branch" {
				git("checkout", "-b", "other", "HEAD~1")
			}
			if mode == "fallback-branch" {
				control, err := config.ControlDirForFolder(filepath.Join(config.RunsDir(root), r.ID))
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(config.Home(), "worktrees", filepath.Base(filepath.Dir(control)), r.ID, "run.json")
				writeWorktreeConfig(t, path, `{}`)
			}
			if mode == "running" {
				rec.State, rec.Pid = "running", os.Getpid()
			}
			if mode == "supervisor" {
				writeWorktreeConfig(t, filepath.Join(task.Folder, "supervisor.pid"), fmt.Sprint(os.Getpid()))
			}
			if mode == "finish-pending" {
				rec.FinishPending = true
			}
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			var status projectStatus
			output, err := waitCommand(t, "status", "--json")
			if err != nil || json.Unmarshal([]byte(output), &status) != nil {
				t.Fatalf("status: %s, %v", output, err)
			}
			_, statErr := os.Stat(w.Dir)
			if landed != os.IsNotExist(statErr) {
				t.Fatalf("landed=%t: Worktree: %v", landed, statErr)
			}
			branches := git("branch", "--list", "maestro/"+r.ID+"/*")
			if landed && strings.TrimSpace(branches) != "" {
				t.Fatalf("landed branches remain: %s", branches)
			}
			if !landed && (!strings.Contains(branches, r.Branch) || !strings.Contains(branches, w.Branch)) {
				t.Fatalf("unlanded branches removed: %s", branches)
			}
			wantKept := !landed && mode != "running" && mode != "supervisor"
			if (len(status.KeptWorktrees) == 1) != wantKept {
				t.Fatalf("kept identities: %s", output)
			}
			if wantKept {
				kept := status.KeptWorktrees[0]
				command := "maestro clean --older-than 30d"
				if rec.FinishPending {
					command = "maestro dispatch --finish " + task.Task
				}
				if kept.ID != r.ID || kept.Reason == "" || kept.NextCommand != command {
					t.Fatalf("kept identity: %+v", kept)
				}
				text, err := waitCommand(t, "status")
				if err != nil || !strings.Contains(text, kept.ID) || !strings.Contains(text, kept.Reason) || !strings.Contains(text, command) {
					t.Fatalf("text status: %s, %v", text, err)
				}
			}
		})
	}
}

func TestSweepProtectsRuns(t *testing.T) {
	for _, mode := range []string{"live", "held", "paused", "supervisor", "answer", "finish-pending", "landed", "unlanded"} {
		t.Run(mode, func(t *testing.T) {
			root, git := worktreeConfigFixture(t)
			r, err := worktree.Open(root, "r-0001")
			if err != nil {
				t.Fatal(err)
			}
			w, err := r.Add("abcd")
			if err != nil {
				t.Fatal(err)
			}
			s := &run.State{Project: root, Run: r.ID, Folder: filepath.Join(config.RunsDir(root), r.ID), State: run.StateFinished, Mode: "worktree"}
			switch mode {
			case "live", "held", "paused":
				s.State = mode
			case "supervisor":
				s.Pid = os.Getpid()
			case "answer":
				s.Tasks = []*run.TaskState{{ID: "abcd", State: run.TaskBlocked}}
			case "finish-pending":
				s.FinishPending = true
			case "unlanded":
				writeWorktreeConfig(t, filepath.Join(w.Dir, "base.txt"), "keep this edit\n")
			}
			if err := os.MkdirAll(s.Folder, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			kept := sweepWorktrees(root)
			_, statErr := os.Stat(w.Dir)
			if (mode == "landed") != os.IsNotExist(statErr) {
				t.Fatalf("Worktree for %s: %v", mode, statErr)
			}
			if mode != "landed" && !strings.Contains(git("branch", "--list", "maestro/"+r.ID+"/*"), w.Branch) {
				t.Fatal("protected Task branch removed")
			}
			if mode == "unlanded" && (len(kept) != 1 || !strings.Contains(kept[0].Reason, "uncommitted changes") || kept[0].NextCommand != "maestro clean --older-than 30d") {
				t.Fatalf("kept Run: %+v", kept)
			}
			if mode == "finish-pending" && (len(kept) != 1 || kept[0].NextCommand != "maestro resume r-0001 --finish") {
				t.Fatalf("pending Finish: %+v", kept)
			}
		})
	}
}

func sweepStderr(t *testing.T, fn func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	old := os.Stderr
	os.Stderr = file
	defer func() { os.Stderr = old }()
	fn()
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSweepFailureWarnsAndStatusSucceeds(t *testing.T) {
	root, _ := worktreeConfigFixture(t)
	_, _, r, _ := sweepDispatchFixture(t, root, false)
	control, err := config.ControlDirForFolder(filepath.Join(config.RunsDir(root), r.ID))
	if err != nil {
		t.Fatal(err)
	}
	writeWorktreeConfig(t, filepath.Join(config.Home(), "worktrees", filepath.Base(filepath.Dir(control)), r.ID, "run.json"), "invalid json")
	stderr := sweepStderr(t, func() {
		output, err := waitCommand(t, "status", "--json")
		if err != nil || !json.Valid([]byte(output)) {
			t.Fatalf("status failed after sweep error: %s, %v", output, err)
		}
	})
	if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, "Worktree sweep") {
		t.Fatalf("missing warning: %s", stderr)
	}
}

func TestSweepVerbBoundaries(t *testing.T) {
	for _, verb := range []string{"run", "dispatch", "wait", "status", "hook"} {
		t.Run(verb, func(t *testing.T) {
			root, _ := worktreeConfigFixture(t)
			task, _, _, w := sweepDispatchFixture(t, root, false)
			args := []string{verb, "missing.md"}
			if verb == "wait" {
				args = []string{verb, task.Task, "--json"}
			} else if verb == "status" {
				args = []string{verb, "--json"}
			} else if verb == "hook" {
				args = []string{verb, "codex"}
				t.Setenv("MAESTRO_TASK_FOLDER", task.Folder)
				input, err := os.Open(os.DevNull)
				if err != nil {
					t.Fatal(err)
				}
				old := os.Stdin
				os.Stdin = input
				t.Cleanup(func() { os.Stdin = old; input.Close() })
			}
			stderr := sweepStderr(t, func() { _, _ = waitCommand(t, args...) })
			_, err := os.Stat(w.Dir)
			if verb == "hook" {
				if err != nil || stderr != "" {
					t.Fatalf("hook swept Worktree: %v, %s", err, stderr)
				}
			} else if !os.IsNotExist(err) || !strings.Contains(stderr, "1 landed Worktrees") {
				t.Fatalf("%s did not sweep: %v, %s", verb, err, stderr)
			}
		})
	}
}

func TestSweepProgressAndQuiet(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		t.Run(fmt.Sprint(quiet), func(t *testing.T) {
			root, _ := worktreeConfigFixture(t)
			sweepDispatchFixture(t, root, false)
			args := []string{"status", "--json"}
			if quiet {
				args = append(args, "--quiet")
			}
			stderr := sweepStderr(t, func() {
				output, err := waitCommand(t, args...)
				if err != nil || !json.Valid([]byte(output)) {
					t.Fatalf("status: %s, %v", output, err)
				}
			})
			if stderr != "cleaned 1 landed Worktrees\n" {
				t.Fatalf("progress: %q", stderr)
			}
			stderr = sweepStderr(t, func() { sweepWorktrees(root) })
			if stderr != "" {
				t.Fatalf("empty sweep printed %q", stderr)
			}
		})
	}
}
