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
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/verify"
)

func TestRunOnceTakesFreshBaseline(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	folder := t.TempDir()
	old, err := verify.Take(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.SaveTo(filepath.Join(folder, FileBaseline)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "earlier-task.txt"), []byte("earlier work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "lane")
	script := "#!/bin/sh\ncat >/dev/null\nprintf 'new work\\n' > \"$TEST_CHANGED\"\nprintf 'STATUS: done\\n' > \"$TEST_REPORT\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &roster.Roster{}
	r.Router.Backend = "rules"
	env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": bin})}
	task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: brief.AccessEdit,
		Route: &router.Route{Harness: "codex"}, WallLimit: "1m", IdleLimit: "1m"}
	t.Setenv("TEST_REPORT", filepath.Join(folder, FileReport))
	for i, path := range []string{"first.txt", "retry.txt", "escalation.txt"} {
		task.Touch = []string{path}
		t.Setenv("TEST_CHANGED", filepath.Join(root, path))
		rec := runOnce(env, task, &brief.Brief{Title: "Test attempt"}, i+1, nil)
		if rec.Outcome.Outcome != OutcomeUnverified {
			t.Fatalf("attempt %d: outcome %+v", i+1, rec.Outcome)
		}
		if got := rec.Verification.Changed; !reflect.DeepEqual(got, []string{path}) {
			t.Fatalf("attempt %d: changed %v, want only %s", i+1, got, path)
		}
		snap, err := verify.LoadSnapshot(filepath.Join(folder, FileBaseline))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := snap.Dirty["earlier-task.txt"]; !ok || !snap.Taken.After(old.Taken) {
			t.Fatalf("attempt %d did not replace the old baseline: %+v", i+1, snap)
		}
		old = snap
	}
}

func TestArchiveAttemptPreservesBaselineAndTabLog(t *testing.T) {
	folder := t.TempDir()
	for _, name := range []string{FileBaseline, "tab.log"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archiveAttempt(folder, 1)
	for _, name := range []string{FileBaseline, "tab.log"} {
		b, err := os.ReadFile(filepath.Join(folder, FileAttempts, "1", name))
		if err != nil || string(b) != name {
			t.Fatalf("archive %s: %q, %v", name, b, err)
		}
	}
}

func TestAttemptDiffUsesDirtyFileContentsAtStart(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	path := filepath.Join(root, "shared.txt")
	if err := os.WriteFile(path, []byte("earlier Task\nold line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	snap, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("earlier Task\nnew line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := snap.diff(root, []string{"shared.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-old line") || !strings.Contains(diff, "+new line") || strings.Contains(diff, "+earlier Task") {
		t.Fatalf("diff includes work before the attempt: %s", diff)
	}
	rec := &Record{Path: folder, Verification: Verification{Changed: []string{"shared.txt"}}}
	if got := DiffFor(root, rec); got != diff {
		t.Fatalf("saved baseline diff = %q, want %q", got, diff)
	}
}

func TestEditPreflightRequiresGitBeforeRouting(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			if missing {
				t.Setenv("PATH", t.TempDir())
			}
			env := &Env{Root: t.TempDir(), Roster: &roster.Roster{}}
			b := &brief.Brief{Access: brief.AccessEdit}
			if _, err := Prepare(env, b, router.Request{}, "", ""); err == nil || !strings.Contains(err.Error(), "snapshot needs git") {
				t.Fatalf("preflight error: %v", err)
			}
		})
	}
}

func TestReadDispatchWorksWithoutGitOrRepository(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root, folder := t.TempDir(), t.TempDir()
	bin := filepath.Join(t.TempDir(), "lane")
	script := "#!/bin/sh\nprintf '## Answer\\nanswer\\nSTATUS: done\\n' > \"$TEST_REPORT\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_REPORT", filepath.Join(folder, FileReport))
	env := &Env{Root: root, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{"codex": bin})}
	task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "read", Access: brief.AccessRead, Route: &router.Route{Harness: "codex"}, WallLimit: "1m", IdleLimit: "1m"}
	rec := runOnce(env, task, &brief.Brief{}, 1, nil)
	if rec.Outcome.Outcome != OutcomeAnswered {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
}
