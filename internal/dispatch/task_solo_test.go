package dispatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/solo"
)

func TestTaskKeepsSoloTodoAfterEscalationAndCompletesUnverified(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("MAESTRO_TEST_SOLO_HELPER", "1")
	callsPath := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv("MAESTRO_TEST_CALLS", callsPath)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_TEST_BINARY", bin)
	helper := filepath.Join(t.TempDir(), "solo-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexec \"$MAESTRO_TEST_BINARY\" -test.run=TestSoloTabHelper\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_SOLO_MCP", helper)
	client, err := solo.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	lane := filepath.Join(t.TempDir(), "lane")
	if err := os.WriteFile(lane, []byte("#!/bin/sh\ncat >/dev/null\nprintf 'STATUS: %s\\n' \"$MAESTRO_TEST_STATUS\" > \"$MAESTRO_TEST_REPORT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	d, _, err := config.ParseFile(roster.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	r := roster.Merge(d, &config.File{})
	r.Router.Backend = "rules"
	off := false
	r.Failure.KeepAwake = &off
	env := &Env{Root: root, Roster: r, Solo: &SoloLink{Client: client, ProjectID: 1}, Reg: harness.New(map[string]string{"codex": lane}),
		Disc: &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{"codex": {Path: lane, Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}, {ID: "gpt-6-astra"}}}}}}
	b, err := brief.ParseText("---\ntitle: Test escalation\naccess: edit\n---\n## Goal\nFinish the Task.\n## Done\nThe Task is complete.\n")
	if err != nil {
		t.Fatal(err)
	}
	b.Path = filepath.Join(root, "brief.md")
	if err := brief.Write(b, b.Path); err != nil {
		t.Fatal(err)
	}
	first, err := Prepare(env, b, router.Request{Lane: "codex-sol:medium"}, "r-0001", "abcd")
	if err != nil {
		t.Fatal(err)
	}
	first.MaxAttempt = 1
	t.Setenv("MAESTRO_TEST_REPORT", filepath.Join(first.Folder, FileReport))
	t.Setenv("MAESTRO_TEST_STATUS", "partial")
	rec, err := Execute(env, first)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Outcome.Outcome == OutcomeVerified || rec.Outcome.Outcome == OutcomeUnverified {
		t.Fatalf("first attempt succeeded: %+v", rec.Outcome)
	}
	second, err := Prepare(env, b, router.Request{Lane: "codex-astra:high"}, "r-0001", "abcd")
	if err != nil {
		t.Fatal(err)
	}
	if second.Todo != first.Todo || second.Todo == 0 {
		t.Fatalf("todo lost: %d -> %d", first.Todo, second.Todo)
	}
	second.FirstAttempt, second.MaxAttempt = 2, 2
	t.Setenv("MAESTRO_TEST_STATUS", "done")
	rec, err = Execute(env, second)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Outcome.Outcome != OutcomeUnverified {
		t.Fatalf("second outcome: %+v", rec.Outcome)
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int{"todo_create": 1, "todo_complete": 1, "todo_comment_create": 2} {
		if got := strings.Count(string(calls), `"name":"`+name+`"`); got != want {
			t.Fatalf("%s count = %d, want %d: %s", name, got, want, calls)
		}
	}
}
