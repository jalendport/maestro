package dispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
)

func TestPumpDrops70MBLineAndContinues(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "output.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result := `{"type":"result","subtype":"success","result":"after long line"}`
	reader := io.MultiReader(strings.NewReader(strings.Repeat("x", 70<<20)), strings.NewReader("\n"+result+"\n"))
	parser := (&harness.Claude{}).NewParser()
	events := pumpStream(reader, file, parser, func() {})
	if len(events) != 1 || events[0].Kind != "stream" || !strings.Contains(events[0].Text, fmt.Sprint(70<<20)) {
		t.Fatalf("events: %+v", events)
	}
	if parser.Outcome().Text != "after long line" {
		t.Fatalf("pump stopped: %+v", parser.Outcome())
	}
	out, err := os.ReadFile(file.Name())
	if err != nil || string(out) != result+"\n" {
		t.Fatalf("output = %q, %v", out, err)
	}
}

func TestPumpHandlesFinalLineAndCRLF(t *testing.T) {
	parser := (&harness.Claude{}).NewParser()
	events := pumpStream(strings.NewReader("\r\n"+`{"type":"result","result":"final"}`), nil, parser, func() {})
	if len(events) != 0 || parser.Outcome().Text != "final" {
		t.Fatalf("events=%+v outcome=%+v", events, parser.Outcome())
	}
}

func TestFinishedLaneWinsReadyTimer(t *testing.T) {
	for _, wantErr := range []error{nil, errors.New("Lane exit")} {
		done := make(chan error, 1)
		done <- wantErr
		timer := time.NewTimer(0)
		<-timer.C
		stopped := false
		err, finished := laneFinished(done, func() { stopped = true; timer.Stop() })
		if !finished || err != wantErr || !stopped {
			t.Fatalf("finished=%v stopped=%v err=%v", finished, stopped, err)
		}
	}
}

func TestRunningLaneDoesNotStopTicker(t *testing.T) {
	stopped := false
	_, finished := laneFinished(make(chan error), func() { stopped = true })
	if finished || stopped {
		t.Fatalf("finished=%v stopped=%v", finished, stopped)
	}
}

func TestLimitProbeTimeoutIsUnconfirmed(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := budget.Load()
	if err != nil {
		t.Fatal(err)
	}
	env := &Env{Budget: f, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{"claude": bin})}
	rec := &Record{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	confirmed, note := env.confirmLimitContext(ctx, rec, "claude", "claude")
	if confirmed || !strings.Contains(note, "unconfirmed") || !strings.Contains(note, "timed out") {
		t.Fatalf("confirmed=%v note=%q", confirmed, note)
	}
	if len(rec.Events) != 1 || rec.Events[0].Text != note {
		t.Fatalf("events: %+v", rec.Events)
	}
	if f.Snapshot("claude") != nil {
		t.Fatal("timeout recorded a Budget block")
	}
}
