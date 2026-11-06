package run

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/worktree"
)

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func runWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func worktreeSupervisor(t *testing.T) *Supervisor {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "user@example.test")
	t.Setenv("GIT_COMMITTER_NAME", "Test User")
	t.Setenv("GIT_COMMITTER_EMAIL", "user@example.test")
	t.Setenv("GIT_EDITOR", "true")
	t.Setenv("TYPESAFE_API_KEY", "")
	root := t.TempDir()
	runGit(t, root, "init", "-b", "master")
	runWrite(t, filepath.Join(root, ".gitignore"), ".agents/\nnode_modules/\n")
	runWrite(t, filepath.Join(root, "base.txt"), "base\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "Start test repository")
	r, err := roster.Load()
	if err != nil {
		t.Fatal(err)
	}
	off := false
	r.Failure.KeepAwake = &off
	r.Router.Backend = "rules"
	bin := filepath.Join(t.TempDir(), "lane")
	script := "#!/bin/sh\nread pointer\nprintf 'Task change\\n' > \"$MAESTRO_TASK.txt\"\nprintf '## Summary\\nChange the Task file.\\n\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &dispatch.Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": bin}), RetryDelay: time.Millisecond}
	s := saveRun(t, env)
	sup := &Supervisor{Env: env, State: s, running: map[string]chan struct{}{}, results: make(chan result, 8), pending: map[*TaskState]pendingDispatch{}, errorTexts: map[string]int{}}
	for i, id := range []string{"first", "second"} {
		b := &brief.Brief{Fields: brief.Fields{Title: "Change " + id, Access: brief.AccessEdit, Touch: []string{"/" + id + ".txt"}}, Goal: "Change the Task file.", Done: "The Task file exists."}
		b.Path = filepath.Join(s.Folder, id, "source.md")
		if err := brief.Write(b, b.Path); err != nil {
			t.Fatal(err)
		}
		route := &router.Route{Family: r.Families["codex-sol"], FamilyID: "codex-sol", Harness: "codex", Model: "gpt-6.1-sol", Tier: roster.TierDaily}
		task := &TaskState{Number: i + 1, ID: id, Title: b.Title, State: TaskWaiting, Access: brief.AccessEdit, BriefPath: b.Path, Folder: filepath.Dir(b.Path), Files: []string{id + ".txt"}, Touch: b.Touch, Route: route}
		s.Tasks = append(s.Tasks, task)
		sup.pending[task] = pendingDispatch{brief: b, route: route}
	}
	chooseMode(env, s)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = worktree.Remove(root, s.Run) })
	t.Cleanup(func() {
		for _, cancel := range sup.running {
			close(cancel)
		}
		for range len(sup.running) {
			select {
			case <-sup.results:
			case <-time.After(5 * time.Second):
			}
		}
	})
	return sup
}

func takeResult(t *testing.T, sup *Supervisor) *dispatch.Record {
	t.Helper()
	select {
	case result := <-sup.results:
		if result.err != nil {
			t.Fatal(result.err)
		}
		sup.handle(result)
		return result.rec
	case <-time.After(10 * time.Second):
		t.Fatal("Task did not finish")
		return nil
	}
}

func TestWorktreeModeReasons(t *testing.T) {
	for _, reason := range []string{"", "tree has uncommitted changes", "worktrees off", "one edit task", "project off", "project cannot enable", "HEAD is not on a branch"} {
		t.Run(reason, func(t *testing.T) {
			sup := worktreeSupervisor(t)
			off, on := false, true
			want := reason
			switch reason {
			case "HEAD is not on a branch":
				runGit(t, sup.Env.Root, "checkout", "--detach")
			case "tree has uncommitted changes":
				runWrite(t, filepath.Join(sup.Env.Root, "dirty.txt"), "dirty")
				want = ""
			case "worktrees off":
				sup.Env.Roster.Run.Worktrees = &off
			case "one edit task":
				sup.State.Tasks = sup.State.Tasks[:1]
				want = ""
			case "project off":
				sup.Env.Project = &config.Project{Worktrees: &off}
				want = "worktrees off"
			case "project cannot enable":
				sup.Env.Project = &config.Project{Worktrees: &on}
				sup.Env.Roster.Run.Worktrees = &off
				want = "worktrees off"
			}
			chooseMode(sup.Env, sup.State)
			if sup.State.ModeReason != want || (sup.State.Mode == "worktree") != (want == "") {
				t.Fatalf("mode: %s (%s)", sup.State.Mode, sup.State.ModeReason)
			}
			if err := sup.State.Save(); err != nil {
				t.Fatal(err)
			}
			saved, err := Load(sup.State.Folder)
			if err != nil || saved.ModeReason != want || !strings.Contains(Status(saved), ModeLine(saved)) || !strings.Contains(Report(saved), ModeLine(saved)) {
				t.Fatalf("saved mode: %+v, %v", saved, err)
			}
		})
	}
}

func TestContainerCheckMode(t *testing.T) {
	for _, check := range []string{"ddev exec vendor/bin/phpunit", "docker compose run --rm app test", "./vendor/bin/sail test", "go test ./..."} {
		for _, source := range []string{"project", "Task"} {
			t.Run(source+"/"+check, func(t *testing.T) {
				sup := worktreeSupervisor(t)
				if source == "project" {
					sup.Env.Project = &config.Project{Checks: []string{check}}
				} else {
					b, err := brief.Parse(sup.State.Tasks[1].BriefPath)
					if err != nil {
						t.Fatal(err)
					}
					b.Checks = []string{check}
					if err := brief.Write(b, b.Path); err != nil {
						t.Fatal(err)
					}
				}
				chooseMode(sup.Env, sup.State)
				if check == "go test ./..." {
					if sup.State.Mode != "worktree" || sup.State.ModeReason != "" {
						t.Fatalf("Go mode: %s, %s", sup.State.Mode, sup.State.ModeReason)
					}
					return
				}
				want := "checks run in a container bound to the project folder (" + strings.Fields(check)[0] + ")"
				if sup.State.Mode != "tree" || sup.State.ModeReason != want || !strings.Contains(ModeLine(sup.State), want) {
					t.Fatalf("container mode: %s, %s", sup.State.Mode, sup.State.ModeReason)
				}
			})
		}
	}
}

func TestDependencyFolderMode(t *testing.T) {
	for _, tc := range []struct {
		name, access, reason string
		touch                []string
	}{
		{name: "no touch", access: brief.AccessEdit},
		{name: "general pattern", access: brief.AccessEdit, touch: []string{"**"}},
		{name: "explicit folder", access: brief.AccessEdit, touch: []string{"node_modules/**"}, reason: "task 2 touches node_modules"},
		{name: "read Task", access: brief.AccessRead, touch: []string{"node_modules/**"}, reason: "task 2 touches node_modules"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sup := worktreeSupervisor(t)
			runWrite(t, filepath.Join(sup.Env.Root, "node_modules", "pkg", "index.js"), "dependency")
			task := sup.State.Tasks[1]
			task.Touch, task.Access = tc.touch, tc.access
			if tc.access == brief.AccessRead {
				sup.State.Tasks = append(sup.State.Tasks, &TaskState{Number: 3, ID: "third", Access: brief.AccessEdit})
			}
			chooseMode(sup.Env, sup.State)
			if sup.State.ModeReason != tc.reason || (sup.State.Mode == "worktree") != (tc.reason == "") {
				t.Fatalf("mode: %s (%s)", sup.State.Mode, sup.State.ModeReason)
			}
		})
	}
}

func TestIndependentTasksStartTogetherAndLand(t *testing.T) {
	for _, commit := range []bool{true, false} {
		t.Run(fmt.Sprint(commit), func(t *testing.T) {
			sup := worktreeSupervisor(t)
			sup.State.Commit = commit
			base := runGit(t, sup.Env.Root, "rev-parse", "HEAD")
			sup.schedule()
			if len(sup.running) != 2 || sup.State.Mode != "worktree" {
				t.Fatalf("running: %d, state: %+v", len(sup.running), sup.State)
			}
			for range 2 {
				rec := takeResult(t, sup)
				if rec.Verification.CommitRange == "" {
					t.Fatalf("no supervisor commit: %+v", rec)
				}
				if diff := dispatch.DiffFor(sup.Env.Root, rec); !strings.Contains(diff, "+Task change") {
					t.Fatalf("committed diff is absent after merge: %s", diff)
				}
				body, err := os.ReadFile(filepath.Join(rec.Path, dispatch.FileBrief))
				if err != nil || !strings.Contains(string(body), "Do not commit") {
					t.Fatalf("Brief: %s, %v", body, err)
				}
				message := runGit(t, sup.Env.Root, "show", "-s", "--format=%an <%ae>%n%B", rec.Verification.CommitRange)
				if !strings.Contains(message, "Test User <user@example.test>") || !strings.Contains(message, "Co-Authored-By: Codex <noreply@openai.com>") {
					t.Fatalf("message: %s", message)
				}
			}
			if err := sup.end(StateFinished); err != nil || sup.State.State != StateFinished {
				t.Fatalf("Finish: %v, %+v", err, sup.State)
			}
			if got := runGit(t, sup.Env.Root, "rev-list", "--count", base+"..HEAD"); got != map[bool]string{true: "2", false: "0"}[commit] {
				t.Fatalf("Task commits: %s", got)
			}
			dirty, err := worktree.Dirty(sup.Env.Root)
			if err != nil || (len(dirty) == 0) != commit {
				t.Fatalf("landed changes: %v, %v", dirty, err)
			}
			if got := runGit(t, sup.Env.Root, "branch", "--list", sup.State.RunBranch); got != "" {
				t.Fatalf("Run branch remains: %s", got)
			}
		})
	}
}

func TestOverlapRunsInPlanOrder(t *testing.T) {
	sup := worktreeSupervisor(t)
	first, second := sup.State.Tasks[0], sup.State.Tasks[1]
	first.Files, second.Files = []string{"base.txt"}, []string{"base.txt"}
	setOrder(sup.State)
	sup.schedule()
	if len(sup.running) != 1 || second.Waiting != "files overlap with 1" || !reflect.DeepEqual(second.After, []int{1}) {
		t.Fatalf("order: %+v", second)
	}
	for _, text := range []string{Status(sup.State), Report(sup.State)} {
		if !strings.Contains(text, "after 1 (files overlap)") {
			t.Fatalf("missing order: %s", text)
		}
	}
	takeResult(t, sup)
	sup.schedule()
	if second.State != TaskRunning {
		t.Fatalf("second Task: %+v", second)
	}
	takeResult(t, sup)
}

func TestDependencyStartsFromMergedRunBranch(t *testing.T) {
	sup := worktreeSupervisor(t)
	first, second := sup.State.Tasks[0], sup.State.Tasks[1]
	second.BlockedBy = []string{first.ID}
	setOrder(sup.State)
	sup.schedule()
	if len(sup.running) != 1 {
		t.Fatal("dependent Task started early")
	}
	takeResult(t, sup)
	sup.schedule()
	if _, err := os.Stat(filepath.Join(second.Workdir, "first.txt")); err != nil {
		t.Fatalf("dependency change is absent: %v", err)
	}
	takeResult(t, sup)
}

func TestTreeTaskCommitUsesFilledTemplateAfterGate(t *testing.T) {
	sup := worktreeSupervisor(t)
	t.Setenv("TYPESAFE_API_KEY", "fixture-key")
	gateRan := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gateRan = true
		fmt.Fprint(w, `{"answers":{"satisfies":{"noul":1},"verdict":{"choice":"pass"},"risk":{"score":1,"legend":{"1":"low"}}}}`)
	}))
	defer server.Close()
	sup.Env.Roster.Router.Backend, sup.Env.Roster.Router.BaseURL = "jev", server.URL
	sup.State.Tasks = sup.State.Tasks[:1]
	task := sup.State.Tasks[0]
	b := sup.pending[task].brief
	on := true
	b.Commit = &on
	if err := brief.Write(b, b.Path); err != nil {
		t.Fatal(err)
	}
	template := "{title}\n\nRun {run}, Task {task}, {family}, {model}\n{summary}\nCo-Authored-By: {lane}"
	off := false
	sup.Env.Project = &config.Project{Worktrees: &off, CommitMessage: &template, Checks: []string{"true"}}
	sup.Env.Roster.Commit.Identities = map[string]string{"codex": "Custom Lane <lane@example.test>"}
	chooseMode(sup.Env, sup.State)
	sup.schedule()
	rec := takeResult(t, sup)
	if task.State != TaskDone || rec.Verification.CommitRange == "" || task.Workdir != "" || !gateRan || rec.Outcome.Outcome != dispatch.OutcomeVerified {
		t.Fatalf("tree Task: %+v; %+v", task, rec)
	}
	message := runGit(t, sup.Env.Root, "show", "-s", "--format=%B", "HEAD")
	for _, want := range []string{task.Title, sup.State.Run, task.ID, "codex-sol", "gpt-6.1-sol", "Change the Task file.", "Custom Lane <lane@example.test>"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message lacks %q: %s", want, message)
		}
	}
}

func TestVerifiedTaskUsesTemplateAndUserAuthorThenMerges(t *testing.T) {
	sup := worktreeSupervisor(t)
	task := sup.State.Tasks[0]
	if err := sup.taskWorkdir(task, sup.pending[task].brief); err != nil {
		t.Fatal(err)
	}
	template := "{title}\n\n{run}/{task}: {summary}\nCo-Authored-By: {lane}"
	sup.Env.Roster.Commit.Message = &template
	runWrite(t, filepath.Join(task.Workdir, "first.txt"), "verified change\n")
	runWrite(t, filepath.Join(task.Folder, dispatch.FileReport), "## Summary\nKeep the verified change.\n\nSTATUS: done\n")
	rec := &dispatch.Record{Kind: "dispatch", Path: task.Folder, Attempt: 1, Route: task.Route, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeVerified}, Verification: dispatch.Verification{Changed: []string{"first.txt"}}}
	sup.handle(result{task: task, rec: rec})
	if task.State != TaskDone || rec.Verification.CommitRange == "" {
		t.Fatalf("verified Task: %+v, %+v", task, rec)
	}
	want := "Test User <user@example.test>\n" + task.Title + "\n\n" + sup.State.Run + "/" + task.ID + ": Keep the verified change.\nCo-Authored-By: Codex <noreply@openai.com>"
	if got := runGit(t, sup.Env.Root, "show", "-s", "--format=%an <%ae>%n%B", sup.State.RunBranch); got != want {
		t.Fatalf("message and author: %q, want %q", got, want)
	}
	if got := runGit(t, sup.Env.Root, "show", sup.State.RunBranch+":first.txt"); got != "verified change" {
		t.Fatalf("merged content: %s", got)
	}
}

func TestConflictAttemptBlocksAndHandMergeResumes(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			sup := worktreeSupervisor(t)
			first, second := sup.State.Tasks[0], sup.State.Tasks[1]
			for _, task := range sup.State.Tasks {
				task.Touch = append(task.Touch, "/base.txt")
				pending := sup.pending[task]
				pending.brief.Touch = task.Touch
				if err := brief.Write(pending.brief, pending.brief.Path); err != nil {
					t.Fatal(err)
				}
				if err := sup.taskWorkdir(task, sup.pending[task].brief); err != nil {
					t.Fatal(err)
				}
			}
			for _, task := range sup.State.Tasks {
				runWrite(t, filepath.Join(task.Workdir, "base.txt"), task.ID+"\n")
				runWrite(t, filepath.Join(task.Folder, dispatch.FileReport), "## Summary\nChange the shared file.\n\nSTATUS: done\n")
				rec := &dispatch.Record{Kind: "dispatch", Path: task.Folder, Attempt: 1, Route: task.Route, Verification: dispatch.Verification{Changed: []string{"base.txt"}}, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeVerified, Status: brief.StatusDone}}
				sup.handle(result{task: task, rec: rec})
			}
			if first.State != TaskDone || second.State != TaskWaiting || !second.ConflictAttempt || !reflect.DeepEqual(second.Conflicts, []string{"base.txt"}) {
				t.Fatalf("first merge conflict: %+v", second)
			}
			if failure {
				sup.handle(result{task: second, rec: &dispatch.Record{Attempt: 2, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, FailureClass: dispatch.FailBad}}})
			} else {
				if !sup.start(second) {
					t.Fatalf("conflict attempt did not start: %+v", second)
				}
				takeResult(t, sup)
				body, err := os.ReadFile(filepath.Join(second.Folder, dispatch.FileBrief))
				for _, want := range []string{"## Merge conflict", "base.txt", "Rebase this branch onto `" + sup.State.RunBranch + "`", "the Done of the Task still holds"} {
					if err != nil || !strings.Contains(string(body), want) {
						t.Fatalf("conflict Brief lacks %q: %s, %v", want, body, err)
					}
				}
				if !strings.Contains(string(body), "Maestro started this rebase") {
					t.Fatalf("conflict Brief does not say Maestro started the rebase: %s", body)
				}
				// The Lane left the conflict markers, so the attempt cannot merge.
				if (worktree.Worktree{Root: sup.Env.Root, Dir: second.Workdir}).Rebasing() || second.ConflictRebase {
					t.Fatal("blocked Task kept the supervisor's rebase")
				}
			}
			if second.State != TaskBlocked || second.Error != "merge conflict in base.txt" || second.Cause != "" || second.Escalations != 0 {
				t.Fatalf("second merge conflict: %+v", second)
			}
			if !strings.Contains(blockedText(second), second.Workdir) || !strings.HasSuffix(ResumeCommand(sup.State, second), "--merge") {
				t.Fatal("conflict answer lacks Worktree or merge command")
			}
			cmd := exec.Command("git", "-C", second.Workdir, "rebase", sup.State.RunBranch)
			if out, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("expected a conflict for the hand fix: %s", out)
			}
			runWrite(t, filepath.Join(second.Workdir, "base.txt"), "first and second\n")
			if err := sup.State.Save(); err != nil {
				t.Fatal(err)
			}
			if _, err := QueueResume(sup.State.Folder, Resume{Task: second.ID, Merge: true}); err == nil || !strings.Contains(err.Error(), "rebase is in progress") {
				t.Fatalf("--merge during a rebase: %v", err)
			}
			runGit(t, second.Workdir, "add", "-A")
			runGit(t, second.Workdir, "rebase", "--continue")
			if err := sup.State.Save(); err != nil {
				t.Fatal(err)
			}
			if _, err := QueueResume(sup.State.Folder, Resume{Task: second.ID, Merge: true}); err != nil {
				t.Fatal(err)
			}
			sup.consumeControl()
			if second.State != TaskDone || len(second.Conflicts) > 0 || len(sup.running) != 0 || second.Error != "" {
				t.Fatalf("hand merge: %+v", second)
			}
		})
	}
}

func TestConflictAttemptResolvesAndMerges(t *testing.T) {
	sup := worktreeSupervisor(t)
	first, second := sup.State.Tasks[0], sup.State.Tasks[1]
	for _, task := range sup.State.Tasks {
		task.Touch = append(task.Touch, "/base.txt")
		pending := sup.pending[task]
		pending.brief.Touch = task.Touch
		if err := brief.Write(pending.brief, pending.brief.Path); err != nil {
			t.Fatal(err)
		}
		if err := sup.taskWorkdir(task, pending.brief); err != nil {
			t.Fatal(err)
		}
	}
	for _, task := range sup.State.Tasks {
		runWrite(t, filepath.Join(task.Workdir, "base.txt"), task.ID+"\n")
		runWrite(t, filepath.Join(task.Folder, dispatch.FileReport), "## Summary\nChange the shared file.\n\nSTATUS: done\n")
		rec := &dispatch.Record{Kind: "dispatch", Path: task.Folder, Attempt: 1, Route: task.Route, Verification: dispatch.Verification{Changed: []string{"base.txt"}}, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeVerified, Status: brief.StatusDone}}
		sup.handle(result{task: task, rec: rec})
	}
	if first.State != TaskDone || second.State != TaskWaiting || !second.ConflictAttempt {
		t.Fatalf("first merge conflict: %+v", second)
	}
	// This Lane edits only the conflicted file; it runs no git command.
	bin := filepath.Join(t.TempDir(), "resolve")
	script := "#!/bin/sh\nread pointer\nprintf 'first and second\\n' > base.txt\nprintf '## Summary\\nKeep both changes.\\n\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	sup.Env.Reg = harness.New(map[string]string{"codex": bin})
	if !sup.start(second) {
		t.Fatalf("conflict attempt did not start: %+v", second)
	}
	if !second.ConflictRebase || !(worktree.Worktree{Root: sup.Env.Root, Dir: second.Workdir}).Rebasing() {
		t.Fatal("the supervisor did not start the rebase")
	}
	takeResult(t, sup)
	if second.State != TaskDone || len(second.Conflicts) > 0 || second.ConflictRebase {
		t.Fatalf("resolved conflict: %+v", second)
	}
	if got := runGit(t, sup.Env.Root, "show", sup.State.RunBranch+":base.txt"); got != "first and second" {
		t.Fatalf("merged content: %s", got)
	}
	if got := runGit(t, sup.Env.Root, "rev-list", "--count", "master.."+sup.State.RunBranch); got != "2" {
		t.Fatalf("Run branch commits: %s", got)
	}
}

func TestFinishPausesAndRetriesWithoutLane(t *testing.T) {
	sup := worktreeSupervisor(t)
	sup.State.Commit = true
	sup.schedule()
	for range 2 {
		takeResult(t, sup)
	}
	runWrite(t, filepath.Join(sup.Env.Root, "first.txt"), "user change\n")
	if err := sup.end(StateFinished); err != nil {
		t.Fatal(err)
	}
	if !sup.State.FinishPending || sup.State.State != StatePaused || !strings.Contains(sup.State.PauseReason, "uncommitted changes in first.txt overlap the Run") || !strings.Contains(Report(sup.State), "--finish") {
		t.Fatalf("Finish did not pause: %+v", sup.State)
	}
	if err := os.Remove(filepath.Join(sup.Env.Root, "first.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := QueueResume(sup.State.Folder, Resume{Finish: true}); err != nil {
		t.Fatal(err)
	}
	sup.consumeControl()
	if err := sup.end(StateFinished); err != nil || sup.State.State != StateFinished || sup.State.FinishPending || len(sup.running) != 0 {
		t.Fatalf("Finish retry: %+v, %v", sup.State, err)
	}
}

func TestDependencyFolderUsesTreeAndTreeBusy(t *testing.T) {
	sup := worktreeSupervisor(t)
	runWrite(t, filepath.Join(sup.Env.Root, "node_modules", "test", "index.js"), "dependency")
	for _, task := range sup.State.Tasks {
		task.Touch = []string{"node_modules/**", "/*.txt"}
		task.Files = nil
		pending := sup.pending[task]
		pending.brief.Touch = task.Touch
		sup.pending[task] = pending
	}
	chooseMode(sup.Env, sup.State)
	sup.schedule()
	first, second := sup.State.Tasks[0], sup.State.Tasks[1]
	if len(sup.running) != 1 || first.Workdir != "" || second.Waiting != "tree busy" || sup.State.Mode != "tree" || sup.State.ModeReason != "task 1 touches node_modules" {
		t.Fatalf("tree fallback: %+v, %+v", first, second)
	}
	if branch := runGit(t, sup.Env.Root, "symbolic-ref", "--short", "HEAD"); branch != "master" {
		t.Fatalf("user branch changed: %s", branch)
	}
	takeResult(t, sup)
	sup.schedule()
	takeResult(t, sup)
	if first.State != TaskDone || second.State != TaskDone {
		t.Fatalf("tree merge: %+v, %+v", first, second)
	}
	if err := sup.end(StateFinished); err != nil || sup.State.State != StateFinished {
		t.Fatalf("tree Finish: %+v, %v", sup.State, err)
	}
}

func TestCancelKeepsRunBranchAndRemovesWorktrees(t *testing.T) {
	sup := worktreeSupervisor(t)
	for _, task := range sup.State.Tasks {
		if err := sup.taskWorkdir(task, sup.pending[task].brief); err != nil {
			t.Fatal(err)
		}
	}
	first, second := sup.State.Tasks[0], sup.State.Tasks[1]
	runWrite(t, filepath.Join(first.Workdir, "first.txt"), "first\n")
	w := worktree.Worktree{Root: sup.Env.Root, Dir: first.Workdir}
	if _, err := w.Commit("Change first file", []string{"first.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := sup.mergeTask(first); err != nil {
		t.Fatal(err)
	}
	runWrite(t, filepath.Join(second.Workdir, "second.txt"), "second\n")
	w = worktree.Worktree{Root: sup.Env.Root, Dir: second.Workdir}
	if _, err := w.Commit("Change second file", []string{"second.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := sup.end(StateCancelled); err != nil {
		t.Fatal(err)
	}
	for _, task := range sup.State.Tasks {
		if _, err := os.Stat(task.Workdir); !os.IsNotExist(err) {
			t.Fatalf("cancel kept Worktree: %s, %v", task.Workdir, err)
		}
	}
	want := []string{sup.State.RunBranch, "maestro/" + sup.State.Run + "/" + second.ID}
	if !reflect.DeepEqual(sup.State.KeptBranches, want) {
		t.Fatalf("kept branches: %v, want %v", sup.State.KeptBranches, want)
	}
	for _, branch := range want {
		if got := runGit(t, sup.Env.Root, "branch", "--list", branch); got == "" || !strings.Contains(Report(sup.State), "kept branch "+branch) {
			t.Fatalf("cancel lost branch %s", branch)
		}
	}
	saved, err := Load(sup.State.Folder)
	if err != nil || !reflect.DeepEqual(saved.KeptBranches, want) {
		t.Fatalf("saved kept branches: %+v, %v", saved, err)
	}
}
