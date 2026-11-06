package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/hook"
	"github.com/jalendport/maestro/internal/run"
)

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
