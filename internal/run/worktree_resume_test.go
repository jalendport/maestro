package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/worktree"
)

func TestDirtySingleTaskUsesHeadCommit(t *testing.T) {
	sup := worktreeSupervisor(t)
	sup.State.Tasks = sup.State.Tasks[:1]
	runWrite(t, filepath.Join(sup.Env.Root, "base.txt"), "user work\n")
	runWrite(t, filepath.Join(sup.Env.Root, "untracked.txt"), "user work\n")
	chooseMode(sup.Env, sup.State)
	base := runGit(t, sup.Env.Root, "rev-parse", "--short", "HEAD")
	notice, err := StartNotice(sup.State)
	want := "tree has uncommitted changes: Tasks start from " + base + "; your uncommitted work is not in them"
	if err != nil || notice != want || sup.State.Mode != "worktree" {
		t.Fatalf("mode notice: %s, %v, %s", notice, err, sup.State.Mode)
	}
	task := sup.State.Tasks[0]
	if err := sup.taskWorkdir(task, sup.pending[task].brief); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(task.Workdir, "base.txt"))
	if err != nil || string(data) != "base\n" {
		t.Fatalf("Task read uncommitted work: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(task.Workdir, "untracked.txt")); !os.IsNotExist(err) {
		t.Fatalf("Task has user's untracked file: %v", err)
	}
}

func TestRestartDetachedRunKeepsModeAndRetriesFinish(t *testing.T) {
	sup := worktreeSupervisor(t)
	sup.State.Commit = true
	sup.State.Lane = "codex-sol"
	sup.State.Tasks[1].BlockedBy = []string{sup.State.Tasks[0].ID}
	h, _ := sup.Env.Reg.Get("codex")
	sup.Env.Disc = &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{
		"codex": {Path: h.Bin(), Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}}},
	}}
	restore := SetPollIntervalForTest(10 * time.Millisecond)
	t.Cleanup(restore)
	sup.schedule()
	takeResult(t, sup)
	runGit(t, sup.Env.Root, "checkout", "--detach")
	if err := sup.State.Save(); err != nil {
		t.Fatal(err)
	}
	if err := Start(sup.Env, sup.State.Folder); err != nil {
		t.Fatal(err)
	}
	saved, err := Load(sup.State.Folder)
	if err != nil || saved.Mode != "worktree" || saved.State != StatePaused || !saved.FinishPending || saved.PauseReason != "user branch master is not checked out" {
		t.Fatalf("saved mode or pause: %+v, %v", saved, err)
	}
	if task := saved.Tasks[1]; task.State != TaskDone || task.Workdir == "" || task.Workdir == saved.Project {
		t.Fatalf("resumed Task did not use a Worktree: %+v", task)
	}
	runGit(t, sup.Env.Root, "checkout", "master")
	if _, err := QueueResume(saved.Folder, Resume{Finish: true}); err != nil {
		t.Fatal(err)
	}
	if err := Start(sup.Env, saved.Folder); err != nil {
		t.Fatal(err)
	}
	saved, err = Load(saved.Folder)
	if err != nil || saved.State != StateFinished || saved.Mode != "worktree" || saved.FinishPending {
		t.Fatalf("Finish retry: %+v, %v", saved, err)
	}
}

func TestReadWorktreeIsReusedAndRemovedInEitherMode(t *testing.T) {
	for _, mode := range []string{"tree", "worktree"} {
		t.Run(mode, func(t *testing.T) {
			sup := worktreeSupervisor(t)
			task := sup.State.Tasks[0]
			task.Access = brief.AccessRead
			b := sup.pending[task].brief
			if err := sup.taskWorkdir(task, b); err != nil {
				t.Fatal(err)
			}
			dir := task.Workdir
			r, err := worktree.Open(sup.Env.Root, sup.State.Run)
			if err != nil {
				t.Fatal(err)
			}
			w, err := r.Add("change")
			if err != nil {
				t.Fatal(err)
			}
			runWrite(t, filepath.Join(w.Dir, "base.txt"), "merged work\n")
			if _, err := w.Commit("Change the file", []string{"base.txt"}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Merge("change"); err != nil {
				t.Fatal(err)
			}
			runWrite(t, filepath.Join(dir, "base.txt"), "left by stopped supervisor\n")
			task.Workdir = ""
			if err := sup.taskWorkdir(task, b); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "base.txt"))
			if err != nil || string(data) != "merged work\n" || task.Workdir != dir {
				t.Fatalf("reused read Worktree: %q, %v, %s", data, err, task.Workdir)
			}
			sup.State.Mode = mode
			if err := sup.removeReadWorktree(task); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("read Worktree remains: %v", err)
			}
		})
	}
}

func TestCreateAndApprovePermitAnotherLiveRun(t *testing.T) {
	sup := worktreeSupervisor(t)
	if err := WriteIndex(sup.State); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { RemoveIndex(sup.State.Project, sup.State.Run) })
	sup.Env.Disc = &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{
		"codex": {Path: "/fixture/codex", Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}}},
	}}
	plan := filepath.Join(t.TempDir(), "plan.md")
	runWrite(t, plan, "---\ntitle: Another Run\nlane: codex-sol\n---\n\n## Task 1: Change a file\n\n### Goal\nChange the file.\n\n### Done\nThe file is changed.\n")
	s, err := Create(sup.Env, plan, CreateOpts{NoCouncil: true})
	if err != nil || s.Run == sup.State.Run || s.Mode != "worktree" || len(s.Approval) != 0 {
		t.Fatalf("second Run: %+v, %v", s, err)
	}
	t.Cleanup(func() { _, _ = worktree.Remove(s.Project, s.Run) })
	s.State = StateHeld
	runGit(t, sup.Env.Root, "checkout", "--detach")
	if err := Approve(sup.Env, s); err != nil {
		t.Fatal(err)
	}
	if s.Mode != "worktree" || strings.Contains(s.ModeReason, "HEAD") {
		t.Fatalf("approval changed saved mode: %+v", s)
	}
}
