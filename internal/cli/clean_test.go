package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/hook"
	"github.com/jalendport/maestro/internal/run"
	"github.com/jalendport/maestro/internal/worktree"
)

func TestCleanStandaloneWorktrees(t *testing.T) {
	for _, mode := range []string{"old identity", "failed", "blocked", "cancelled", "finish-pending", "landed", "recent", "running"} {
		t.Run(mode, func(t *testing.T) {
			root, git := worktreeConfigFixture(t)
			id := dispatch.StandaloneRunID(&dispatch.Task{Run: "r-0001", Task: "abcd"})
			if mode == "old identity" {
				id = "abcd"
			}
			r, err := worktree.Open(root, id)
			if err != nil {
				t.Fatal(err)
			}
			w, err := r.Add("abcd")
			if err != nil {
				t.Fatal(err)
			}
			writeWorktreeConfig(t, filepath.Join(w.Dir, "base.txt"), "Lane work\n")
			if _, err := w.Commit("Change the file", []string{"base.txt"}); err != nil {
				t.Fatal(err)
			}
			if mode == "landed" || mode == "finish-pending" {
				if _, err := r.Merge("abcd"); err != nil {
					t.Fatal(err)
				}
				if mode == "landed" {
					git("merge", "--ff-only", r.Branch)
				}
				w, err = r.Add("abcd")
				if err != nil {
					t.Fatal(err)
				}
			}
			folder := filepath.Join(config.RunsDir(root), "r-0001", "abcd")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			task := &dispatch.Task{Project: root, Workdir: w.Dir, Folder: folder, Run: "r-0001", Task: "abcd",
				Access: "edit", Standalone: &dispatch.Standalone{}}
			if err := task.Save(); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-48 * time.Hour)
			rec := &dispatch.Record{Project: root, Workdir: w.Dir, Run: task.Run, Task: task.Task, Path: folder,
				State: "ended", EndedAt: old, Outcome: dispatch.Outcome{Outcome: mode}, FinishPending: mode == "finish-pending"}
			if mode == "recent" {
				rec.EndedAt = time.Now()
			} else if mode == "running" {
				rec.State, rec.EndedAt = "running", time.Time{}
			}
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(filepath.Dir(folder), old, old); err != nil {
				t.Fatal(err)
			}
			control := controlFolder(t, folder)
			writeWorktreeConfig(t, filepath.Join(control, "lane.json"), `{"root":"`+root+`","run_folder":"`+folder+`"}`)
			output, err := waitCommand(t, "clean", "--older-than", "1d")
			if err != nil {
				t.Fatalf("clean: %s, %v", output, err)
			}
			keep := mode == "recent" || mode == "running"
			for _, path := range []string{w.Dir, folder, control} {
				_, err := os.Stat(path)
				if (keep && err != nil) || (!keep && !os.IsNotExist(err)) {
					t.Fatalf("keep=%t: %s: %v", keep, path, err)
				}
			}
			branches := git("branch", "--list", "maestro/"+id+"/*")
			for _, branch := range []string{r.Branch, w.Branch} {
				want := keep || (mode != "landed" && (branch == w.Branch || mode == "finish-pending"))
				if strings.Contains(branches, branch) != want || (!keep && want && !strings.Contains(output, "kept branch "+branch)) {
					t.Fatalf("keep=%t for branch %s: %s, %s", want, branch, branches, output)
				}
			}
		})
	}
}

func TestCleanStandaloneKeepsOtherDispatchWithSameTaskID(t *testing.T) {
	root, git := worktreeConfigFixture(t)
	var tasks []*dispatch.Task
	for _, id := range []string{"r-0001", "r-0002"} {
		task := &dispatch.Task{Project: root, Run: id, Task: "abcd", Access: "edit", Standalone: &dispatch.Standalone{}}
		task.Folder = filepath.Join(config.RunsDir(root), id, task.Task)
		if err := os.MkdirAll(task.Folder, 0o755); err != nil {
			t.Fatal(err)
		}
		r, err := worktree.Open(root, dispatch.StandaloneRunID(task))
		if err != nil {
			t.Fatal(err)
		}
		w, err := r.Add(task.Task)
		if err != nil {
			t.Fatal(err)
		}
		task.Workdir = w.Dir
		writeWorktreeConfig(t, filepath.Join(w.Dir, "base.txt"), id+"\n")
		if _, err := w.Commit("Change the file", []string{"base.txt"}); err != nil {
			t.Fatal(err)
		}
		if err := task.Save(); err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	if _, err := cleanStandaloneWorktrees(filepath.Dir(tasks[0].Folder)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tasks[0].Workdir); !os.IsNotExist(err) {
		t.Fatalf("expired Worktree remains: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(tasks[1].Workdir, "base.txt")); err != nil || string(data) != "r-0002\n" {
		t.Fatalf("other Worktree changed: %q, %v", data, err)
	}
	if got := strings.TrimSpace(git("show", "maestro/"+dispatch.StandaloneRunID(tasks[1])+"/abcd:base.txt")); got != "r-0002" {
		t.Fatalf("other Task branch changed: %s", got)
	}
}

func TestCleanGlobalControlAndCache(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root, other := t.TempDir(), t.TempDir()
	t.Chdir(root)
	old := time.Now().Add(-48 * time.Hour)
	writeJSON := func(path string, value any) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	makeControl := func(project, id, state string, ended time.Time) (string, string) {
		t.Helper()
		folder := filepath.Join(config.RunsDir(project), id, "abcd")
		if state != "" {
			writeJSON(filepath.Join(folder, dispatch.FileRecord), dispatch.Record{Path: folder, State: state, EndedAt: ended})
		}
		control := controlFolder(t, folder)
		writeJSON(filepath.Join(control, "lane.json"), hook.Lane{Root: project, RunFolder: folder})
		return folder, control
	}
	_, oldControl := makeControl(other, "old-dispatch", "ended", old)
	_, orphan := makeControl(other, "orphan", "", time.Time{})
	_, recent := makeControl(other, "recent", "ended", time.Now())
	_, running := makeControl(other, "running", "running", time.Time{})
	localFolder, localRecent := makeControl(root, "recent", "ended", time.Now())
	if err := os.Chtimes(filepath.Dir(localFolder), old, old); err != nil {
		t.Fatal(err)
	}
	liveFolder, liveControl := makeControl(other, "live-run", "ended", old)
	live := &run.State{Project: other, Run: "live-run", Folder: filepath.Dir(liveFolder), State: run.StateLive, StartedAt: old, Pid: os.Getpid()}
	if err := live.Save(); err != nil {
		t.Fatal(err)
	}
	if err := run.WriteIndex(live); err != nil {
		t.Fatal(err)
	}
	finishedFolder, finishedControl := makeControl(other, "old-run", "ended", old)
	if err := (&run.State{Project: other, Run: "old-run", Folder: filepath.Dir(finishedFolder), State: run.StateFinished, EndedAt: old}).Save(); err != nil {
		t.Fatal(err)
	}
	// The control folder is new; the record's end time sets its age.
	var canaryFolders, canaryControls []string
	for _, name := range []string{"claude", "codex"} {
		cache := filepath.Join(config.Home(), "cache", "maestro-canary-"+name)
		resolved, err := filepath.EvalSymlinks(config.Home())
		if err != nil {
			t.Fatal(err)
		}
		scratch := filepath.Join(resolved, "cache", "maestro-canary-"+name)
		folder := filepath.Join(config.RunsDir(cache), "canary", "check")
		writeJSON(filepath.Join(folder, dispatch.FileRecord), dispatch.Record{Path: folder, State: "ended", EndedAt: old})
		control := controlFolder(t, folder)
		writeJSON(filepath.Join(control, "lane.json"), hook.Lane{Root: cache, RunFolder: folder})
		writeJSON(filepath.Join(config.Home(), "canaries", name+".json"), dispatch.CanaryResult{Harness: name, CheckedAt: old, ScratchFolder: scratch})
		canaryFolders = append(canaryFolders, cache)
		canaryControls = append(canaryControls, control)
	}
	oldCache := filepath.Join(config.Home(), "cache", "old-cache")
	oldCanary := filepath.Join(config.Home(), "cache", "maestro-canary-removed")
	recentCache := filepath.Join(config.Home(), "cache", "recent-cache")
	for _, folder := range []string{oldCache, oldCanary, recentCache} {
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range append([]string{oldCache, oldCanary, recent, running, liveControl}, canaryFolders...) {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	ledger, budget := []byte("Ledger stays\n"), []byte("Budget stays\n")
	if err := os.WriteFile(config.LedgerFile(), ledger, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetFile(), budget, 0o600); err != nil {
		t.Fatal(err)
	}
	command := (&app{}).cleanCmd()
	command.SetArgs([]string{"--older-than", "1d"})
	captureStdout(t, command.Execute)
	for _, path := range []string{oldControl, finishedControl, orphan, oldCache, oldCanary} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("clean kept %s: %v", path, err)
		}
	}
	kept := []string{liveControl, recent, running, recentCache, finishedFolder, localFolder, localRecent}
	kept = append(kept, canaryFolders...)
	kept = append(kept, canaryControls...)
	for _, path := range kept {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("clean removed %s: %v", path, err)
		}
	}
	for path, want := range map[string][]byte{config.LedgerFile(): ledger, config.BudgetFile(): budget} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(want) {
			t.Fatalf("clean changed %s: %q, %v", path, got, err)
		}
	}
}

func TestCleanRunExpiryDoesNotDependOnBlockedTasksOrFinish(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	old := time.Now().Add(-48 * time.Hour)
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, mode := range []string{"tree", "worktree"} {
		for _, state := range []string{run.StateCancelled, run.StateFinished, run.StatePaused, run.StateLive} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				folder := t.TempDir()
				s := &run.State{Folder: folder, Mode: mode, State: state, EndedAt: old, FinishPending: true, Tasks: []*run.TaskState{{State: run.TaskBlocked}}}
				if err := s.Save(); err != nil {
					t.Fatal(err)
				}
				want := state == run.StateCancelled || state == run.StateFinished || (mode == "worktree" && state == run.StatePaused)
				for _, recent := range []bool{false, true} {
					if recent {
						s.EndedAt = time.Now()
						if err := s.Save(); err != nil {
							t.Fatal(err)
						}
					}
					exists, expired, err := stateFileExpired(filepath.Join(folder, run.FileState), cutoff)
					if err != nil || !exists || expired != (want && !recent) {
						t.Fatalf("expiry: exists=%t expired=%t recent=%t: %v", exists, expired, recent, err)
					}
					expired, err = controlRecordExpired(filepath.Join(folder, "task"), cutoff)
					if err != nil || expired != (want && !recent) {
						t.Fatalf("control expiry: expired=%t recent=%t: %v", expired, recent, err)
					}
				}
			})
		}
	}
}
