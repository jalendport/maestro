package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/run"
)

func TestLandingCheckFixThroughCLI(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	root, git := worktreeConfigFixture(t)
	if err := os.Symlink(sh, filepath.Join(os.Getenv("PATH"), "sh")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TYPESAFE_API_KEY", "")
	bin := filepath.Join(t.TempDir(), "lane")
	script := "#!/bin/sh\n[ \"$1\" = exec ] || exit 0\nread pointer\ncase \"$(/bin/cat \"$MAESTRO_TASK_FOLDER/brief.source.md\")\" in\n*'Fix the combined failure'*) file=fixed.txt;;\n*'Change first'*) file=first.txt;;\n*) file=second.txt;;\nesac\nprintf 'fixture change\\n' > \"$file\"\nprintf '## Summary\\nChanged the fixture file.\\n\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorktreeConfig(t, config.UserFile(), fmt.Sprintf("[router]\nbackend = 'rules'\n[failure]\nkeep_awake = false\n[harness.codex]\nbin = %q\n", bin))
	command := "if [ -f first.txt ] && [ -f second.txt ] && [ ! -f fixed.txt ]; then printf 'combined failure\\n'; exit 1; fi"
	writeWorktreeConfig(t, config.ProjectFile(root), fmt.Sprintf("router = 'rules'\nchecks = [%q]\n", command))
	cache := &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{
		"codex": {Path: bin, Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}}},
	}}
	if err := cache.Save(); err != nil {
		t.Fatal(err)
	}
	bf := &budget.File{Accounts: map[string]*budget.Account{}}
	bf.Record("codex", []harness.Window{{Kind: "weekly", Used: .1, ResetsAt: time.Now().Add(time.Hour)}}, budget.SourceManual)
	if err := bf.Save(); err != nil {
		t.Fatal(err)
	}
	plan := "---\naccess: edit\ncategory: implement\nlane: codex-sol:medium\nlevel: some\ncommit: true\n---\n# Landing fixture\n"
	for _, name := range []string{"first", "second"} {
		plan += fmt.Sprintf("\n## Change %s\n- touch: ['/%s.txt']\n### Goal\nCreate %s.txt.\n### Done\nThe file exists.\n### Files\n- %s.txt\n", name, name, name, name)
	}
	planPath := filepath.Join(root, ".agents", "plan.md")
	writeWorktreeConfig(t, planPath, plan)
	restore := run.SetPollIntervalForTest(5 * time.Millisecond)
	t.Cleanup(restore)
	output, err := waitCommand(t, "run", planPath, "--no-plan-check", "--no-council", "--wait")
	if code, ok := proc.Code(err); !ok || code != 2 || !strings.Contains(output, "landing check failed") {
		t.Fatalf("landing did not pause: %s, %v", output, err)
	}
	folder := filepath.Join(config.RunsDir(root), "r-0001")
	s, err := run.Load(folder)
	if err != nil || s.State != run.StatePaused || !s.FinishPending || run.SupervisorAlive(s) {
		t.Fatalf("paused Run: %+v, %v", s, err)
	}
	if _, ok := run.LiveRunFor(root); ok {
		t.Fatal("paused Run kept its live index")
	}
	head := strings.TrimSpace(git("rev-parse", s.RunBranch))
	fixPath := filepath.Join(root, ".agents", "fix.md")
	fix := &brief.Brief{Fields: brief.Fields{Title: "Fix the combined failure", Access: brief.AccessEdit, Category: "implement", Lane: "codex-sol:medium", Level: "some", Touch: []string{"/fixed.txt"}}, Goal: "Fix the combined failure.", Done: "The checks pass."}
	if err := brief.Write(fix, fixPath); err != nil {
		t.Fatal(err)
	}
	if output, err := waitCommand(t, "add", fixPath); err != nil {
		t.Fatalf("add fix: %s, %v", output, err)
	}
	if output, err := waitCommand(t, "resume", s.Run, "--finish", "--wait"); err != nil {
		t.Fatalf("finish retry: %s, %v", output, err)
	}
	s, err = run.Load(folder)
	if err != nil || s.State != run.StateFinished || s.FinishPending || len(s.Tasks) != 3 || s.Tasks[2].State != run.TaskDone || s.LandingCheck.State != "passed" {
		t.Fatalf("fixed Run: %+v, %v", s, err)
	}
	if got := strings.TrimSpace(git("rev-list", "--count", head+"..HEAD")); got != "1" {
		t.Fatalf("fix commit did not land: %s", got)
	}
	if got := strings.TrimSpace(git("rev-parse", "HEAD^")); got != head {
		t.Fatalf("fix did not start at the merged head: %s; want %s", got, head)
	}
}
