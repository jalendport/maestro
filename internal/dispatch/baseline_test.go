package dispatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInheritCarriesEarlierAttemptsIntoTheBaseline(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	root, folder := t.TempDir(), t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("a.txt", "one\n")
	git("add", "a.txt")
	git("commit", "-q", "-m", "start")
	write("d.txt", "d0\n") // dirty before the Task starts

	// Attempt 1 changes a tracked file, a dirty file, and adds a file, then fails.
	first, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	write("a.txt", "two\n")
	write("d.txt", "d1\n")
	write("n.txt", "new\n")
	want := []string{"a.txt", "d.txt", "n.txt"}
	changed := first.Changed(root)
	if !reflect.DeepEqual(changed, want) {
		t.Fatalf("attempt 1 changed = %v, want %v", changed, want)
	}
	rec := &Record{Path: folder, Attempt: 1, Verification: Verification{Changed: changed}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	archiveAttempt(folder, 1)

	// Attempt 2 starts on the tree attempt 1 left and touches nothing itself.
	second, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	if changed := second.Changed(root); len(changed) != 0 {
		t.Fatalf("a fresh baseline must hide the earlier work; changed = %v", changed)
	}
	if err := second.inherit(folder, 2); err != nil {
		t.Fatal(err)
	}
	changed = second.Changed(root)
	if !reflect.DeepEqual(changed, want) {
		t.Fatalf("inherited changed = %v, want %v", changed, want)
	}
	diff, err := second.diff(root, changed)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"-one", "+two", "-d0", "+d1", "new file: n.txt"} {
		if !strings.Contains(diff, line) {
			t.Fatalf("diff lacks %q:\n%s", line, diff)
		}
	}
	// The saved baseline carries the same view for DiffFor.
	if got := DiffFor(root, &Record{Path: folder, Verification: Verification{Changed: changed}}); got != diff {
		t.Fatalf("DiffFor = %q, want %q", got, diff)
	}
}
