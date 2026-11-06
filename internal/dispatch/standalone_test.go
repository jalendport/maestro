package dispatch

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/worktree"
)

func standaloneFixture(t *testing.T, commit bool, script string) (*Env, *Task, func(...string) string) {
	t.Helper()
	testGitEnv(t)
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("SOLO_PROCESS_ID", "")
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "master")
	writeStandalone(t, filepath.Join(root, ".gitignore"), ".agents/\nvendor/\n")
	writeStandalone(t, filepath.Join(root, "work.txt"), "base\n")
	git("add", ".")
	git("commit", "-m", "Start test repository")
	bin := filepath.Join(t.TempDir(), "lane")
	writeStandalone(t, bin, "#!/bin/sh\ncat >/dev/null\nprintf 'call\\n' >> \"$MAESTRO_TASK_FOLDER/calls\"\n"+script+"\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	d, _, err := config.ParseFile(roster.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	r := roster.Merge(d, &config.File{})
	r.Router.Backend = "rules"
	off := false
	r.Failure.KeepAwake = &off
	message := "Task {task}: {title}\n\n{summary}"
	r.Commit.Message = &message
	env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": bin}),
		Disc: &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{"codex": {
			Path: bin, Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}},
		}}}}
	b := &brief.Brief{Fields: brief.Fields{Title: "Change the file", Access: brief.AccessEdit,
		Touch: []string{"work.txt"}, Commit: &commit}, Goal: "Change the file.", Done: "The file has the change."}
	b.Path = filepath.Join(t.TempDir(), "source.md")
	if err := brief.Write(b, b.Path); err != nil {
		t.Fatal(err)
	}
	task, err := Prepare(env, b, router.Request{Lane: "codex-sol"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	task.MaxAttempt = 1
	return env, task, git
}

func writeStandalone(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

const standaloneChange = "printf 'Lane work\\n' > work.txt\nprintf '## Summary\\nChange the file.\\n\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\""

func TestStandaloneFinishUsesOldIdentity(t *testing.T) {
	env, task, git := standaloneFixture(t, true, standaloneChange)
	r, err := worktree.Open(env.Root, task.Task)
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Add(task.Task)
	if err != nil {
		t.Fatal(err)
	}
	task.Workdir = w.Dir
	outcome := Outcome{Outcome: OutcomeUnverified}
	task.Standalone = &Standalone{Outcome: &outcome}
	writeStandalone(t, filepath.Join(w.Dir, "work.txt"), "old Dispatch\n")
	writeStandalone(t, filepath.Join(task.Folder, FileReport), "## Summary\nChange the file.\n\nSTATUS: done\n")
	if err := task.Save(); err != nil {
		t.Fatal(err)
	}
	rec := &Record{Kind: "dispatch", Project: env.Root, Run: task.Run, Task: task.Task, Path: task.Folder,
		State: "ended", FinishPending: true, Outcome: outcome}
	rec.Verification.Changed = []string{"work.txt"}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	rec, err = FinishStandalone(env, task)
	if err != nil || rec.FinishPending || rec.RunBranch != r.Branch {
		t.Fatalf("Finish: %+v, %v", rec, err)
	}
	if data, _ := os.ReadFile(filepath.Join(env.Root, "work.txt")); string(data) != "old Dispatch\n" {
		t.Fatalf("old work lost: %q", data)
	}
	if got := git("branch", "--list", "maestro/*"); got != "" {
		t.Fatalf("branches remain: %s", got)
	}
}

func TestStandaloneHandFixAfterFailedCleanup(t *testing.T) {
	env, task, git := standaloneFixture(t, true, standaloneChange)
	env.Project = &config.Project{Checks: []string{"test ! -f user.txt || test -f fixed.txt"}}
	if err := openStandalone(env, task); err != nil {
		t.Fatal(err)
	}
	writeStandalone(t, filepath.Join(env.Root, "user.txt"), "user change\n")
	git("add", "user.txt")
	git("commit", "-m", "Change user file")
	rec, err := Execute(env, task)
	if err != nil || !rec.FinishPending {
		t.Fatalf("landing did not stop: %+v, %v", rec, err)
	}
	lock := filepath.Join(env.Root, ".git", "refs", "heads", "maestro", StandaloneRunID(task), "run.lock")
	writeStandalone(t, lock, "")
	env.Project.Checks = []string{"true"}
	rec, err = FinishStandalone(env, task)
	if err != nil || !rec.FinishPending || !strings.Contains(rec.Outcome.ErrorText, "Finish remove") {
		t.Fatalf("cleanup did not stop: %+v, %v", rec, err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	writeStandalone(t, filepath.Join(task.Workdir, "fixed.txt"), "hand fix\n")
	env.Project.Checks = []string{"test -f fixed.txt"}
	rec, err = FinishStandalone(env, task)
	if err != nil || rec.FinishPending || task.Standalone.LandingCheck.State != "passed" {
		t.Fatalf("Finish: %+v, %v", rec, err)
	}
	if data, _ := os.ReadFile(filepath.Join(env.Root, "fixed.txt")); string(data) != "hand fix\n" {
		t.Fatalf("hand fix lost: %q", data)
	}
	if got := git("log", "-1", "--format=%s"); got != "Task "+task.Task+": Change the file" {
		t.Fatalf("fix commit lost: %s", got)
	}
	if got := git("branch", "--list", "maestro/*"); got != "" {
		t.Fatalf("branches remain: %s", got)
	}
}

func TestStandaloneIdentityIncludesRun(t *testing.T) {
	env, first, git := standaloneFixture(t, true, standaloneChange)
	if err := openStandalone(env, first); err != nil {
		t.Fatal(err)
	}
	writeStandalone(t, filepath.Join(first.Workdir, "work.txt"), "kept work\n")
	second := *first
	second.Run, second.Workdir = "r-9999", ""
	second.Standalone = nil
	second.Folder = filepath.Join(config.RunsDir(env.Root), second.Run, second.Task)
	if err := os.MkdirAll(second.Folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := openStandalone(env, &second); err != nil {
		t.Fatal(err)
	}
	if first.Workdir == second.Workdir || StandaloneRunID(first) == StandaloneRunID(&second) {
		t.Fatal("Dispatches share a Worktree identity")
	}
	if got := git("-C", second.Workdir, "symbolic-ref", "--short", "HEAD"); got != "maestro/"+StandaloneRunID(&second)+"/"+second.Task {
		t.Fatalf("Task branch: %s", got)
	}
	second.Workdir = first.Workdir
	if err := openStandalone(env, &second); err == nil {
		t.Fatal("accepted another Dispatch's Worktree")
	}
	if data, _ := os.ReadFile(filepath.Join(first.Workdir, "work.txt")); string(data) != "kept work\n" {
		t.Fatalf("kept work changed: %q", data)
	}
}

func TestStandaloneFinishLandsHandFix(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			env, task, git := standaloneFixture(t, true, standaloneChange)
			env.Project = &config.Project{Checks: []string{"test ! -f user.txt || test -f fixed.txt"}}
			if err := openStandalone(env, task); err != nil {
				t.Fatal(err)
			}
			writeStandalone(t, filepath.Join(env.Root, "user.txt"), "user change\n")
			git("add", "user.txt")
			git("commit", "-m", "User change")
			rec, err := Execute(env, task)
			if err != nil || !rec.FinishPending {
				t.Fatalf("landing did not stop: %+v, %v", rec, err)
			}
			writeStandalone(t, filepath.Join(task.Workdir, "fixed.txt"), "hand fix\n")
			message := "Task " + task.Task + ": Change the file"
			if committed {
				message = "Fix the landing check"
				git("-C", task.Workdir, "add", "fixed.txt")
				git("-C", task.Workdir, "commit", "-m", message)
			}
			reloaded, err := LoadTask(task.Folder)
			if err != nil {
				t.Fatal(err)
			}
			rec, err = FinishStandalone(env, reloaded)
			if err != nil || rec.FinishPending || reloaded.Standalone.LandingCheck.State != "passed" {
				t.Fatalf("Finish: %+v, %v", rec, err)
			}
			if got := git("log", "-1", "--format=%s"); got != message {
				t.Fatalf("fix commit: %s", got)
			}
			if data, _ := os.ReadFile(filepath.Join(env.Root, "fixed.txt")); string(data) != "hand fix\n" {
				t.Fatalf("hand fix lost: %q", data)
			}
			if got := git("branch", "--list", "maestro/*"); got != "" {
				t.Fatalf("branches remain: %s", got)
			}
			calls, _ := os.ReadFile(filepath.Join(task.Folder, "calls"))
			if string(calls) != "call\n" {
				t.Fatalf("new Lane: %q", calls)
			}
		})
	}
}

func TestStandaloneLandingCheckSkipsOrChecksRebasedHead(t *testing.T) {
	for _, mode := range []string{"no checks", "same base", "moved base"} {
		t.Run(mode, func(t *testing.T) {
			env, task, git := standaloneFixture(t, true, standaloneChange)
			if mode != "no checks" {
				env.Project = &config.Project{Checks: []string{"test -f work.txt"}}
			}
			if err := openStandalone(env, task); err != nil {
				t.Fatal(err)
			}
			if mode == "moved base" {
				writeStandalone(t, filepath.Join(env.Root, "user.txt"), "user change\n")
				git("add", "user.txt")
				git("commit", "-m", "User change")
			}
			rec, err := Execute(env, task)
			if err != nil || rec.FinishPending || rec.Outcome.Outcome != OutcomeUnverified {
				t.Fatalf("Execute: %+v, %v", rec, err)
			}
			saved, err := LoadTask(task.Folder)
			if err != nil || saved.Standalone.LandingCheck == nil {
				t.Fatalf("saved landing check: %+v, %v", saved, err)
			}
			check := saved.Standalone.LandingCheck
			want := map[string]string{"no checks": "landing check: skipped (no project checks)", "same base": "landing check: skipped (one merged Task checked on the same base)", "moved base": "landing check: passed (1 checks, "}[mode]
			if !strings.Contains(check.Line(), want) {
				t.Fatalf("landing check: %+v", check)
			}
			if mode == "moved base" && check.Head != git("rev-parse", "HEAD") {
				t.Fatal("landing check did not test the rebased head")
			}
		})
	}
}

func TestStandaloneLandingFailureKeepsCommitsAndFinishRetries(t *testing.T) {
	env, task, git := standaloneFixture(t, true, standaloneChange)
	command := "if [ -f user.txt ] && [ ! -f fixed.txt ]; then printf 'combined failure\\nmore detail\\n'; exit 1; fi"
	env.Project = &config.Project{Checks: []string{command}}
	if err := openStandalone(env, task); err != nil {
		t.Fatal(err)
	}
	writeStandalone(t, filepath.Join(env.Root, "user.txt"), "user change\n")
	git("add", "user.txt")
	git("commit", "-m", "User change")
	userHead := git("rev-parse", "HEAD")
	rec, err := Execute(env, task)
	want := "landing check failed: " + command + ": combined failure"
	if err != nil || rec.Outcome.Outcome != OutcomeBlocked || !rec.FinishPending || !strings.Contains(rec.Outcome.ErrorText, want) {
		t.Fatalf("Execute did not block: %+v, %v", rec, err)
	}
	if len(rec.Verification.Checks) != 1 || !rec.Verification.Checks[0].OK || git("rev-parse", "HEAD") != userHead || git("rev-list", "--count", "master.."+rec.RunBranch) != "1" {
		t.Fatalf("Task checks or commits: %+v", rec)
	}
	r, err := worktree.Open(env.Root, StandaloneRunID(task))
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Add("fix")
	if err != nil {
		t.Fatal(err)
	}
	writeStandalone(t, filepath.Join(w.Dir, "fixed.txt"), "fixed\n")
	if _, err := w.Commit("Fix combined failure", []string{"fixed.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Merge("fix"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadTask(task.Folder)
	if err != nil {
		t.Fatal(err)
	}
	rec, err = FinishStandalone(env, reloaded)
	if err != nil || rec.FinishPending || rec.Outcome.Outcome != OutcomeUnverified || reloaded.Standalone.LandingCheck.State != "passed" {
		t.Fatalf("Finish: %+v, %v", rec, err)
	}
	if git("rev-list", "--count", userHead+"..HEAD") != "2" {
		t.Fatal("Finish lost the Task or fix commit")
	}
	calls, err := os.ReadFile(filepath.Join(task.Folder, "calls"))
	if err != nil || string(calls) != "call\n" {
		t.Fatalf("Finish started another Lane: %q, %v", calls, err)
	}
}

func TestStandaloneLandsBothCommitSettings(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprint(commit), func(t *testing.T) {
			env, task, git := standaloneFixture(t, commit, standaloneChange)
			base := git("rev-parse", "HEAD")
			// A dirty path outside the Dispatch stays staged through either landing.
			writeStandalone(t, filepath.Join(env.Root, "user.txt"), "user work\n")
			git("add", "user.txt")
			rec, err := Execute(env, task)
			if err != nil || rec.Outcome.Outcome != OutcomeUnverified || rec.FinishPending {
				t.Fatalf("Execute: %+v, %v", rec, err)
			}
			if task.Workdir == "" || task.Workdir == task.Project {
				t.Fatalf("no Worktree: %+v", task)
			}
			if got, _ := os.ReadFile(filepath.Join(env.Root, "work.txt")); string(got) != "Lane work\n" {
				t.Fatalf("landing: %q", got)
			}
			if commit {
				if n := git("rev-list", "--count", base+"..HEAD"); n != "1" {
					t.Fatalf("commits: %s", n)
				}
				want := "Task " + task.Task + ": Change the file\n\nChange the file."
				if got := git("log", "-1", "--format=%B"); got != want {
					t.Fatalf("message: %q, want %q", got, want)
				}
			} else {
				if got := git("rev-parse", "HEAD"); got != base {
					t.Fatalf("commit:false moved HEAD: %s", got)
				}
				if got := git("diff", "--name-only"); got != "work.txt" {
					t.Fatalf("unstaged changes: %s", got)
				}
			}
			if got := git("diff", "--cached", "--name-only"); got != "user.txt" {
				t.Fatalf("user index changed: %s", got)
			}
			if got := git("branch", "--list", "maestro/*"); got != "" {
				t.Fatalf("branches remain: %s", got)
			}
			if _, err := os.Stat(task.Workdir); !os.IsNotExist(err) {
				t.Fatalf("Worktree remains: %v", err)
			}
		})
	}
}

func TestStandaloneKeepsMainCheckoutWhileLaneRuns(t *testing.T) {
	env, task, git := standaloneFixture(t, true, "printf 'Lane work\\n' > work.txt\nprintf ready > \"$MAESTRO_TASK_FOLDER/ready\"\nwhile [ ! -f \"$MAESTRO_TASK_FOLDER/release\" ]; do sleep 0.01; done\nprintf 'STATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"")
	base := git("rev-parse", "HEAD")
	type result struct {
		rec *Record
		err error
	}
	ended := make(chan result, 1)
	go func() { rec, err := Execute(env, task); ended <- result{rec, err} }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(task.Folder, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Lane did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, readErr := os.ReadFile(filepath.Join(env.Root, "work.txt"))
	head := git("rev-parse", "HEAD")
	dirty := git("status", "--porcelain")
	writeStandalone(t, filepath.Join(task.Folder, "release"), "")
	got := <-ended
	if readErr != nil || string(data) != "base\n" || head != base || dirty != "" {
		t.Fatalf("main checkout changed while Lane ran: %q, %s, %s, %v", data, head, dirty, readErr)
	}
	if got.err != nil || got.rec.Outcome.Outcome != OutcomeUnverified {
		t.Fatalf("Execute: %+v", got)
	}
}

func TestStandaloneMovedBranchAndLandingRetry(t *testing.T) {
	for _, mode := range []string{"moved", "overlap", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			env, task, git := standaloneFixture(t, true, standaloneChange)
			if err := openStandalone(env, task); err != nil {
				t.Fatal(err)
			}
			if mode == "moved" {
				writeStandalone(t, filepath.Join(env.Root, "user.txt"), "user commit\n")
				git("add", "user.txt")
				git("commit", "-m", "User change")
			} else {
				writeStandalone(t, filepath.Join(env.Root, "work.txt"), "user work\n")
				if mode == "conflict" {
					git("commit", "-am", "User change")
				}
			}
			rec, err := Execute(env, task)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "moved" {
				if rec.Outcome.Outcome != OutcomeUnverified || git("log", "-2", "--format=%s") != "Task "+task.Task+": Change the file\nUser change" {
					t.Fatalf("rebase: %+v", rec)
				}
				return
			}
			if rec.Outcome.Outcome != OutcomeBlocked || !rec.FinishPending || !strings.Contains(rec.Outcome.ErrorText, "work.txt") || !strings.Contains(rec.Outcome.ErrorText, "maestro dispatch --finish "+task.Task) {
				t.Fatalf("landing did not block: %+v", rec)
			}
			if _, err := os.Stat(task.Workdir); err != nil {
				t.Fatalf("Worktree lost: %v", err)
			}
			if mode == "overlap" {
				git("restore", "work.txt")
			} else {
				git("checkout", rec.RunBranch)
				if err := exec.Command("git", "-C", env.Root, "rebase", "master").Run(); err == nil {
					t.Fatal("expected a landing conflict to resolve")
				}
				writeStandalone(t, filepath.Join(env.Root, "work.txt"), "user work\nLane work\n")
				git("add", "work.txt")
				git("-c", "core.editor=true", "rebase", "--continue")
				git("checkout", "master")
			}
			reloaded, err := LoadTask(task.Folder)
			if err != nil {
				t.Fatal(err)
			}
			rec, err = FinishStandalone(env, reloaded)
			if err != nil || rec.FinishPending || rec.Outcome.Outcome != OutcomeUnverified {
				t.Fatalf("Finish: %+v, %v", rec, err)
			}
			calls, _ := os.ReadFile(filepath.Join(task.Folder, "calls"))
			if string(calls) != "call\n" {
				t.Fatalf("new Lane: %q", calls)
			}
			if got := git("branch", "--list", "maestro/*"); got != "" {
				t.Fatalf("branches: %s", got)
			}
		})
	}
}

func TestStandaloneTreeFallbacks(t *testing.T) {
	for _, mode := range []string{"global", "project", "history", "detached", "dependency"} {
		t.Run(mode, func(t *testing.T) {
			env, task, git := standaloneFixture(t, false, standaloneChange)
			want := "worktrees off"
			off := false
			switch mode {
			case "global":
				env.Roster.Run.Worktrees = &off
			case "project":
				env.Project = &config.Project{Worktrees: &off}
			case "history":
				git("checkout", "--orphan", "empty")
				want = "tree has no git history"
			case "detached":
				git("checkout", "--detach")
				want = "HEAD is not on a branch"
			case "dependency":
				if err := os.Mkdir(filepath.Join(env.Root, "vendor"), 0o755); err != nil {
					t.Fatal(err)
				}
				task.Touch = []string{"work.txt", "vendor/**"}
				want = "task 1 touches vendor"
			}
			if got := StandaloneModeLine(env, task); got != "worktrees: off ("+want+")" {
				t.Fatalf("mode: %s", got)
			}
			rec, err := Execute(env, task)
			if err != nil || rec.Outcome.Outcome != OutcomeUnverified || task.Workdir != "" {
				t.Fatalf("fallback: %+v, %v", rec, err)
			}
			if data, _ := os.ReadFile(filepath.Join(env.Root, "work.txt")); string(data) != "Lane work\n" {
				t.Fatalf("tree work: %q", data)
			}
		})
	}
}

func TestStandaloneContainerChecks(t *testing.T) {
	for _, check := range []string{"ddev exec vendor/bin/phpunit", "docker compose run --rm app test", "./vendor/bin/sail test", "go test ./..."} {
		for _, source := range []string{"project", "Task"} {
			t.Run(source+"/"+check, func(t *testing.T) {
				env, task, _ := standaloneFixture(t, false, standaloneChange)
				if source == "project" {
					env.Project = &config.Project{Checks: []string{check}}
				} else {
					task.Checks = []string{check}
				}
				want := ""
				if check != "go test ./..." {
					want = "checks run in a container bound to the project folder (" + strings.Fields(check)[0] + ")"
				}
				if reason := standaloneReason(env, task); reason != want {
					t.Fatalf("reason = %q, want %q", reason, want)
				}
				if err := openStandalone(env, task); err != nil {
					t.Fatal(err)
				}
				if (task.Workdir != "") != (want == "") || task.Standalone.ModeReason != want {
					t.Fatalf("mode: %+v", task)
				}
				if want != "" && !strings.Contains(StandaloneModeLine(env, task), want) {
					t.Fatal("mode line lost the reason")
				}
			})
		}
	}
}

func TestStandaloneKeepsStoppedWorktree(t *testing.T) {
	for _, mode := range []string{"failed", "blocked", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			script := "printf 'Lane work\\n' > work.txt\nprintf 'STATUS: partial\\n' > \"$MAESTRO_TASK_FOLDER/result.md\""
			if mode == "blocked" {
				script = "printf 'Lane work\\n' > work.txt\nprintf '## Blocked\\n- Missing input\\nSTATUS: blocked\\n' > \"$MAESTRO_TASK_FOLDER/result.md\""
			}
			env, task, git := standaloneFixture(t, true, script)
			if mode == "cancelled" {
				cancel := make(chan struct{})
				close(cancel)
				task.Cancel = cancel
			}
			rec, err := Execute(env, task)
			if err != nil {
				t.Fatal(err)
			}
			if rec.Outcome.Outcome == OutcomeVerified || rec.Outcome.Outcome == OutcomeUnverified || rec.FinishPending {
				t.Fatalf("stopped outcome: %+v", rec.Outcome)
			}
			if _, err := os.Stat(task.Workdir); err != nil {
				t.Fatalf("Worktree lost: %v", err)
			}
			if got := git("branch", "--list", "maestro/"+StandaloneRunID(task)+"/*"); !strings.Contains(got, "maestro/"+StandaloneRunID(task)+"/run") || !strings.Contains(got, "maestro/"+StandaloneRunID(task)+"/"+task.Task) {
				t.Fatalf("branches lost: %s", got)
			}
			if data, _ := os.ReadFile(filepath.Join(env.Root, "work.txt")); string(data) != "base\n" {
				t.Fatalf("main checkout changed: %q", data)
			}
		})
	}
}

func TestStandaloneVerifiedLanding(t *testing.T) {
	env, task, _ := standaloneFixture(t, false, standaloneChange)
	// The supervisor also accepts a Task prepared with explicit ids and no saved state.
	task.Standalone = nil
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"answers":{"satisfies":{"noul":1},"verdict":{"choice":"pass"},"risk":{"score":1,"legend":{"1":"low"}}}}`)
	}))
	defer server.Close()
	env.Roster.Router.Backend, env.Roster.Router.BaseURL = "jev", server.URL
	task.Checks = []string{"test -f work.txt"}
	rec, err := Execute(env, task)
	if err != nil || rec.Outcome.Outcome != OutcomeVerified || rec.FinishPending {
		t.Fatalf("verified landing: %+v, %v", rec, err)
	}
}

func TestStandaloneReadDoesNotUseWorktree(t *testing.T) {
	env, task, _ := standaloneFixture(t, false, "printf '## Answer\\nRead complete.\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"")
	b, err := brief.Parse(task.BriefPath)
	if err != nil {
		t.Fatal(err)
	}
	b.Access = brief.AccessRead
	if err := brief.Write(b, b.Path); err != nil {
		t.Fatal(err)
	}
	read, err := Prepare(env, b, router.Request{Lane: "codex-sol"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Execute(env, read)
	if err != nil || rec.Outcome.Outcome != OutcomeAnswered || read.Standalone != nil || rec.Workdir != "" {
		t.Fatalf("read changed: %+v, %v", rec, err)
	}
}
