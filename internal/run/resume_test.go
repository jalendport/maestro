package run

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func TestResumeWithBriefAtDispatchCapRunsAgain(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	r, err := roster.Load()
	if err != nil {
		t.Fatal(err)
	}
	r.Router.Backend = "rules"
	r.Failure.MaxDispatches = 2
	off := false
	r.Failure.KeepAwake = &off
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	bin := filepath.Join(t.TempDir(), "lane")
	script := `#!/bin/sh
read pointer
if [ ! -f "$TEST_RESUME_MARKER" ]; then
  printf 'started\n' > "$TEST_RESUME_MARKER"
  printf 'unknown model\n' >&2
  exit 1
fi
printf 'resumed\n' > "$TEST_RESUME_WORK"
printf 'STATUS: done\n' > "$MAESTRO_TASK_FOLDER/result.md"
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_RESUME_MARKER", filepath.Join(t.TempDir(), "marker"))
	t.Setenv("TEST_RESUME_WORK", filepath.Join(root, "work.txt"))
	b := &brief.Brief{Fields: brief.Fields{Title: "Resume the Task", Access: brief.AccessEdit, Touch: []string{"work.txt"}}, Goal: "Resume the Task.", Done: "The Task runs again."}
	b.Path = filepath.Join(root, "corrected.md")
	if err := brief.Write(b, b.Path); err != nil {
		t.Fatal(err)
	}
	env := &dispatch.Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": bin}), RetryDelay: time.Millisecond}
	task := &TaskState{Number: 1, ID: "abcd", State: TaskFailed, Attempt: 2, Escalations: 2, Access: brief.AccessEdit, Outcome: dispatch.OutcomeFailed, Error: "unknown model"}
	s := saveRun(t, env, task)
	task.Folder = filepath.Join(s.Folder, task.ID)
	sup := &Supervisor{Env: env, State: s, running: map[string]chan struct{}{}, results: make(chan result, 1), errorTexts: map[string]int{}}
	sup.applyResume(resumeFile(t, s.Folder, Resume{Task: task.ID, Brief: b.Path}))
	if task.State != TaskWaiting || task.AnswerAttempt != 2 || task.Escalations != 0 {
		t.Fatalf("resumed Task: %+v", task)
	}
	route := &router.Route{Family: r.Families["codex-sol"], FamilyID: "codex-sol", Harness: "codex", Model: "gpt-6.1-sol", Tier: roster.TierDaily}
	sup.pending = map[*TaskState]pendingDispatch{task: {brief: b, route: route}}
	if !sup.start(task) {
		t.Fatalf("Task did not start: %+v", task)
	}
	select {
	case got := <-sup.results:
		if got.err != nil || got.rec == nil || got.rec.Attempt != 4 || got.rec.Outcome.Outcome != dispatch.OutcomeUnverified {
			t.Fatalf("result: %+v, Record: %+v", got, got.rec)
		}
		sup.handle(got)
	case <-time.After(20 * time.Second):
		t.Fatal("resumed Task did not finish")
	}
	if task.State != TaskDone || task.Attempt != 4 {
		t.Fatalf("Task: %+v", task)
	}
	saved, err := Load(s.Folder)
	if err != nil || saved.Tasks[0].AnswerAttempt != 2 || !reflect.DeepEqual(saved.Tasks[0].JudgedFiles, []string{"work.txt"}) {
		t.Fatalf("saved answer attempt: %+v, %v", saved, err)
	}
	if _, err := os.Stat(filepath.Join(task.Folder, dispatch.FileAttempts, "3", dispatch.FileRecord)); err != nil {
		t.Fatalf("resumed retry was not archived: %v", err)
	}
}

func TestResumedAnswerKeepsItsDispatchCap(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	r := &roster.Roster{}
	r.Failure.MaxDispatches = 2
	task := &TaskState{ID: "abcd", State: TaskRunning, Attempt: 2, AnswerAttempt: 2, Outcome: dispatch.OutcomeFailed, Error: "old error", Route: &router.Route{FamilyID: "first", Tier: roster.TierDaily}}
	env := &dispatch.Env{Root: t.TempDir(), Roster: r}
	s := saveRun(t, env, task)
	sup := &Supervisor{Env: env, State: s, running: map[string]chan struct{}{}, errorTexts: map[string]int{}}
	sup.handle(result{task: task, rec: &dispatch.Record{Attempt: 3, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, FailureClass: dispatch.FailLane, ErrorText: "old error"}}})
	if task.State != TaskWaiting || sup.paused {
		t.Fatalf("earlier answer counted against resume: %+v", task)
	}
	task.Route = &router.Route{FamilyID: "second", Tier: roster.TierHeavy}
	sup.handle(result{task: task, rec: &dispatch.Record{Attempt: 4, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, FailureClass: dispatch.FailLane, ErrorText: "new error"}}})
	if task.State != TaskFailed {
		t.Fatalf("resumed answer exceeded Dispatch cap: %+v", task)
	}
}
