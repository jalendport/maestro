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
