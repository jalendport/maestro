package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
)

func TestInheritedPathLeavesTouchGuardWhenReverted(t *testing.T) {
	for _, kind := range []string{"tracked", "committed", "dirty tracked", "dirty untracked", "new", "deleted", "no commit"} {
		for _, reverted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverted=%t", kind, reverted), func(t *testing.T) {
				root, folder := causeRoot(t), t.TempDir()
				git := func(args ...string) {
					t.Helper()
					if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
						t.Fatalf("git %v: %v %s", args, err, out)
					}
				}
				write := func(text string) {
					t.Helper()
					if err := os.WriteFile(filepath.Join(root, "prior.txt"), []byte(text), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if kind != "no commit" {
					write("committed before\n")
					git("add", "prior.txt")
					git("commit", "-qm", "Start")
				}
				before := "committed before\n"
				switch kind {
				case "dirty tracked", "dirty untracked":
					before = "dirty before\n"
					if kind == "dirty untracked" {
						git("rm", "prior.txt")
						git("commit", "-qm", "Remove")
					}
					write(before)
				case "new":
					git("rm", "prior.txt")
					git("commit", "-qm", "Remove")
				}
				_, err := takeBaseline(root, folder)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "deleted" {
					if err := os.Remove(filepath.Join(root, "prior.txt")); err != nil {
						t.Fatal(err)
					}
				} else {
					write("earlier attempt\n")
				}
				if kind == "committed" {
					git("add", "prior.txt")
					git("commit", "-qm", "Earlier attempt")
				}
				if err := (&Record{Path: folder, Attempt: 1, Verification: Verification{Changed: []string{"prior.txt"}}}).Save(); err != nil {
					t.Fatal(err)
				}
				archiveAttempt(folder, 1)
				snap, err := takeBaseline(root, folder)
				if err != nil {
					t.Fatal(err)
				}
				if err := snap.inherit(folder, 2); err != nil {
					t.Fatal(err)
				}
				if reverted {
					if kind == "new" || kind == "no commit" {
						if err := os.Remove(filepath.Join(root, "prior.txt")); err != nil {
							t.Fatal(err)
						}
					} else {
						write(before)
					}
				}
				rec := &Record{}
				recordChanges(&Task{Project: root, Access: brief.AccessEdit, Touch: []string{"allowed.txt"}}, rec, snap)
				want := []string{"prior.txt"}
				if reverted {
					want = []string{}
				}
				if !reflect.DeepEqual(snap.Changed(root), want) || len(rec.Verification.Changed) != len(want) || len(rec.Verification.TouchViolations) != len(want) {
					t.Fatalf("changed=%v verification=%+v want=%v", snap.Changed(root), rec.Verification, want)
				}
			})
		}
	}
}

func TestInheritCarriesEarlierAttemptsIntoTheBaseline(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	testGitEnv(t)
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
	if !second.Dirty["d.txt"].Equal(first.Dirty["d.txt"]) {
		t.Fatalf("inherited mtime = %v, want %v", second.Dirty["d.txt"], first.Dirty["d.txt"])
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
	if got := DiffFor(root, &Record{Path: folder, Verification: Verification{Changed: changed}}); got != diff {
		t.Fatalf("DiffFor = %q, want %q", got, diff)
	}
}

func TestAttemptDiffDoesNotRunConfiguredHelpers(t *testing.T) {
	root, folder := diffRepo(t), t.TempDir()
	path := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(path, []byte("dirty before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("dirty after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "marker")
	t.Setenv("TEST_GIT_MARKER", marker)
	script := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'called\\n' > \"$TEST_GIT_MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(t.TempDir(), "gitconfig")
	if out, err := exec.Command("git", "config", "--file", global, "diff.external", script).CombinedOutput(); err != nil {
		t.Fatalf("git config: %v %s", err, out)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	control := exec.Command("git", "-C", root, "diff", "--ext-diff", "HEAD")
	if out, err := control.CombinedOutput(); err != nil {
		t.Fatalf("control diff: %v %s", err, out)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatalf("configured helper did not run in the control: %v", err)
	}
	diff, err := snap.diff(root, []string{"tracked.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-dirty before") || !strings.Contains(diff, "+dirty after") {
		t.Fatalf("attempt diff = %s", diff)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("configured helper ran during attempt diff: %v", err)
	}
}

func TestInheritKeepsTheEmptyCommitOfAnUncommittedStart(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	testGitEnv(t)
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
	git("init", "-q")

	first, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	if first.Commit != "" {
		t.Fatalf("baseline commit = %q, want none", first.Commit)
	}
	if err := os.WriteFile(filepath.Join(root, "made.txt"), []byte("made\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "made.txt")
	git("commit", "-q", "-m", "first")
	changed := first.Changed(root)
	if !reflect.DeepEqual(changed, []string{"made.txt"}) {
		t.Fatalf("attempt 1 changed = %v", changed)
	}
	rec := &Record{Path: folder, Attempt: 1, Verification: Verification{Changed: changed}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	archiveAttempt(folder, 1)

	second, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.inherit(folder, 2); err != nil {
		t.Fatal(err)
	}
	if second.Commit != "" {
		t.Fatalf("inherited commit = %q, want the empty start", second.Commit)
	}
	if changed := second.Changed(root); !reflect.DeepEqual(changed, []string{"made.txt"}) {
		t.Fatalf("inherited changed = %v, want [made.txt]", changed)
	}
	diff, err := second.diff(root, []string{"made.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "new file: made.txt") {
		t.Fatalf("diff lacks the committed file:\n%s", diff)
	}
}
