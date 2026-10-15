package verify

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSnapshotChanged(t *testing.T) {
	root := snapshotRepo(t)
	s, err := Take(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Commit == "" || s.Taken.IsZero() || len(s.Dirty) != 0 {
		t.Fatalf("Take(%q) = %+v, want a clean snapshot with a commit and time", root, s)
	}
	if err := os.WriteFile(filepath.Join(root, "a-new.txt"), []byte("new\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "z-committed.txt"), []byte("modified\n"), 0644); err != nil {
		t.Fatal(err)
	}
	want := []string{"a-new.txt", "z-committed.txt"}
	if got := s.Changed(root); !reflect.DeepEqual(got, want) {
		t.Fatalf("Changed(%q) = %v, want %v", root, got, want)
	}
}

func TestSnapshotDoesNotRunConfiguredFSMonitor(t *testing.T) {
	root := snapshotRepo(t)
	marker := filepath.Join(t.TempDir(), "marker")
	t.Setenv("TEST_GIT_MARKER", marker)
	script := filepath.Join(t.TempDir(), "fsmonitor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'called\\n' > \"$TEST_GIT_MARKER\"\nprintf 'token\\000'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "config", "core.fsmonitor", script).CombinedOutput(); err != nil {
		t.Fatalf("git config: %v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", root, "status", "--porcelain").CombinedOutput(); err != nil {
		t.Fatalf("control status: %v %s", err, out)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatalf("configured fsmonitor did not run in the control: %v", err)
	}
	s, err := Take(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("configured fsmonitor ran during Take: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "made.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.Changed(root); !reflect.DeepEqual(got, []string{"made.txt"}) {
		t.Fatalf("Changed = %v", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("configured fsmonitor ran during Changed: %v", err)
	}
}

func TestSnapshotChangedIgnoresUntouchedUntrackedFile(t *testing.T) {
	root := snapshotRepo(t)
	path := "existing.txt"
	if err := os.WriteFile(filepath.Join(root, path), []byte("existing\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := Take(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Dirty[path]; !ok {
		t.Fatalf("Take(%q).Dirty = %v, want %q", root, s.Dirty, path)
	}
	if got := s.Changed(root); len(got) != 0 {
		t.Fatalf("Changed(%q) = %v, want no changed paths", root, got)
	}
}

func snapshotRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	if err := os.WriteFile(filepath.Join(root, "z-committed.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", "z-committed.txt")
	git("-c", "user.name=Snapshot Test", "-c", "user.email=snapshot@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "Initial file")
	return root
}

func TestTakeRequiresGitAndRepository(t *testing.T) {
	t.Run("missing git", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, err := Take(t.TempDir()); err == nil || !strings.Contains(err.Error(), "snapshot needs git") {
			t.Fatalf("Take error = %v", err)
		}
	})
	t.Run("not a repository", func(t *testing.T) {
		if _, err := Take(t.TempDir()); err == nil || !strings.Contains(err.Error(), "repository") {
			t.Fatalf("Take error = %v", err)
		}
	})
	t.Run("no commits", func(t *testing.T) {
		root := t.TempDir()
		if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
		snapshot, err := Take(root)
		if err != nil || snapshot.Commit != "" || snapshot.Taken.IsZero() {
			t.Fatalf("snapshot %+v, error %v", snapshot, err)
		}
	})
}

func TestSnapshotChangedIncludesCommittedPaths(t *testing.T) {
	root := snapshotRepo(t)
	s, err := Take(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "z-committed.txt"), []byte("attempt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-am", "Attempt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := []string{"dirty.txt", "z-committed.txt"}
	if got := s.Changed(root); !reflect.DeepEqual(got, want) {
		t.Fatalf("Changed = %v, want %v", got, want)
	}
	if got := ScopeViolations(s.Changed(root), []string{"dirty.txt"}, ""); !reflect.DeepEqual(got, []string{"z-committed.txt"}) {
		t.Fatalf("violations: %v", got)
	}
}

func TestSnapshotChangedIncludesDeletedDirtyPaths(t *testing.T) {
	root := snapshotRepo(t)
	for _, p := range []string{"untracked.txt", "z-committed.txt"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte("dirty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Take(root)
	if err != nil {
		t.Fatal(err)
	}
	for p := range s.Dirty {
		if err := os.Remove(filepath.Join(root, p)); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"untracked.txt", "z-committed.txt"}
	if got := s.Changed(root); !reflect.DeepEqual(got, want) {
		t.Fatalf("Changed = %v, want %v", got, want)
	}
}

func TestSnapshotChangedIncludesFirstCommit(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	s, err := Take(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "first.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "add", "first.txt").CombinedOutput(); err != nil {
		t.Fatalf("add: %v %s", err, out)
	}
	cmd := exec.Command("git", "-C", root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "First")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v %s", err, out)
	}
	if got := s.Changed(root); !reflect.DeepEqual(got, []string{"first.txt"}) {
		t.Fatalf("Changed = %v", got)
	}
}
