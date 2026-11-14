package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
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

func TestPauseNoticeChangesOnlyWithAnswerSet(t *testing.T) {
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
