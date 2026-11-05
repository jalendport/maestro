package dispatch

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
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

func TestSoloHelper(t *testing.T) {
	if os.Getenv("MAESTRO_TEST_SOLO_HELPER") != "1" {
		return
	}
	calls, err := os.Create(os.Getenv("MAESTRO_TEST_CALLS"))
	if err != nil {
		panic(err)
	}
	defer calls.Close()
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var req struct {
			ID     int
			Method string
			Params struct {
				Name      string
				Arguments struct {
					TodoID int `json:"todo_id"`
				}
			}
		}
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			panic(err)
		}
		if req.ID == 0 {
			continue
		}
		var result any = map[string]any{}
		if req.Method == "tools/call" {
			if _, err := calls.Write(append(sc.Bytes(), '\n')); err != nil {
				panic(err)
			}
			body := `{}`
			if req.Params.Name == os.Getenv("MAESTRO_TEST_SOLO_DELAY_CALL") {
				if delay, err := time.ParseDuration(os.Getenv("MAESTRO_TEST_SOLO_DELAY")); err == nil {
					time.Sleep(delay)
				}
			}
			switch req.Params.Name {
			case "spawn_process":
				body = `{"process_id":8}`
			case "todo_create":
				body = `{"todo_id":7}`
			case "todo_get":
				body = fmt.Sprintf(`{"todo":{"id":%d}}`, req.Params.Arguments.TodoID)
			}
			result = map[string]any{"isError": (req.Params.Name == "todo_get" && req.Params.Arguments.TodoID == 999) || req.Params.Name == os.Getenv("MAESTRO_TEST_SOLO_FAIL"),
				"content": []any{map[string]any{"type": "text", "text": body}}}
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}); err != nil {
			panic(err)
		}
	}
	os.Exit(0)
}

func deferredSoloClient(t *testing.T) (*solo.Client, string) {
	t.Helper()
	t.Setenv("MAESTRO_TEST_SOLO_HELPER", "1")
	calls := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv("MAESTRO_TEST_CALLS", calls)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_TEST_BINARY", bin)
	helper := filepath.Join(t.TempDir(), "solo-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexec \"$MAESTRO_TEST_BINARY\" -test.run=TestSoloHelper\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_SOLO_MCP", helper)
	client, err := solo.Connect()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, calls
}

func TestDispatchAdoptsNamedSoloTodo(t *testing.T) {
	for _, todo := range []int{123, 999} {
		t.Run(fmt.Sprint(todo), func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("CODEX_HOME", t.TempDir())
			client, callsPath := deferredSoloClient(t)
			root := t.TempDir()
			lane := filepath.Join(t.TempDir(), "lane")
			if err := os.WriteFile(lane, []byte("#!/bin/sh\nprintf '## Answer\\nDone\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"), 0o755); err != nil {
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
				Disc: &discovery.Cache{Harness: map[string]discovery.Record{"codex": {Path: lane, Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}}}}}}
			b, err := brief.ParseText(fmt.Sprintf("---\ntitle: Named todo\naccess: read\ntodo: %d\n---\n## Goal\nWork\n## Done\nDone\n", todo))
			if err != nil {
				t.Fatal(err)
			}
			b.Path = filepath.Join(root, "brief.source.md")
			if err := brief.Write(b, b.Path); err != nil {
				t.Fatal(err)
			}
			task, err := Prepare(env, b, router.Request{Lane: "codex-sol:medium"}, "r-test", "named")
			if err != nil {
				t.Fatal(err)
			}
			rec, err := Execute(env, task)
			if err != nil || rec.Outcome.Outcome != OutcomeAnswered {
				t.Fatalf("record=%+v err=%v", rec, err)
			}
			wantTodo, wantCreates := 123, 0
			if todo == 999 {
				wantTodo, wantCreates = 7, 1
			}
			saved, err := LoadTask(task.Folder)
			if err != nil || saved.Todo != wantTodo {
				t.Fatalf("saved task=%+v err=%v", saved, err)
			}
			calls, err := os.ReadFile(callsPath)
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]int{"todo_get": 1, "todo_create": wantCreates, "todo_comment_create": 1, "todo_complete": 1, "spawn_agent": 0} {
				if got := strings.Count(string(calls), `"name":"`+name+`"`); got != want {
					t.Fatalf("%s count=%d want=%d: %s", name, got, want, calls)
				}
			}
			for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
				var call struct {
					Params struct {
						Name      string
						Arguments struct {
							TodoID int `json:"todo_id"`
						}
					}
				}
				if err := json.Unmarshal([]byte(line), &call); err != nil {
					t.Fatal(err)
				}
				if (call.Params.Name == "todo_comment_create" || call.Params.Name == "todo_complete") && call.Params.Arguments.TodoID != wantTodo {
					t.Fatalf("wrong todo: %s", line)
				}
			}
		})
	}
}

func TestWakeUsesOnlyFixedFields(t *testing.T) {
	client, callsPath := deferredSoloClient(t)
	link := &SoloLink{Client: client, ProjectID: 1, Orchestrator: 42}
	folder := t.TempDir()
	task := &Task{Task: "abcd", Title: "Brief title", Folder: folder}
	rec := &Record{Path: folder}
	tracker := &relayTracker{}
	tracker.start(&Env{Solo: link}, task, rec, "claude", RelayRequest{ID: "0123456789abcdef", AskedAt: time.Now(), ToolName: "LANE_TOOL", ToolInput: json.RawMessage(`{"command":"LANE_TEXT"}`)})
	link.Wake("run end", "r-test", "Run title", "", "")
	link.Wake("council end", "c-test", "Council title", "codex", "")
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		var call struct {
			Params struct {
				Name      string
				Arguments struct{ Body string }
			}
		}
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Name == "timer_set" {
			bodies = append(bodies, call.Params.Arguments.Body)
		}
	}
	want := []string{
		`Maestro relay: id="abcd" title="Brief title" harness="claude" relay="0123456789abcdef". Run: maestro answer abcd allow|deny --relay 0123456789abcdef`,
		`Maestro run end: id="r-test" title="Run title" harness="". Run: maestro wait r-test`,
	}
	if len(bodies) != len(want) {
		t.Fatalf("wake bodies: %q", bodies)
	}
	for i, body := range bodies {
		if body != want[i] || strings.Contains(body, "LANE_") {
			t.Fatalf("wake body=%q want=%q", body, want[i])
		}
	}
	if len(rec.Events) != 1 || !strings.Contains(rec.Events[0].Text, "LANE_TEXT") {
		t.Fatalf("Relay text lost from record: %+v", rec.Events)
	}
}
