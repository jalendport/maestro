package run

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/router"
)

func TestVendorTouchRequiresTreeMode(t *testing.T) {
	sup := worktreeSupervisor(t)
	runWrite(t, filepath.Join(sup.Env.Root, ".gitignore"), ".agents/\nnode_modules/\nvendor/\n")
	runGit(t, sup.Env.Root, "add", ".gitignore")
	runGit(t, sup.Env.Root, "commit", "-m", "Ignore vendor")
	runWrite(t, filepath.Join(sup.Env.Root, "vendor", "autoload.php"), "dependency")
	sup.State.Tasks[1].Touch = []string{"vendor/**"}
	chooseMode(sup.Env, sup.State)
	if sup.State.Mode != "tree" || sup.State.ModeReason != "task 2 touches vendor" {
		t.Fatalf("mode: %s (%s)", sup.State.Mode, sup.State.ModeReason)
	}
}

func TestReadTaskSeesMergedRunBranchAndRemovesWorktree(t *testing.T) {
	sup := worktreeSupervisor(t)
	runWrite(t, filepath.Join(sup.Env.Root, "node_modules", "pkg", "index.js"), "dependency")
	first := sup.State.Tasks[0]
	b := &brief.Brief{Fields: brief.Fields{Title: "Review the change", Access: brief.AccessRead, BlockedBy: []string{first.ID}}, Files: []string{"first.txt"}, Goal: "Review the merged change.", Done: "The merged change is read."}
	b.Path = filepath.Join(sup.State.Folder, "review", "source.md")
	if err := brief.Write(b, b.Path); err != nil {
		t.Fatal(err)
	}
	review := &TaskState{Number: 3, ID: "review", Title: b.Title, Access: brief.AccessRead, State: TaskWaiting, BriefPath: b.Path, Folder: filepath.Dir(b.Path), Files: b.Files, BlockedBy: b.BlockedBy, Route: first.Route}
	sup.State.Tasks = append(sup.State.Tasks, review)
	sup.pending[review] = pendingDispatch{brief: b, route: first.Route}
	sup.schedule()
	if review.State != TaskWaiting || len(sup.running) != 2 {
		t.Fatalf("review started before the edit: %+v", review)
	}
	for range 2 {
		takeResult(t, sup)
	}
	bin := filepath.Join(t.TempDir(), "read-lane")
	script := "#!/bin/sh\nread pointer\ntest -f first.txt || exit 1\ntest -L node_modules || exit 1\nprintf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"## Answer\\nRead the merged change.\\n\\nSTATUS: done\\n\"}}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	sup.Env.Reg = harness.New(map[string]string{"codex": bin})
	head := runGit(t, sup.Env.Root, "rev-parse", sup.State.RunBranch)
	sup.schedule()
	dir := review.Workdir
	if review.State != TaskRunning || dir == "" || dir == sup.State.Project {
		t.Fatalf("read Task has no Worktree: %+v", review)
	}
	if got := runGit(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatalf("read head: %s, want %s", got, head)
	}
	if branch := runGit(t, dir, "branch", "--show-current"); branch != "" {
		t.Fatalf("read Worktree is on a branch: %s", branch)
	}
	if got := runGit(t, dir, "show", "HEAD:first.txt"); got != "Task change" {
		t.Fatalf("merged edit is absent: %s", got)
	}
	if target, err := os.Readlink(filepath.Join(dir, "node_modules")); err != nil || target != filepath.Join(sup.Env.Root, "node_modules") {
		t.Fatalf("dependency link: %s, %v", target, err)
	}
	rec := takeResult(t, sup)
	if review.State != TaskDone || rec.Workdir != dir || !strings.Contains(strings.Join(rec.Permission.Flags, " "), "-s read-only") {
		t.Fatalf("read result: %+v, %+v", review, rec)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("read Worktree remains: %v", err)
	}
	if list := runGit(t, sup.Env.Root, "worktree", "list", "--porcelain"); strings.Contains(list, dir) {
		t.Fatalf("read Worktree remains registered: %s", list)
	}
}

func TestReadWorktreeRemovedAfterFailureOrCancel(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			sup := worktreeSupervisor(t)
			task := sup.State.Tasks[0]
			task.Access, task.Cancel = brief.AccessRead, cancelled
			if err := sup.taskWorkdir(task, sup.pending[task].brief); err != nil {
				t.Fatal(err)
			}
			dir := task.Workdir
			rec := &dispatch.Record{Attempt: 1, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, FailureClass: dispatch.FailBad}, Verification: dispatch.Verification{Gate: json.RawMessage(`{"cause":"none"}`)}}
			sup.handle(result{task: task, rec: rec})
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("read Worktree remains: %v", err)
			}
		})
	}
}

func TestCommitFailureRecordsBlockedDecision(t *testing.T) {
	sup := worktreeSupervisor(t)
	task := sup.State.Tasks[0]
	if err := sup.taskWorkdir(task, sup.pending[task].brief); err != nil {
		t.Fatal(err)
	}
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\necho 'Commit denied in test' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, sup.Env.Root, "config", "core.hooksPath", hooks)
	runWrite(t, filepath.Join(task.Workdir, "first.txt"), "verified change\n")
	rec := &dispatch.Record{Kind: "dispatch", Project: sup.Env.Root, Task: task.ID, Path: task.Folder, Attempt: 1, Route: task.Route,
		Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeVerified}, Verification: dispatch.Verification{Changed: []string{"first.txt"}}}
	sup.handle(result{task: task, rec: rec})
	if task.State != TaskBlocked || !strings.Contains(task.Error, "Commit denied in test") {
		t.Fatalf("Task: %+v", task)
	}
	saved, err := dispatch.LoadRecord(task.Folder)
	if err != nil || saved.Outcome.Decision != "blocked" || !strings.Contains(saved.Outcome.ErrorText, "Commit denied in test") {
		t.Fatalf("record: %+v, %v", saved, err)
	}
	rows := 0
	if err := ledger.Each(func(raw json.RawMessage) error {
		rows++
		var row dispatch.Record
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		if row.Outcome.Decision != "blocked" || row.Outcome.ErrorText != saved.Outcome.ErrorText {
			t.Fatalf("Ledger row: %s", raw)
		}
		return nil
	}); err != nil || rows != 1 {
		t.Fatalf("Ledger rows: %d, %v", rows, err)
	}
	for _, state := range []string{TaskBlocked, TaskFailed} {
		task.State = state
		if report := Report(sup.State); !strings.Contains(report, "Worktree: "+task.Workdir) {
			t.Fatalf("missing Worktree for %s: %s", state, report)
		}
	}
}

func TestApproveKeepsOneFolderPerTask(t *testing.T) {
	sup := worktreeSupervisor(t)
	sup.Env.Disc = &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{
		"codex": {Path: "/fixture/codex", Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}}},
	}}
	planPath := filepath.Join(t.TempDir(), "plan.md")
	writePlan := func(count int) {
		t.Helper()
		text := "---\ntitle: Test approval\nlane: codex-sol\n---\n"
		for i := range count {
			text += fmt.Sprintf("\n## Task %d: Change file %d\n\n### Goal\nChange the file.\n\n### Done\nThe file is changed.\n", i+1, i+1)
		}
		runWrite(t, planPath, text)
	}
	writePlan(3)
	s, err := Create(sup.Env, planPath, CreateOpts{NoCouncil: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{3, 2, 4} {
		s.State = StateHeld
		writePlan(count)
		firstID := s.Tasks[0].ID
		if err := Approve(sup.Env, s); err != nil {
			t.Fatal(err)
		}
		if len(s.Tasks) != count || s.Tasks[0].ID != firstID {
			t.Fatalf("approved Tasks: %+v", s.Tasks)
		}
		entries, err := os.ReadDir(s.Folder)
		if err != nil {
			t.Fatal(err)
		}
		folders := 0
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "control" {
				folders++
				if _, ok := s.Task(entry.Name()); !ok {
					t.Fatalf("orphan Task folder: %s", entry.Name())
				}
			}
		}
		if folders != count {
			t.Fatalf("Task folders: %d, want %d", folders, count)
		}
	}
}

func TestGrokWorktreeLaunchKeepsSandboxAndRouteEnforcement(t *testing.T) {
	for _, extraDir := range []bool{false, true} {
		t.Run(fmt.Sprint(extraDir), func(t *testing.T) {
			sup := worktreeSupervisor(t)
			bin := filepath.Join(t.TempDir(), "grok-fixture")
			script := "#!/bin/sh\nprintf 'Grok change\\n' > first.txt\nprintf '%s\\n' '{\"type\":\"text\",\"data\":\"## Summary\\nChange the file.\\n\\nSTATUS: done\\n\"}'\n"
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			sup.Env.Reg = harness.New(map[string]string{"grok": bin})
			sup.Env.Disc = &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{
				"grok": {Path: bin, Version: "1.0.50", Models: []harness.Model{{ID: "grok-4.7"}}},
			}}
			t.Setenv("GROK_HOME", filepath.Join(os.Getenv("HOME"), ".grok"))
			if result := adapter.Install("grok", os.Getenv("HOME"), "skill"); result.Err != nil {
				t.Fatal(result.Err)
			}
			task := sup.State.Tasks[0]
			b := sup.pending[task].brief
			if extraDir {
				b.Dirs = []string{filepath.Join(sup.Env.Root, "cache")}
			}
			route, err := dispatch.RouteBrief(sup.Env, b, router.Request{Lane: "grok"})
			if err != nil {
				t.Fatal(err)
			}
			sup.pending[task] = pendingDispatch{brief: b, route: route}
			if !sup.start(task) {
				t.Fatalf("Grok Task did not start: %+v", task)
			}
			rec := takeResult(t, sup)
			want := harness.Enforced
			if extraDir {
				want = harness.BestEffort
			}
			if task.State != TaskDone || route.Enforcement != want || rec.Permission.Enforcement != want || !rec.Outcome.ReportByText {
				t.Fatalf("Grok Route and launch: %+v, %+v", route, rec)
			}
			if flags := strings.Join(rec.Permission.Flags, " "); strings.Contains(flags, "--sandbox workspace") == extraDir {
				t.Fatalf("Grok sandbox flags: %s", flags)
			}
			text, err := os.ReadFile(filepath.Join(task.Folder, dispatch.FileBrief))
			if err != nil || !strings.Contains(string(text), "Your final message is your report.") {
				t.Fatalf("message Report instruction: %s, %v", text, err)
			}
			pointer, err := os.ReadFile(filepath.Join(task.Folder, "pointer.txt"))
			if err != nil || !strings.Contains(string(pointer), filepath.Join(task.Folder, dispatch.FileBrief)) {
				t.Fatalf("absolute Brief path: %s, %v", pointer, err)
			}
		})
	}
}
