package run

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func TestRunNudgeKeepsSameLaneRetryWithinCaps(t *testing.T) {
	for _, errorAttempt := range []int{1, 2} {
		t.Run(fmt.Sprint(errorAttempt), func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("CODEX_HOME", t.TempDir())
			t.Setenv("TYPESAFE_API_KEY", "test-key")
			root := t.TempDir()
			if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, out)
			}
			r, err := roster.Load()
			if err != nil {
				t.Fatal(err)
			}
			off := false
			r.Failure.KeepAwake = &off
			r.Failure.MaxDispatches = 2
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				score := 0.55
				if calls > 1 {
					score = 0.80
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{
					"satisfies": map[string]float64{"noul": score},
					"verdict":   map[string]string{"choice": "pass"},
					"risk":      map[string]any{"score": 1, "legend": map[string]string{"1": "low"}},
					"done_1":    map[string]float64{"noul": 0.2},
				}})
			}))
			defer server.Close()
			r.Router.Backend, r.Router.BaseURL = "jev", server.URL
			bin := filepath.Join(t.TempDir(), "lane")
			script := `#!/bin/sh
read pointer
count=0
if [ -f "$TEST_NUDGE_COUNT" ]; then read count < "$TEST_NUDGE_COUNT"; fi
count=$((count + 1))
printf '%s\n' "$count" > "$TEST_NUDGE_COUNT"
printf 'work %s\n' "$count" > "$TEST_NUDGE_WORK"
if [ "$count" = "$TEST_NUDGE_ERROR" ]; then printf 'unknown model\n' >&2; exit 1; fi
printf 'STATUS: done\n' > "$MAESTRO_TASK_FOLDER/result.md"
`
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TEST_NUDGE_COUNT", filepath.Join(t.TempDir(), "count"))
			t.Setenv("TEST_NUDGE_WORK", filepath.Join(root, "work.txt"))
			t.Setenv("TEST_NUDGE_ERROR", fmt.Sprint(errorAttempt))
			b := &brief.Brief{Fields: brief.Fields{Title: "Complete the work", Access: brief.AccessEdit, Touch: []string{"work.txt"}, Checks: []string{"true"}}, Goal: "Complete the work.", Done: "- The behavior works."}
			b.Path = filepath.Join(root, "source.md")
			if err := brief.Write(b, b.Path); err != nil {
				t.Fatal(err)
			}
			env := &dispatch.Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": bin}), RetryDelay: time.Millisecond}
			route := &router.Route{Family: r.Families["codex-sol"], FamilyID: "codex-sol", Harness: "codex", Model: "gpt-6.1-sol", Tier: roster.TierDaily}
			task := &TaskState{Number: 1, ID: "abcd", State: TaskWaiting, BriefPath: b.Path, Access: brief.AccessEdit}
			s := saveRun(t, env, task)
			task.Folder = filepath.Join(s.Folder, task.ID)
			sup := &Supervisor{Env: env, State: s, running: map[string]chan struct{}{}, results: make(chan result, 1), pending: map[*TaskState]pendingDispatch{task: {brief: b, route: route}}, errorTexts: map[string]int{}}
			if !sup.start(task) {
				t.Fatalf("Task did not start: %+v", task)
			}
			select {
			case result := <-sup.results:
				if result.err != nil || result.rec.Attempt != 3 || !result.rec.Nudged || result.rec.Outcome.Outcome != dispatch.OutcomeVerified {
					t.Fatalf("result=%+v Record=%+v", result, result.rec)
				}
				sup.handle(result)
			case <-time.After(20 * time.Second):
				t.Fatal("Task did not finish")
			}
			if task.State != TaskDone || !task.Nudged || task.Attempt != 3 {
				t.Fatalf("Task=%+v", task)
			}
			rows := 0
			if err := ledger.Each(func(raw json.RawMessage) error {
				var row dispatch.Record
				if err := json.Unmarshal(raw, &row); err != nil {
					return err
				}
				rows++
				if row.Attempt != rows || row.Route.FamilyID != route.FamilyID {
					t.Errorf("Ledger row=%+v", row)
				}
				return nil
			}); err != nil || rows != 3 {
				t.Fatalf("Ledger rows=%d error=%v", rows, err)
			}
		})
	}
}

func TestNudgeAllowanceSurvivesReroute(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	r := &roster.Roster{}
	r.Failure.MaxDispatches = 4
	task := &TaskState{Number: 1, ID: "abcd", State: TaskRunning, Route: &router.Route{FamilyID: "first", Tier: roster.TierDaily}}
	env := &dispatch.Env{Root: t.TempDir(), Roster: r}
	s := saveRun(t, env, task)
	sup := &Supervisor{Env: env, State: s, running: map[string]chan struct{}{}, errorTexts: map[string]int{}}
	rec := &dispatch.Record{Attempt: 4, Nudged: true, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, FailureClass: dispatch.FailLane, ErrorText: "unknown model"}}
	sup.handle(result{task: task, rec: rec})
	if task.State != TaskWaiting || !task.Nudged || rec.Outcome.Decision != "re-route" {
		t.Fatalf("nudge used the Run cap: Task=%+v Record=%+v", task, rec)
	}
	task.Route = &router.Route{FamilyID: "second", Tier: roster.TierHeavy}
	sup.handle(result{task: task, rec: &dispatch.Record{Attempt: 5, Nudged: true, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, FailureClass: dispatch.FailLane, ErrorText: "different error"}}})
	if task.State != TaskFailed {
		t.Fatalf("Run exceeded its extra attempt: %+v", task)
	}
}
