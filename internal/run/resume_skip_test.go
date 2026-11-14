package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/harness"
)

func TestSkipReleasesThreeTaskChain(t *testing.T) {
	sup := worktreeSupervisor(t)
	first, second := sup.State.Tasks[0], sup.State.Tasks[1]
	first.State = TaskBlocked
	second.BlockedBy = []string{first.ID}
	third := *second
	third.Number, third.ID, third.Title = 3, "third", "Change third"
	third.Folder = filepath.Join(sup.State.Folder, third.ID)
	third.BlockedBy = []string{second.ID}
	sup.State.Tasks = append(sup.State.Tasks, &third)
	third.Touch, third.Files = []string{"/third.txt"}, []string{"third.txt"}
	runWrite(t, third.BriefPath+".third", "---\naccess: edit\nlane: codex-sol:medium\ntouch: ['/third.txt']\n---\n# Change third\n## Goal\nChange the Task file.\n## Done\nThe file exists.\n")
	third.BriefPath += ".third"
	b, err := brief.Parse(third.BriefPath)
	if err != nil {
		t.Fatal(err)
	}
	sup.pending[&third] = pendingDispatch{brief: b, route: second.Route}
	setOrder(sup.State)
	sup.updateDependencies()
	if second.State != TaskBlocked || third.State != TaskBlocked {
		t.Fatal("chain was not blocked")
	}
	sup.consumeControl()
	resumeFile(t, sup.State.Folder, Resume{Task: first.ID, Skip: true})
	sup.consumeControl()
	sup.schedule()
	if first.State != TaskSkipped || second.State != TaskRunning || third.State != TaskWaiting {
		t.Fatalf("skip did not start only Task 2: %s, %s, %s", first.State, second.State, third.State)
	}
	takeResult(t, sup)
	sup.updateDependencies()
	sup.schedule()
	if second.State != TaskDone || third.State != TaskRunning {
		t.Fatalf("chain stopped: %s, %s", second.State, third.State)
	}
	takeResult(t, sup)
	if third.State != TaskDone || sup.completionState() != StateFinished {
		t.Fatalf("chain did not finish: %+v", third)
	}
}

func TestOverlapAfterSkip(t *testing.T) {
	for _, state := range []string{TaskSkipped, TaskFailed, TaskBlocked, TaskCancelled} {
		t.Run(state, func(t *testing.T) {
			sup := worktreeSupervisor(t)
			first, second := sup.State.Tasks[0], sup.State.Tasks[1]
			first.State = state
			second.After = []int{first.Number}
			sup.updateDependencies()
			sup.schedule()
			if state == TaskSkipped {
				if second.State != TaskRunning {
					t.Fatalf("skipped overlap did not release Task: %+v", second)
				}
				takeResult(t, sup)
			} else if second.State != TaskBlocked || !second.DependencyBlocked {
				t.Fatalf("terminal overlap did not block Task: %+v", second)
			}
		})
	}
}

func TestAnswerDoesNotReleaseOtherBlockedTask(t *testing.T) {
	for _, dependent := range []bool{false, true} {
		t.Run(map[bool]string{false: "independent", true: "dependent"}[dependent], func(t *testing.T) {
			sup := worktreeSupervisor(t)
			sup.State.Mode = "tree"
			first, second := sup.State.Tasks[0], sup.State.Tasks[1]
			first.State, first.Error = TaskBlocked, "needs a new answer"
			second.State = TaskBlocked
			if dependent {
				second.DependencyBlocked, second.BlockedBy = true, []string{first.ID}
			}
			resumeFile(t, sup.State.Folder, Resume{Task: second.ID, Brief: second.BriefPath})
			pending := sup.pending[second]
			sup.consumeControl()
			sup.pending[second] = pending
			sup.schedule()
			if first.State != TaskBlocked || first.Error != "needs a new answer" {
				t.Fatalf("unanswered Task changed: %+v", first)
			}
			if dependent {
				if second.State != TaskBlocked || !second.DependencyBlocked || len(sup.running) != 0 {
					t.Fatalf("answer bypassed dependency: %+v", second)
				}
			} else {
				if second.State != TaskRunning || len(sup.running) != 1 {
					t.Fatalf("answered Task did not start: %+v", second)
				}
				takeResult(t, sup)
			}
		})
	}
}

func TestPauseNoticeSuppressesAnswersThatStartNothing(t *testing.T) {
	env := quietRunEnv(t)
	first := &TaskState{Number: 1, ID: "first", State: TaskBlocked}
	second := &TaskState{Number: 2, ID: "second", State: TaskBlocked, DependencyBlocked: true, BlockedBy: []string{first.ID}}
	s := saveRun(t, env, first, second)
	previous := notify
	t.Cleanup(func() { notify = previous })
	var notices []string
	notify = func(text string) { notices = append(notices, text) }
	sup := &Supervisor{Env: env, State: s}
	if err := sup.end(StatePaused); err != nil {
		t.Fatal(err)
	}
	// The answer for Task 2 cannot release it while Task 1 is blocked.
	resumeFile(t, s.Folder, Resume{Task: second.ID, Lane: "codex-sol"})
	sup.consumeControl()
	if err := sup.end(StatePaused); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 {
		t.Fatalf("unchanged answer set sent %d notices", len(notices))
	}
	// Check the same rule after a new supervisor loads the Run.
	loaded, err := Load(s.Folder)
	if err != nil {
		t.Fatal(err)
	}
	sup = &Supervisor{Env: env, State: loaded}
	if err := sup.end(StatePaused); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 {
		t.Fatalf("new supervisor repeated notice: %q", notices)
	}
	resumeFile(t, s.Folder, Resume{Task: first.ID, Skip: true})
	sup.consumeControl()
	loaded.Tasks[1].State, loaded.Tasks[1].DependencyBlocked = TaskBlocked, false
	if err := sup.end(StatePaused); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 2 || strings.Contains(PauseReason(loaded), "task 1") {
		t.Fatalf("changed set or skipped Task: notices=%q reason=%s", notices, PauseReason(loaded))
	}
	if _, err := os.Stat(filepath.Join(s.Folder, FileReport)); err != nil {
		t.Fatal(err)
	}
}

func countPauseNotices(t *testing.T) *int {
	t.Helper()
	previous := notify
	t.Cleanup(func() { notify = previous })
	count := new(int)
	notify = func(string) { *count++ }
	return count
}

func TestPauseNoticeAfterResumeFailsBeforeClaim(t *testing.T) {
	for _, failure := range []string{"router", "brief"} {
		t.Run(failure, func(t *testing.T) {
			sup := worktreeSupervisor(t)
			sup.State.Mode = "tree"
			sup.State.Tasks = sup.State.Tasks[:1]
			task := sup.State.Tasks[0]
			task.State = TaskBlocked
			count := countPauseNotices(t)
			if err := sup.end(StatePaused); err != nil {
				t.Fatal(err)
			}
			// A corrected Brief can pass resume validation and fail at routing.
			corrected := task.BriefPath + ".corrected"
			runWrite(t, corrected, "---\nlane: codx-sol\n---\n# Correct the Task\n## Goal\nCorrect the work.\n## Done\nThe work is complete.\n")
			resumeFile(t, sup.State.Folder, Resume{Task: task.ID, Brief: corrected})
			sup.consumeControl()
			if failure == "brief" {
				runWrite(t, corrected, "invalid Brief")
			}
			loaded, err := Load(sup.State.Folder)
			if err != nil {
				t.Fatal(err)
			}
			sup.State, task = loaded, loaded.Tasks[0]
			if sup.start(task) || task.State != TaskFailed || task.Attempt != 0 {
				t.Fatalf("expected failure before claim: %+v", task)
			}
			if failure == "router" && !strings.Contains(task.Error, `unknown family "codx-sol"`) {
				t.Fatalf("expected Router error: %s", task.Error)
			}
			if fileExists(resumeWorkPath(sup.State, task)) {
				t.Fatal("resume work marker remains after failure")
			}
			if err := sup.State.Save(); err != nil {
				t.Fatal(err)
			}
			loaded, err = Load(sup.State.Folder)
			if err != nil {
				t.Fatal(err)
			}
			sup.State = loaded
			if err := sup.end(StatePaused); err != nil {
				t.Fatal(err)
			}
			if *count != 2 {
				t.Fatalf("pause notices = %d, want 2", *count)
			}
		})
	}
}

func TestPauseNoticeAfterResumedDispatchBlocksAgain(t *testing.T) {
	for _, answer := range []string{"lane", "brief"} {
		t.Run(answer, func(t *testing.T) {
			sup := worktreeSupervisor(t)
			sup.State.Mode, sup.State.Lane = "tree", "codex-sol"
			sup.State.Tasks = sup.State.Tasks[:1]
			task := sup.State.Tasks[0]
			task.State = TaskBlocked
			bin := filepath.Join(t.TempDir(), "blocked")
			runWrite(t, bin, "#!/bin/sh\nread pointer\nprintf '## Blocked\\nNeeds a correction.\\n\\nSTATUS: blocked\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n")
			if err := os.Chmod(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			sup.Env.Reg = harness.New(map[string]string{"codex": bin})
			sup.Env.Disc = &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{
				"codex": {Path: bin, Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}}},
			}}
			count := countPauseNotices(t)
			if err := sup.end(StatePaused); err != nil {
				t.Fatal(err)
			}
			for attempt := 1; attempt <= 2; attempt++ {
				r := Resume{Task: task.ID, Lane: "codex-sol:medium"}
				if answer == "brief" {
					r.Lane, r.Brief = "", task.BriefPath
				}
				resumeFile(t, sup.State.Folder, r)
				sup.consumeControl()
				loaded, err := Load(sup.State.Folder)
				if err != nil {
					t.Fatal(err)
				}
				sup.State, task = loaded, loaded.Tasks[0]
				if !sup.start(task) {
					t.Fatalf("resumed Dispatch did not start: %+v", task)
				}
				takeResult(t, sup)
				if task.State != TaskBlocked || task.Attempt != attempt {
					t.Fatalf("resumed Dispatch did not block: %+v", task)
				}
				loaded, err = Load(sup.State.Folder)
				if err != nil {
					t.Fatal(err)
				}
				sup.State, task = loaded, loaded.Tasks[0]
				if err := sup.end(StatePaused); err != nil {
					t.Fatal(err)
				}
				if *count != attempt+1 {
					t.Fatalf("pause notices: got %d, want %d", *count, attempt+1)
				}
			}
		})
	}
}

func TestPauseNoticeAfterRepeatedFailedFinish(t *testing.T) {
	sup := worktreeSupervisor(t)
	sup.schedule()
	for range 2 {
		takeResult(t, sup)
	}
	runGit(t, sup.State.Project, "checkout", "--detach")
	count := countPauseNotices(t)
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if _, err := QueueResume(sup.State.Folder, Resume{Finish: true}); err != nil {
				t.Fatal(err)
			}
			sup.consumeControl()
		}
		if err := sup.end(StateFinished); err != nil {
			t.Fatal(err)
		}
		if !sup.State.FinishPending || sup.State.State != StatePaused || *count != attempt+1 {
			t.Fatalf("Finish pause %d: notices=%d state=%+v", attempt, *count, sup.State)
		}
	}
}

func TestPauseNoticeSuppressesRefusedAnswers(t *testing.T) {
	for _, answer := range []Resume{
		{Task: "first"},
		{Task: "first", Brief: "/missing/brief.md"},
		{Task: "first", Accept: true},
		{Task: "first", Merge: true},
		{Finish: true},
		{Task: "running", Lane: "codex-sol"},
	} {
		t.Run(fmt.Sprint(answer), func(t *testing.T) {
			env := quietRunEnv(t)
			s := saveRun(t, env, &TaskState{Number: 1, ID: "first", State: TaskBlocked}, &TaskState{Number: 2, ID: "running", State: TaskRunning})
			sup := &Supervisor{Env: env, State: s}
			count := countPauseNotices(t)
			if err := sup.end(StatePaused); err != nil {
				t.Fatal(err)
			}
			resumeFile(t, s.Folder, answer)
			sup.consumeControl()
			if err := sup.end(StatePaused); err != nil {
				t.Fatal(err)
			}
			if *count != 1 {
				t.Fatalf("refused answer sent %d notices", *count)
			}
		})
	}
}

func TestPauseNoticeAfterFailedMergeRetry(t *testing.T) {
	sup, _ := acceptanceConflictFixture(t)
	task := sup.State.Tasks[0]
	task.State = TaskBlocked
	count := countPauseNotices(t)
	if err := sup.end(StatePaused); err != nil {
		t.Fatal(err)
	}
	resumeFile(t, sup.State.Folder, Resume{Task: task.ID, Merge: true})
	sup.consumeControl()
	if err := sup.end(StatePaused); err != nil {
		t.Fatal(err)
	}
	if task.State != TaskBlocked || *count != 2 {
		t.Fatalf("merge retry: notices=%d Task=%+v", *count, task)
	}
}

func TestRefusedAnswerDoesNotResetNoticeForOtherDispatch(t *testing.T) {
	sup := worktreeSupervisor(t)
	sup.State.Mode = "tree"
	first, second := sup.State.Tasks[0], sup.State.Tasks[1]
	first.State = TaskBlocked
	count := countPauseNotices(t)
	if err := sup.end(StatePaused); err != nil {
		t.Fatal(err)
	}
	resumeFile(t, sup.State.Folder, Resume{Task: first.ID, Accept: true})
	sup.consumeControl()
	if !sup.start(second) {
		t.Fatal("independent Task did not start")
	}
	takeResult(t, sup)
	if err := sup.end(StatePaused); err != nil {
		t.Fatal(err)
	}
	if *count != 1 {
		t.Fatalf("refused answer and unrelated Dispatch sent %d notices", *count)
	}
}
