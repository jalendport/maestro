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
				want = "Task touches vendor"
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
			if got := git("branch", "--list", "maestro/"+task.Task+"/*"); !strings.Contains(got, "maestro/"+task.Task+"/run") || !strings.Contains(got, "maestro/"+task.Task+"/"+task.Task) {
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
