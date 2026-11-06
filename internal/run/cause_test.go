package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func TestSupervisorRoutesOnlyFailuresAStrongerLaneCanHelp(t *testing.T) {
	for _, tc := range []struct {
		name, class, cause, status, outcome, blocked, wantCause, decision, state string
	}{
		{"Stall", dispatch.FailStall, "", "", dispatch.OutcomeTimedOut, "", "", "re-route", TaskWaiting},
		{"Lane error", dispatch.FailLane, "", "", dispatch.OutcomeFailed, "", "", "re-route", TaskWaiting},
		{"Transient error", dispatch.FailTransient, "", "", dispatch.OutcomeFailed, "", "", "re-route", TaskWaiting},
		{"Limit hit", dispatch.FailLimit, "", "", dispatch.OutcomeFailed, "", "", "re-route", TaskWaiting},
		{"capability", dispatch.FailBad, dispatch.CauseCapability, brief.StatusDone, dispatch.OutcomeFailed, "", dispatch.CauseCapability, "re-route", TaskWaiting},
		{"none", dispatch.FailBad, dispatch.CauseNone, brief.StatusDone, dispatch.OutcomeFailed, "gate needs an answer", dispatch.CauseNone, "blocked", TaskBlocked},
		{"brief", dispatch.FailBad, dispatch.CauseBrief, brief.StatusDone, dispatch.OutcomeFailed, "required.go is outside touch", dispatch.CauseBrief, "blocked", TaskBlocked},
		{"environment", dispatch.FailBad, dispatch.CauseEnvironment, brief.StatusDone, dispatch.OutcomeEscalated, "Docker is down", dispatch.CauseEnvironment, "blocked", TaskBlocked},
		{"rules partial", dispatch.FailBad, "", brief.StatusPartial, dispatch.OutcomeFailed, "module is missing", dispatch.CauseEnvironment, "blocked", TaskBlocked},
		{"rules plain failure", dispatch.FailBad, "", brief.StatusDone, dispatch.OutcomeFailed, "", dispatch.CauseCapability, "re-route", TaskWaiting},
		{"blocked Report", dispatch.FailBad, dispatch.CauseCapability, brief.StatusBlocked, dispatch.OutcomeBlocked, "needs the Orchestrator", dispatch.CauseCapability, "blocked", TaskBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			root := t.TempDir()
			folder := filepath.Join(config.RunsDir(root), "r-test")
			task := &TaskState{ID: "abcd", Number: 1, Folder: filepath.Join(folder, "abcd"), State: TaskRunning,
				Route: &router.Route{FamilyID: "first", Tier: roster.TierDaily}}
			if err := os.MkdirAll(task.Folder, 0o755); err != nil {
				t.Fatal(err)
			}
			r := &roster.Roster{}
			r.Failure.MaxDispatches = 6
			s := &State{Run: "r-test", Project: root, Pid: os.Getpid(), Folder: folder, State: StateLive, Tasks: []*TaskState{task}}
			sup := &Supervisor{Env: &dispatch.Env{Root: root, Roster: r}, State: s, running: map[string]chan struct{}{}, errorTexts: map[string]int{}}
			rec := &dispatch.Record{Kind: "dispatch", Project: root, Task: task.ID, Path: task.Folder, Attempt: 1,
				Route: task.Route, Outcome: dispatch.Outcome{Outcome: tc.outcome, FailureClass: tc.class, Status: tc.status, ErrorText: "work failed"}}
			if tc.blocked != "" {
				rec.Permission.Blocked = []string{tc.blocked, "second Blocked item"}
			}
			if tc.cause != "" {
				rec.Verification.Gate, _ = json.Marshal(dispatch.Gate{Cause: tc.cause, CauseSource: "jev"})
			}
			sup.handle(result{task: task, rec: rec})
			if task.State != tc.state || task.Cause != tc.wantCause || rec.Outcome.Decision != tc.decision {
				t.Fatalf("task=%+v outcome=%+v", task, rec.Outcome)
			}
			if tc.state == TaskBlocked {
				if task.Escalations != 0 || len(task.Excluded) != 0 || task.Waiting != "" {
					t.Fatalf("blocked Task re-Routed: %+v", task)
				}
				for _, text := range []string{Report(s), ReminderItems(root)} {
					if !strings.Contains(text, "cause "+tc.wantCause) || !strings.Contains(text, tc.blocked) {
						t.Fatalf("Orchestrator item: %s", text)
					}
				}
				s.State, s.PauseReason = StatePaused, "needs an answer"
				if err := s.Save(); err != nil {
					t.Fatal(err)
				}
				if text := ReminderItems(root); !strings.Contains(text, tc.blocked) || !strings.Contains(text, "cause "+tc.wantCause) {
					t.Fatalf("paused Reminder: %s", text)
				}
			} else if task.Escalations != 1 || len(task.Excluded) != 1 || (tc.wantCause != "" && !strings.Contains(task.Waiting, "cause "+tc.wantCause)) {
				t.Fatalf("re-Route: %+v", task)
			}
			rows := 0
			if err := ledger.Each(func(raw json.RawMessage) error {
				rows++
				var row struct {
					Outcome      struct{ Decision string }
					Verification struct {
						Gate struct {
							Cause  string
							Source string `json:"cause_source"`
						}
					}
				}
				if err := json.Unmarshal(raw, &row); err != nil {
					return err
				}
				if row.Outcome.Decision != tc.decision || row.Verification.Gate.Cause != tc.wantCause {
					t.Fatalf("Ledger row: %s", raw)
				}
				if tc.wantCause != "" {
					wantSource := "jev"
					if tc.cause == "" {
						wantSource = "rule"
					}
					if row.Verification.Gate.Source != wantSource {
						t.Fatalf("Cause source: %s", raw)
					}
				}
				return nil
			}); err != nil || rows != 1 {
				t.Fatalf("Ledger rows=%d error=%v", rows, err)
			}
		})
	}
}

func TestPausedRunIncludesAllUnresolvedCauses(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root := t.TempDir()
	folder := filepath.Join(config.RunsDir(root), "r-test")
	s := &State{Run: "r-test", Project: root, Folder: folder, State: StatePaused, PauseReason: "needs an answer"}
	s.Tasks = append(s.Tasks, &TaskState{ID: "failed", Number: 1, State: TaskFailed, Error: "Lane error"})
	for i, tc := range []struct {
		id, cause, blocked string
	}{
		{"brief", dispatch.CauseBrief, "required.go is outside touch"},
		{"environment", dispatch.CauseEnvironment, "Docker is down"},
		{"answered", dispatch.CauseBrief, "already answered"},
	} {
		task := &TaskState{ID: tc.id, Number: i + 2, State: TaskBlocked, Folder: filepath.Join(folder, tc.id)}
		if err := os.MkdirAll(task.Folder, 0o755); err != nil {
			t.Fatal(err)
		}
		rec := &dispatch.Record{Path: task.Folder, Permission: dispatch.Permission{Blocked: []string{tc.blocked}}}
		rec.Verification.Gate, _ = json.Marshal(dispatch.Gate{Cause: tc.cause, CauseSource: "rule"})
		if err := rec.Save(); err != nil {
			t.Fatal(err)
		}
		s.Tasks = append(s.Tasks, task)
	}
	s.Tasks = append(s.Tasks, &TaskState{ID: "dependent", Number: 5, State: TaskBlocked, DependencyBlocked: true, Error: "dependency block"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{PauseReason(s), ReminderItems(root)} {
		for _, want := range []string{"cause brief: required.go is outside touch", "cause environment: Docker is down"} {
			if !strings.Contains(text, want) {
				t.Fatalf("missing %q in paused Run: %s", want, text)
			}
		}
	}
	if err := os.MkdirAll(filepath.Join(folder, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "control", "resume-answered.json"), []byte(`{"task":"answered","skip":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// wait and the Solo wake-up use the same pause reason.
	if text := PauseReason(s); strings.Contains(text, "already answered") || strings.Contains(text, "dependency block") {
		t.Fatalf("resolved or dependent Task in pause reason: %s", text)
	}
}
