package council

import (
	"bufio"
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
	"github.com/jalendport/maestro/internal/solo"
)

func TestCouncilSoloHelper(t *testing.T) {
	if os.Getenv("MAESTRO_TEST_COUNCIL_HELPER") != "1" {
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
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string `json:"name"`
				Arguments struct {
					ExtraArgs []string `json:"extra_args"`
				} `json:"arguments"`
			} `json:"params"`
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
			text := `{}`
			switch req.Params.Name {
			case "todo_create":
				text = `{"todo_id":7}`
			case "spawn_agent":
				pointer := strings.Join(req.Params.Arguments.ExtraArgs, " ")
				parts := strings.Split(pointer, "`")
				if len(parts) < 2 {
					panic(pointer)
				}
				path := filepath.Join(filepath.Dir(parts[1]), dispatch.FileReport)
				if err := os.WriteFile(path, []byte("## Answer\nPASS\n\n## Findings\n- none\n\n## Concerns\n- none\n\nPOSITION: confirmed\n"), 0o644); err != nil {
					panic(err)
				}
				text = `{"process_id":42}`
			case "get_process_status":
				text = `{"status":"Exited","uptime_seconds":100}`
			case "get_process_output":
				text = `{"output":"Council Seat finished"}`
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}); err != nil {
			panic(err)
		}
	}
	os.Exit(0)
}

func TestCouncilSoloTodoAndMerge(t *testing.T) {
	for _, taskCouncil := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "task"}[taskCouncil], func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("CODEX_HOME", t.TempDir())
			t.Setenv("MAESTRO_TEST_COUNCIL_HELPER", "1")
			callsPath := filepath.Join(t.TempDir(), "calls.jsonl")
			t.Setenv("MAESTRO_TEST_CALLS", callsPath)
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("MAESTRO_TEST_BINARY", bin)
			helper := filepath.Join(t.TempDir(), "solo-helper")
			if err := os.WriteFile(helper, []byte("#!/bin/sh\nexec \"$MAESTRO_TEST_BINARY\" -test.run=TestCouncilSoloHelper\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MAESTRO_SOLO_MCP", helper)
			client, err := solo.Connect()
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			d, _, err := config.ParseFile(roster.Defaults)
			if err != nil {
				t.Fatal(err)
			}
			r := roster.Merge(d, &config.File{})
			r.Router.Backend = "rules"
			r.Harness["codex"] = config.Harness{SoloTool: 9}
			off := false
			r.Failure.KeepAwake = &off
			env := &dispatch.Env{Root: t.TempDir(), Roster: r, Solo: &dispatch.SoloLink{Client: client, ProjectID: 1},
				Reg:  harness.New(map[string]string{"codex": "/must-not-run-one-shot"}),
				Disc: &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{"codex": {Path: "/bin/codex", Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}, {ID: "gpt-6-astra"}}}}}}
			opts := Opts{Question: "Review the result", Lanes: []string{"codex-sol:high", "codex-astra:high"}, Level: roster.LevelSignificant}
			if taskCouncil {
				folder := filepath.Join(config.RunsDir(env.Root), "r-0001", "abcd")
				if err := os.MkdirAll(folder, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := (&dispatch.Task{Project: env.Root, Run: "r-0001", Task: "abcd", Folder: folder, Todo: 11}).Save(); err != nil {
					t.Fatal(err)
				}
				opts.Run, opts.Task, opts.Trigger = "r-0001", "abcd", TriggerTask
			}
			c, err := Create(env, opts)
			if err != nil {
				t.Fatal(err)
			}
			wantTodo := 7
			if taskCouncil {
				wantTodo = 11
			}
			if c.Todo != wantTodo {
				t.Fatalf("todo = %d, want %d", c.Todo, wantTodo)
			}
			c, err = Start(env, c.Path)
			if err != nil {
				t.Fatal(err)
			}
			if c.State != "finished" {
				t.Fatalf("state = %s", c.State)
			}
			if _, err := Start(env, c.Path); err != nil {
				t.Fatal(err)
			}
			calls, err := os.ReadFile(callsPath)
			if err != nil {
				t.Fatal(err)
			}
			counts := map[string]int{}
			for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
				var req struct {
					Params struct {
						Name      string `json:"name"`
						Arguments struct {
							Todo int    `json:"todo_id"`
							Body string `json:"body"`
						} `json:"arguments"`
					} `json:"params"`
				}
				if err := json.Unmarshal([]byte(line), &req); err != nil {
					t.Fatal(err)
				}
				counts[req.Params.Name]++
				if req.Params.Name == "todo_comment_create" && (req.Params.Arguments.Todo != wantTodo || req.Params.Arguments.Body != Merge(c)) {
					t.Fatalf("Merge comment: %s", line)
				}
			}
			wantCreate, wantComplete := 1, 1
			if taskCouncil {
				wantCreate, wantComplete = 0, 0
			}
			if counts["todo_create"] != wantCreate || counts["todo_complete"] != wantComplete || counts["todo_comment_create"] != 1 {
				t.Fatalf("calls = %v", counts)
			}
		})
	}
}
