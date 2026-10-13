package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func TestEscalationRecordsOutcomeInLedgerAndDispatch(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	folder := t.TempDir()
	task := &TaskState{ID: "abcd", Number: 1, State: TaskRunning,
		Route: &router.Route{FamilyID: "first", Tier: roster.TierDaily, Source: "rules"}}
	r := &roster.Roster{}
	r.Failure.MaxDispatches = 6
	sup := &Supervisor{Env: &dispatch.Env{Roster: r}, State: &State{Folder: folder, Tasks: []*TaskState{task}},
		running: map[string]chan struct{}{}, errorTexts: map[string]int{}}
	rec := &dispatch.Record{Kind: "dispatch", Task: task.ID, Path: filepath.Join(folder, task.ID), Attempt: 1,
		Route: task.Route, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, ErrorText: "check failed"}}
	if err := os.MkdirAll(rec.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	sup.handle(result{task: task, rec: rec})
	if task.State != TaskWaiting || task.Outcome != dispatch.OutcomeEscalated {
		t.Fatalf("Task did not escalate: %+v", task)
	}
	saved, err := dispatch.LoadRecord(rec.Path)
	if err != nil || saved.Outcome.Outcome != dispatch.OutcomeEscalated {
		t.Fatalf("Dispatch did not record escalation: %+v, %v", saved, err)
	}
	rows := 0
	if err := ledger.Each(func(raw json.RawMessage) error {
		rows++
		var row dispatch.Record
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		if row.Outcome.Outcome != dispatch.OutcomeEscalated || row.Route.Source != "rules" {
			t.Fatalf("Ledger did not record escalation: %+v", row)
		}
		return nil
	}); err != nil || rows != 1 {
		t.Fatalf("Ledger rows=%d, error=%v", rows, err)
	}
}
