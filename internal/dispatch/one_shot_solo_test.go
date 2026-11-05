package dispatch

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/hook"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

type laneLaunch struct {
	Args    []string
	Env     map[string]string
	Pointer string
	Dir     string
	PID     int
	Parent  int
}

func TestOneShotLaneHelper(t *testing.T) {
	if os.Getenv("MAESTRO_TEST_LANE_HELPER") != "1" {
		return
	}
	capture := laneLaunch{Env: map[string]string{}, PID: os.Getpid(), Parent: os.Getppid()}
	for i, arg := range os.Args {
		if arg == "--" {
			capture.Args = os.Args[i+1:]
			break
		}
	}
	for _, kv := range os.Environ() {
		key, value, _ := strings.Cut(kv, "=")
		capture.Env[key] = value
	}
	capture.Dir, _ = os.Getwd()
	name := os.Getenv("MAESTRO_TEST_HARNESS")
	if name == "claude" || name == "codex" {
		pointer, _ := io.ReadAll(os.Stdin)
		capture.Pointer = string(pointer)
	} else {
		flag := "-p"
		if name == "grok" {
			flag = "--prompt-file"
		}
		for i, arg := range capture.Args {
			if arg == flag && i+1 < len(capture.Args) {
				capture.Pointer = capture.Args[i+1]
				if name == "grok" {
					pointer, err := os.ReadFile(capture.Pointer)
					if err != nil {
						panic(err)
					}
					capture.Pointer = string(pointer)
				}
			}
		}
	}
	raw, _ := json.Marshal(capture)
	if err := os.WriteFile(os.Getenv("MAESTRO_TEST_CAPTURE"), raw, 0o644); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("MAESTRO_TASK_FOLDER"), FileReport), []byte("## Answer\nDone\n\nSTATUS: done\n"), 0o644); err != nil {
		panic(err)
	}
	os.Exit(0)
}

func TestOneShotLaunchAndBriefDoNotDependOnSolo(t *testing.T) {
	for _, name := range []string{"claude", "codex", "grok", "agy"} {
		for _, access := range []string{brief.AccessRead, brief.AccessEdit} {
			t.Run(name+"/"+access, func(t *testing.T) {
				t.Setenv("MAESTRO_HOME", t.TempDir())
				t.Setenv("CODEX_HOME", t.TempDir())
				client, callsPath := deferredSoloClient(t)
				root := t.TempDir()
				if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
					t.Fatalf("git init: %v: %s", err, out)
				}
				folder := filepath.Join(config.RunsDir(root), "r-test", "abcd")
				if err := os.MkdirAll(folder, 0o755); err != nil {
					t.Fatal(err)
				}
				bin := filepath.Join(t.TempDir(), "lane")
				if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec \"$MAESTRO_TEST_BINARY\" -test.run=TestOneShotLaneHelper -- \"$@\"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				capturePath := filepath.Join(folder, "launch.json")
				t.Setenv("MAESTRO_TEST_LANE_HELPER", "1")
				t.Setenv("MAESTRO_TEST_HARNESS", name)
				t.Setenv("MAESTRO_TEST_CAPTURE", capturePath)
				r := &roster.Roster{Harness: map[string]config.Harness{name: {}}}
				r.Router.Backend = "rules"
				env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{name: bin})}
				task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: access,
					Route: &router.Route{Harness: name, Model: "test-model", Effort: "high"}, WallLimit: "1m", IdleLimit: "1m"}
				b := &brief.Brief{Title: "Same Brief", Goal: "Read the files.", Done: "Write the Report.", Files: []string{"main.go"}}
				var outside laneLaunch
				var outsideBrief []byte
				for _, inSolo := range []bool{false, true} {
					// The Solo identity belongs to the Orchestrator, not its child.
					t.Setenv("SOLO_PROCESS_ID", fmt.Sprint(map[bool]int{false: 0, true: 42}[inSolo]))
					t.Setenv("SOLO_PROJECT_ID", "1")
					t.Setenv("SOLO_TEST_IDENTITY", "parent")
					task.InSolo = inSolo
					if inSolo {
						env.Solo = &SoloLink{Client: client, ProjectID: 1}
					}
					rec := runOnce(env, task, b, 1, nil)
					want := OutcomeAnswered
					if access == brief.AccessEdit {
						want = OutcomeUnverified
					}
					wantProcess := 0
					if inSolo {
						wantProcess = 8
					}
					if rec.Outcome.Outcome != want || rec.SoloProcess != wantProcess {
						t.Fatalf("solo=%t: %+v", inSolo, rec.Outcome)
					}
					if rec.Outcome.ReportByText != (access == brief.AccessRead && (name == "codex" || name == "grok")) {
						t.Fatalf("solo=%t: wrong Report rules: %+v", inSolo, rec.Outcome)
					}
					var got laneLaunch
					raw, err := os.ReadFile(capturePath)
					if err != nil || json.Unmarshal(raw, &got) != nil {
						t.Fatalf("launch capture: %v", err)
					}
					if got.Parent != os.Getpid() || got.PID == os.Getpid() || got.Pointer == "" {
						t.Fatal("Lane did not start as a child with a Pointer")
					}
					for key := range got.Env {
						if strings.HasPrefix(key, "SOLO_") {
							t.Fatalf("Lane has Solo identity variable %s", key)
						}
					}
					for _, arg := range got.Args {
						if strings.HasPrefix(arg, "mcp__solo__") {
							t.Fatalf("Lane has Solo launch argument %s", arg)
						}
					}
					policy, ok := hook.LoadLane(controlFolder(t, folder))
					if !ok {
						t.Fatalf("wrong Lane policy: %+v", policy)
					}
					policyJSON, err := json.Marshal(policy)
					if err != nil || strings.Contains(string(policyJSON), `"tab"`) {
						t.Fatalf("Lane policy has a tab field: %s, %v", policyJSON, err)
					}
					rendered, err := os.ReadFile(filepath.Join(folder, FileBrief))
					if err != nil {
						t.Fatal(err)
					}
					got.PID = 0
					if !inSolo {
						outside, outsideBrief = got, rendered
					} else {
						if !reflect.DeepEqual(got.Env, outside.Env) {
							t.Fatal("Solo changed the Lane environment")
						}
						got.Env, outside.Env = nil, nil
						if !reflect.DeepEqual(got, outside) || string(rendered) != string(outsideBrief) {
							t.Fatal("Solo changed the launch arguments, Pointer, working directory, or Brief")
						}
					}
				}
				calls, err := os.ReadFile(callsPath)
				if err != nil || strings.Count(string(calls), `"name":"spawn_process"`) != 1 || strings.Count(string(calls), `"name":"send_input"`) != 1 || strings.Count(string(calls), `"name":"close_process"`) != 1 {
					t.Fatalf("unexpected Solo calls: %s, %v", calls, err)
				}
			})
		}
	}
}
