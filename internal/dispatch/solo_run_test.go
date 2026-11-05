package dispatch_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/run"
	"github.com/jalendport/maestro/internal/solo"
)

func TestSoloRunKeepsTaskTodoReportAndEndWake(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("SOLO_PROCESS_ID", "42")
	t.Setenv("MAESTRO_TEST_SOLO_HELPER", "1")
	callsPath := filepath.Join(t.TempDir(), "calls")
	t.Setenv("MAESTRO_TEST_CALLS", callsPath)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_TEST_BINARY", self)
	helper := filepath.Join(t.TempDir(), "solo-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexec \"$MAESTRO_TEST_BINARY\" -test.run=^TestSoloHelper$\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_SOLO_MCP", helper)
	client, err := solo.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\nprintf '## Answer\\nTask complete\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	d, _, err := config.ParseFile(roster.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	r := roster.Merge(d, &config.File{})
	r.Router.Backend = "rules"
	r.Harness["codex"] = config.Harness{}
	off := false
	r.Failure.KeepAwake = &off
	root := t.TempDir()
	env := &dispatch.Env{Root: root, Roster: r, Solo: &dispatch.SoloLink{Client: client, ProjectID: 1, Orchestrator: 42},
		Reg: harness.New(map[string]string{"codex": bin}),
		Disc: &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{"codex": {
			Path: bin, Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}},
		}}}}
	plan := filepath.Join(root, "plan.md")
	if err := os.WriteFile(plan, []byte("---\naccess: read\nlevel: some\n---\n# Read the project\n\n## Task 1\n### Goal\nRead the project.\n### Done\nAn answer is in the Report.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := run.Create(env, plan, run.CreateOpts{Lane: "codex-sol:medium", NoCouncil: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Start(env, s.Folder); err != nil {
		t.Fatal(err)
	}
	s, err = run.Load(s.Folder)
	if err != nil || s.State != run.StateFinished || len(s.Tasks) != 1 || s.Tasks[0].State != run.TaskDone {
		t.Fatalf("Run did not finish: %+v, %v", s, err)
	}
	task, err := dispatch.LoadTask(s.Tasks[0].Folder)
	if err != nil || task.Todo != 7 {
		t.Fatalf("Task todo: %+v, %v", task, err)
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int{"todo_create": 1, "todo_comment_create": 1, "todo_complete": 1, "timer_set": 1, "spawn_agent": 0} {
		if got := strings.Count(string(calls), `"name":"`+name+`"`); got != want {
			t.Fatalf("%s count=%d, want %d: %s", name, got, want, calls)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		var call struct {
			Params struct {
				Name      string
				Arguments struct {
					Body    string
					Process int `json:"delivery_process_id"`
				}
			}
		}
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Name == "todo_comment_create" && !strings.Contains(call.Params.Arguments.Body, "Task complete") {
			t.Fatalf("Report comment: %s", line)
		}
		if call.Params.Name == "timer_set" && (call.Params.Arguments.Process != 42 || !strings.Contains(call.Params.Arguments.Body, "Maestro run end:") || !strings.Contains(call.Params.Arguments.Body, "maestro wait "+s.Run)) {
			t.Fatalf("Run end wake-up: %s", line)
		}
	}
}
