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

func TestOneShotLaunchAndBriefWithSolo(t *testing.T) {
	// The cases run in parallel: each case has its own project and Lane script,
	// and the environment and the Solo helper below are the same for every case.
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	client, callsPath := deferredSoloClient(t)
	soloCommand := os.Getenv("MAESTRO_SOLO_MCP")
	t.Setenv("MAESTRO_SOLO_MCP", "/unused-solo-command")
	t.Setenv("MAESTRO_TEST_LANE_HELPER", "1")
	// The Solo identity belongs to the Orchestrator, not its child.
	t.Setenv("SOLO_PROCESS_ID", "42")
	t.Setenv("SOLO_PROJECT_ID", "1")
	t.Setenv("SOLO_TEST_IDENTITY", "parent")
	for _, name := range []string{"claude", "codex", "grok", "agy"} {
		for _, access := range []string{brief.AccessRead, brief.AccessEdit} {
			t.Run(name+"/"+access, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
					t.Fatalf("git init: %v: %s", err, out)
				}
				folder := filepath.Join(config.RunsDir(root), "r-test", "abcd")
				if err := os.MkdirAll(folder, 0o755); err != nil {
					t.Fatal(err)
				}
				capturePath := filepath.Join(folder, "launch.json")
				bin := filepath.Join(t.TempDir(), "lane")
				script := fmt.Sprintf("#!/bin/sh\nexport MAESTRO_TEST_HARNESS='%s' MAESTRO_TEST_CAPTURE='%s'\nexec \"$MAESTRO_TEST_BINARY\" -test.run=TestOneShotLaneHelper -- \"$@\"\n", name, capturePath)
				if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				r := &roster.Roster{Harness: map[string]config.Harness{name: {}}}
				r.Router.Backend = "rules"
				env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{name: bin})}
				if name == "claude" {
					env.Project = &config.Project{Allow: []string{"mcp__solo", "mcp__solo__*", "mcp__solo__spawn_agent"}}
				}
				task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: access,
					Route: &router.Route{Harness: name, Model: "test-model", Effort: "high"}, WallLimit: "1m", IdleLimit: "1m"}
				b := &brief.Brief{Title: "Same Brief", Goal: "Read the files.", Done: "Write the Report.", Files: []string{"main.go"}}
				var outside laneLaunch
				var outsideBrief []byte
				for _, inSolo := range []bool{false, true} {
					task.InSolo = inSolo
					if inSolo {
						env.Solo = &SoloLink{Client: client, ProjectID: 1}
					}
					rec := runOnce(env, task, b, 1, nil)
					want := OutcomeAnswered
					if access == brief.AccessEdit {
						want = OutcomeUnverified
					}
					if rec.Outcome.Outcome != want {
						t.Fatalf("solo=%t: %+v", inSolo, rec.Outcome)
					}
					if rec.Outcome.ReportByText != (access == brief.AccessRead && (name == "codex" || name == "grok" || name == "agy")) {
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
					var soloTools, baseArgs []string
					mcpConfig := ""
					allowed := false
					for i := 0; i < len(got.Args); i++ {
						arg := got.Args[i]
						if arg == "--mcp-config" {
							if mcpConfig != "" || i+1 == len(got.Args) {
								t.Fatal("invalid MCP config arguments")
							}
							mcpConfig = got.Args[i+1]
							i++
							continue
						}
						if strings.HasPrefix(arg, "--") {
							allowed = arg == "--allowedTools"
						}
						if strings.HasPrefix(arg, "mcp__solo__") {
							if !allowed {
								t.Fatalf("Solo tool is outside the allow list: %s", arg)
							}
							soloTools = append(soloTools, arg)
							continue
						}
						if arg == "mcp__solo" {
							t.Fatal("Solo server rule permits tools outside the allow list")
						}
						baseArgs = append(baseArgs, arg)
					}
					withSoloTools := inSolo && name == "claude"
					if withSoloTools {
						wantTools := []string{
							"mcp__solo__list_processes", "mcp__solo__get_process_status",
							"mcp__solo__get_process_output", "mcp__solo__search_output",
							"mcp__solo__get_process_ports", "mcp__solo__todo_comment_create",
						}
						if !reflect.DeepEqual(soloTools, wantTools) {
							t.Fatalf("Solo allow list = %v, want %v", soloTools, wantTools)
						}
						var servers struct {
							MCPServers map[string]struct{ Command string } `json:"mcpServers"`
						}
						if err := json.Unmarshal([]byte(mcpConfig), &servers); err != nil || len(servers.MCPServers) != 1 || servers.MCPServers["solo"].Command != soloCommand {
							t.Fatalf("Solo server config = %s, error = %v", mcpConfig, err)
						}
					} else if mcpConfig != "" || len(soloTools) != 0 {
						t.Fatalf("unexpected Solo launch arguments: %s, %v", mcpConfig, soloTools)
					}
					got.Args = baseArgs
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
					soloContext := "## Solo\n\nThe Solo project id is `1`. Each Solo tool call needs this id in `project_id`.\n\n"
					if strings.Contains(string(rendered), soloContext) != withSoloTools {
						t.Fatalf("wrong Solo context in Brief: %s", rendered)
					}
					if !withSoloTools && (strings.Contains(string(rendered), "Solo project id") || strings.Contains(string(rendered), "`project_id`")) {
						t.Fatalf("Solo context in a Brief with no Solo tools: %s", rendered)
					}
					rendered = []byte(strings.ReplaceAll(string(rendered), soloContext, ""))
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
				if err != nil || strings.Count(string(calls), `"name":"spawn_process"`) != 0 || strings.Count(string(calls), `"name":"send_input"`) != 0 || strings.Count(string(calls), `"name":"close_process"`) != 0 {
					t.Fatalf("unexpected Solo calls: %s, %v", calls, err)
				}
			})
		}
	}
}
