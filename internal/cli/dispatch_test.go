package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/worktree"
)

func TestWaitStandaloneLandingNeedsFinish(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[asJSON], func(t *testing.T) {
			rec, _ := waitFixture(t)
			rec.State, rec.FinishPending = "ended", true
			rec.Workdir = filepath.Join(t.TempDir(), "task")
			rec.Outcome = dispatch.Outcome{Status: "done", Outcome: dispatch.OutcomeBlocked,
				ErrorText: "landing blocked: uncommitted changes in work.txt; Run branch maestro/abcd/run"}
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			args := []string{"wait", rec.Task}
			if asJSON {
				args = append(args, "--json")
			}
			output, err := waitCommand(t, args...)
			if code, ok := proc.Code(err); !ok || code != 2 {
				t.Fatalf("wait: %s, %v", output, err)
			}
			for _, want := range []string{"work.txt", "maestro/abcd/run", rec.Workdir, "maestro dispatch --finish abcd"} {
				if !strings.Contains(output, want) {
					t.Fatalf("missing %q: %s", want, output)
				}
			}
		})
	}
}

func TestDispatchFinishRetriesLanding(t *testing.T) {
	root, git := worktreeConfigFixture(t)
	if err := (&discovery.Cache{RefreshedAt: time.Now()}).Save(); err != nil {
		t.Fatal(err)
	}
	r, err := worktree.Open(root, dispatch.StandaloneRunID(&dispatch.Task{Run: "r-0001", Task: "abcd"}))
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Add("abcd")
	if err != nil {
		t.Fatal(err)
	}
	writeWorktreeConfig(t, filepath.Join(w.Dir, "base.txt"), "Lane work\n")
	if _, err := w.Commit("Change the file", []string{"base.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Merge("abcd"); err != nil {
		t.Fatal(err)
	}
	w, err = r.Add("abcd")
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, ".agents", "maestro", "runs", "r-0001", "abcd")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	outcome := dispatch.Outcome{Outcome: dispatch.OutcomeVerified, Status: "done"}
	task := &dispatch.Task{Project: root, Workdir: w.Dir, Task: "abcd", Run: "r-0001", Folder: folder, Commit: true,
		Standalone: &dispatch.Standalone{Merged: true, Outcome: &outcome}}
	if err := task.Save(); err != nil {
		t.Fatal(err)
	}
	rec := &dispatch.Record{Kind: "dispatch", Project: root, Workdir: w.Dir, Task: "abcd", Run: "r-0001", Path: folder, State: "ended", FinishPending: true,
		Route: &router.Route{}, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeBlocked}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	writeWorktreeConfig(t, filepath.Join(root, "base.txt"), "user work\n")
	output, err := waitCommand(t, "dispatch", "--finish", task.Task)
	if code, ok := proc.Code(err); !ok || code != 2 || !strings.Contains(output, "base.txt") {
		t.Fatalf("finish did not block: %s, %v", output, err)
	}
	git("restore", "base.txt")
	output, err = waitCommand(t, "dispatch", "--finish", task.Task)
	if err != nil {
		t.Fatalf("finish: %s, %v", output, err)
	}
	if got := strings.TrimSpace(git("log", "-1", "--format=%s")); got != "Change the file" {
		t.Fatalf("landing: %s", got)
	}
	if got := strings.TrimSpace(git("branch", "--list", "maestro/*")); got != "" {
		t.Fatalf("branches remain: %s", got)
	}
	rec, err = dispatch.LoadRecord(folder)
	if err != nil || rec.FinishPending || rec.Outcome.Outcome != dispatch.OutcomeVerified {
		t.Fatalf("Record: %+v, %v", rec, err)
	}
}

func TestDispatchPrintsTreeFallback(t *testing.T) {
	root, _ := worktreeConfigFixture(t)
	bin := filepath.Join(t.TempDir(), "lane")
	writeWorktreeConfig(t, bin, "#!/bin/sh\nprintf 'Lane work\\n' > base.txt\nprintf 'STATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorktreeConfig(t, config.UserFile(), "[router]\nbackend = \"rules\"\n[run]\nworktrees = false\n[failure]\nkeep_awake = false\n[harness.codex]\nbin = \""+bin+"\"\n")
	cache := &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{"codex": {
		Path: bin, Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}},
	}}}
	if err := cache.Save(); err != nil {
		t.Fatal(err)
	}
	briefPath := filepath.Join(t.TempDir(), "brief.md")
	writeWorktreeConfig(t, briefPath, "---\ntitle: Change the file\naccess: edit\ntouch: [base.txt]\n---\n## Goal\nChange the file.\n## Done\nThe file has the change.\n")
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	var commandErr error
	func() {
		previous := os.Stderr
		os.Stderr = stderr
		defer func() { os.Stderr = previous }()
		_, commandErr = waitCommand(t, "dispatch", briefPath, "--lane", "codex-sol", "--wait")
	}()
	output, err := os.ReadFile(stderr.Name())
	if commandErr != nil || err != nil || !strings.Contains(string(output), "worktrees: off (worktrees off)") {
		t.Fatalf("fallback output: %s, %v, %v", output, commandErr, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "base.txt"))
	if err != nil || string(data) != "Lane work\n" {
		t.Fatalf("tree work: %q, %v", data, err)
	}
}
