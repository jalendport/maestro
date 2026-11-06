package worktree

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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
