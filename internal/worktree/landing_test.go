package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLandingWorktreeIsDetachedLinkedAndCleaned(t *testing.T) {
	root := repository(t)
	write(t, root, ".gitignore", "node_modules/\n")
	tgit(t, root, "add", ".gitignore")
	tgit(t, root, "commit", "-m", "Ignore dependencies")
	write(t, root, "node_modules/package/index.js", "dependency\n")
	r := openRun(t, root)
	w := addTask(t, r, "a")
	commitTask(t, w, "change.txt", "Run change\n")
	mergeTask(t, r, "a")
	dir, cleanup, err := r.LandingWorktree()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if filepath.Base(dir) != "landing" || tgit(t, dir, "branch", "--show-current") != "" || tgit(t, dir, "rev-parse", "HEAD") != tgit(t, root, "rev-parse", r.Branch) {
		t.Fatal("landing Worktree is not detached at the Run branch head")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "node_modules/package/index.js")); err != nil || string(data) != "dependency\n" {
		t.Fatalf("dependency link: %q, %v", data, err)
	}
	write(t, dir, "change.txt", "left by a stopped check\n")
	dir, cleanupAgain, err := r.LandingWorktree()
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "change.txt")); err != nil || string(data) != "Run change\n" {
		t.Fatalf("stopped check was not cleared: %q, %v", data, err)
	}
	cleanupAgain()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("landing Worktree remains: %v", err)
	}
}

func TestFinishCheckedRunsAfterRebaseAndKeepsFailedHead(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	w := addTask(t, r, "a")
	commitTask(t, w, "change.txt", "Run change\n")
	mergeTask(t, r, "a")
	write(t, root, "user.txt", "user change\n")
	tgit(t, root, "add", "user.txt")
	tgit(t, root, "commit", "-m", "User change")
	current := tgit(t, root, "rev-parse", "HEAD")
	failure := errors.New("landing check failed: test: failure")
	err := r.FinishChecked(true, func() error {
		dir, cleanup, err := r.LandingWorktree()
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		if _, err := os.Stat(filepath.Join(dir, "user.txt")); err != nil {
			t.Fatalf("check ran before the landing rebase: %v", err)
		}
		return failure
	})
	if !errors.Is(err, failure) || tgit(t, root, "rev-parse", "HEAD") != current || tgit(t, root, "rev-list", "--count", "master.."+r.Branch) != "1" {
		t.Fatalf("failed landing changed the user branch or lost commits: %v", err)
	}
	if err := r.FinishChecked(true, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSavedRunOnDetachedHead(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	tgit(t, root, "checkout", "--detach")
	saved, err := Open(root, r.ID)
	if err != nil || saved.UserBranch != r.UserBranch || saved.Base != r.Base {
		t.Fatalf("saved Run: %+v, %v", saved, err)
	}
	if _, err := Open(root, "r-new"); err == nil {
		t.Fatal("new Run accepted a detached HEAD")
	}
}

func TestFinishKeepsUnrelatedDirtyWork(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "squash", true: "commit"}[commit], func(t *testing.T) {
			root := repository(t)
			r := openRun(t, root)
			w := addTask(t, r, "a")
			commitTask(t, w, "change.txt", "Run change\n")
			mergeTask(t, r, "a")
			write(t, root, "base.txt", "staged user work\n")
			tgit(t, root, "add", "base.txt")
			write(t, root, "base.txt", "unstaged user work\n")
			write(t, root, "delete.txt", "unstaged change\n")
			write(t, root, "untracked.txt", "untracked work\n")
			index := tgit(t, root, "diff", "--cached", "--binary")
			if err := r.Finish(commit); err != nil {
				t.Fatal(err)
			}
			if got := tgit(t, root, "diff", "--cached", "--binary"); got != index {
				t.Fatalf("user index changed:\n%s", got)
			}
			for path, want := range map[string]string{"base.txt": "unstaged user work\n", "delete.txt": "unstaged change\n", "untracked.txt": "untracked work\n", "change.txt": "Run change\n"} {
				data, err := os.ReadFile(filepath.Join(root, path))
				if err != nil || string(data) != want {
					t.Fatalf("%s: %q, %v", path, data, err)
				}
			}
		})
	}
}

func TestFinishBlocksOverlappingDirtyPaths(t *testing.T) {
	for _, commit := range []bool{false, true} {
		for _, staged := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "squash", true: "commit"}[commit], map[bool]string{false: "unstaged", true: "staged"}[staged]}, "/"), func(t *testing.T) {
				root := repository(t)
				r := openRun(t, root)
				w := addTask(t, r, "a")
				commitTask(t, w, "base.txt", "Run change\n")
				merged := mergeTask(t, r, "a").Head
				write(t, root, "base.txt", "user change\n")
				if staged {
					tgit(t, root, "add", "base.txt")
				}
				index := tgit(t, root, "diff", "--cached", "--binary")
				tree := tgit(t, root, "diff", "--binary")
				err := r.Finish(commit)
				want := "landing blocked: uncommitted changes in base.txt overlap the Run; commit or stash them, then maestro resume " + r.ID + " --finish"
				if err == nil || err.Error() != want {
					t.Fatalf("landing error: %v", err)
				}
				if tgit(t, root, "diff", "--cached", "--binary") != index || tgit(t, root, "diff", "--binary") != tree || tgit(t, root, "rev-parse", "HEAD") != r.Base || tgit(t, root, "rev-parse", r.Branch) != merged {
					t.Fatal("blocked landing changed the tree or branches")
				}
			})
		}
	}
}

func TestLandingRebaseConflictKeepsRunBranch(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	w := addTask(t, r, "a")
	commitTask(t, w, "base.txt", "Run change\n")
	merged := mergeTask(t, r, "a").Head
	write(t, root, "base.txt", "user change\n")
	tgit(t, root, "add", "base.txt")
	tgit(t, root, "commit", "-m", "Change user file")
	current := tgit(t, root, "rev-parse", "HEAD")
	err := r.Finish(true)
	if err == nil || !strings.Contains(err.Error(), "landing conflict in base.txt; Run branch "+r.Branch) || !strings.Contains(err.Error(), "maestro resume "+r.ID+" --finish") {
		t.Fatalf("landing conflict: %v", err)
	}
	if tgit(t, root, "rev-parse", r.Branch) != merged || tgit(t, root, "rev-parse", "HEAD") != current {
		t.Fatal("conflict changed the Run or user branch")
	}
	if paths, err := Dirty(root); err != nil || len(paths) != 0 {
		t.Fatalf("user tree: %v, %v", paths, err)
	}
	if list := tgit(t, root, "worktree", "list", "--porcelain"); strings.Contains(list, "landing") {
		t.Fatalf("landing Worktree remains: %s", list)
	}
}

func TestFinishResumesSquashBeforeResetMarker(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	w := addTask(t, r, "a")
	commitTask(t, w, "change.txt", "Run change\n")
	mergeTask(t, r, "a")
	write(t, root, "base.txt", "user stage\n")
	tgit(t, root, "add", "base.txt")
	index := tgit(t, root, "diff", "--cached", "--binary")
	stagePath := filepath.Join(r.dir, "finish")
	t.Setenv("MAESTRO_TEST_FINISH", stagePath)
	hooks := t.TempDir()
	tgit(t, root, "config", "core.hooksPath", hooks)
	if err := os.WriteFile(filepath.Join(hooks, "post-merge"), []byte("#!/bin/sh\nmv \"$MAESTRO_TEST_FINISH\" \"$MAESTRO_TEST_FINISH.saved\"\nmkdir \"$MAESTRO_TEST_FINISH\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.Finish(false); err == nil || !strings.Contains(err.Error(), "Finish record squash") {
		t.Fatalf("marker write did not fail: %v", err)
	}
	stage, err := os.ReadFile(stagePath + ".saved")
	if err != nil || string(stage) != "squash" {
		t.Fatalf("squash start was not recorded: %q, %v", stage, err)
	}
	if err := os.Remove(stagePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stagePath+".saved", stagePath); err != nil {
		t.Fatal(err)
	}
	saved, err := Open(root, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := saved.Finish(false); err != nil {
		t.Fatal(err)
	}
	if err := saved.Finish(false); err != nil {
		t.Fatal(err)
	}
	if got := tgit(t, root, "diff", "--cached", "--binary"); got != index {
		t.Fatalf("user index changed: %s", got)
	}
	if paths, err := Dirty(root); err != nil || !reflect.DeepEqual(paths, []string{"base.txt", "change.txt"}) {
		t.Fatalf("landed paths: %v, %v", paths, err)
	}
	if got := tgit(t, root, "rev-parse", "HEAD"); got != r.Base {
		t.Fatalf("squash made a commit: %s", got)
	}
}

func TestTwoRunsLandTheirOwnCommits(t *testing.T) {
	root := repository(t)
	first := openRun(t, root)
	second, err := Open(root, "r-second")
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range []Run{first, second} {
		w := addTask(t, r, "a")
		commitTask(t, w, r.ID+".txt", "Run change\n")
		mergeTask(t, r, "a")
		if i == 0 && first.Base != second.Base {
			t.Fatal("Runs did not start at the same base")
		}
	}
	if err := first.Finish(true); err != nil {
		t.Fatal(err)
	}
	if err := second.Finish(true); err != nil {
		t.Fatal(err)
	}
	if got := tgit(t, root, "rev-list", "--count", first.Base+"..HEAD"); got != "2" {
		t.Fatalf("landed commits: %s", got)
	}
	for _, r := range []Run{first, second} {
		if _, err := os.Stat(filepath.Join(root, r.ID+".txt")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFinishResumesResetAfterPathsWereUnstaged(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	w := addTask(t, r, "a")
	commitTask(t, w, "change[1].txt", "Run addition\n")
	commitTask(t, w, "base.txt", "Run change\n")
	mergeTask(t, r, "a")
	write(t, root, "delete.txt", "staged user work\n")
	tgit(t, root, "add", "delete.txt")
	index := tgit(t, root, "diff", "--cached", "--binary")
	tgit(t, root, "merge", "--squash", r.Branch)
	write(t, r.dir, "finish", "reset")
	tgit(t, root, "restore", "--staged", "--", ":(literal)change[1].txt", "base.txt")
	if got := tgit(t, root, "ls-files", "--", ":(literal)change[1].txt"); got != "" {
		t.Fatalf("Run addition is still tracked: %s", got)
	}
	saved, err := Open(root, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := saved.Finish(false); err != nil {
		t.Fatal(err)
	}
	if err := saved.Finish(false); err != nil {
		t.Fatal(err)
	}
	if got := tgit(t, root, "diff", "--cached", "--binary"); got != index {
		t.Fatalf("user index changed: %s", got)
	}
	for path, want := range map[string]string{"change[1].txt": "Run addition\n", "base.txt": "Run change\n", "delete.txt": "staged user work\n"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(data) != want {
			t.Fatalf("%s: %q, %v", path, data, err)
		}
	}
	if got := tgit(t, root, "rev-parse", "HEAD"); got != r.Base {
		t.Fatalf("squash made a commit: %s", got)
	}
	if _, err := os.Stat(r.dir); !os.IsNotExist(err) {
		t.Fatalf("Run folder remains: %v", err)
	}
	if _, err := head(root, r.Branch); err == nil {
		t.Fatal("Run branch remains")
	}
}

func TestFinishRemovesLeftoverLandingWorktree(t *testing.T) {
	for _, rebasing := range []bool{false, true} {
		t.Run(map[bool]string{false: "before rebase", true: "during rebase"}[rebasing], func(t *testing.T) {
			root := repository(t)
			r := openRun(t, root)
			w := addTask(t, r, "a")
			commitTask(t, w, "base.txt", "Run change\n")
			mergeTask(t, r, "a")
			write(t, root, "base.txt", "user change\n")
			tgit(t, root, "add", "base.txt")
			tgit(t, root, "commit", "-m", "Change user file")
			dir := filepath.Join(r.dir, "landing")
			tgit(t, root, "worktree", "add", dir, r.Branch)
			if rebasing {
				if _, err := git(root, dir, "rebase", "--onto", "master", r.Base); err == nil {
					t.Fatal("landing rebase did not stop at a conflict")
				}
				if !(Worktree{Root: root, Dir: dir}).Rebasing() {
					t.Fatal("landing rebase is not in progress")
				}
			}
			write(t, root, "base.txt", "base\n")
			tgit(t, root, "add", "base.txt")
			tgit(t, root, "commit", "-m", "Restore user file")
			current := tgit(t, root, "rev-parse", "HEAD")
			saved, err := Open(root, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := saved.Finish(true); err != nil {
				t.Fatal(err)
			}
			if got := tgit(t, root, "rev-list", "--count", current+"..HEAD"); got != "1" {
				t.Fatalf("landed commits: %s", got)
			}
			if data, err := os.ReadFile(filepath.Join(root, "base.txt")); err != nil || string(data) != "Run change\n" {
				t.Fatalf("Run change did not land: %q, %v", data, err)
			}
			if list := tgit(t, root, "worktree", "list", "--porcelain"); strings.Contains(list, "landing") {
				t.Fatalf("landing Worktree remains: %s", list)
			}
			if paths, err := Dirty(root); err != nil || len(paths) != 0 {
				t.Fatalf("user tree: %v, %v", paths, err)
			}
		})
	}
}

func TestLandingRetryRebasesOnlyRunCommits(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	w := addTask(t, r, "a")
	commitTask(t, w, "change.txt", "Run change\n")
	mergeTask(t, r, "a")
	write(t, root, "user.txt", "first user commit\n")
	tgit(t, root, "add", "user.txt")
	tgit(t, root, "commit", "-m", "Add first user change")
	write(t, root, "change.txt", "dirty user work\n")
	if err := r.Finish(true); err == nil || !strings.Contains(err.Error(), "landing blocked") {
		t.Fatalf("first landing: %v", err)
	}
	write(t, root, "user.txt", "second user commit\n")
	tgit(t, root, "add", "user.txt")
	tgit(t, root, "commit", "-m", "Add second user change")
	current := tgit(t, root, "rev-parse", "HEAD")
	if err := os.Remove(filepath.Join(root, "change.txt")); err != nil {
		t.Fatal(err)
	}
	if err := r.Finish(true); err != nil {
		t.Fatal(err)
	}
	if got := tgit(t, root, "rev-list", "--count", current+"..HEAD"); got != "1" {
		t.Fatalf("retry replayed user commits: %s", got)
	}
}
