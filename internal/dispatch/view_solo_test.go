package dispatch

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func TestSlowViewDoesNotDelayLane(t *testing.T) {
	for _, call := range []string{"spawn_process", "send_input"} {
		t.Run(call, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("CODEX_HOME", t.TempDir())
			t.Setenv("MAESTRO_TEST_SOLO_DELAY", "3s")
			t.Setenv("MAESTRO_TEST_SOLO_DELAY_CALL", call)
			client, callsPath := deferredSoloClient(t)
			root := t.TempDir()
			folder := filepath.Join(root, "task")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(t.TempDir(), "lane")
			if err := os.WriteFile(bin, []byte("#!/bin/sh\nread pointer\nprintf '%s' \"$pointer\" > \"$MAESTRO_TASK_FOLDER/pointer\"\nprintf '## Answer\\nDone\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			env := &Env{Root: root, Roster: &roster.Roster{Concurrency: config.Concurrency{CloseTabs: "always"}}, Reg: harness.New(map[string]string{"codex": bin}), Solo: &SoloLink{Client: client, ProjectID: 1}}
			task := &Task{Project: root, Folder: folder, Task: "check", Title: "Read", Access: brief.AccessRead, Route: &router.Route{Harness: "codex", FamilyID: "codex-sol", Model: "test"}, WallLimit: "1s"}
			started := time.Now()
			rec := runOnce(env, task, &brief.Brief{Goal: "Read", Done: "Report"}, 1, nil)
			if elapsed := time.Since(started); elapsed >= time.Second || rec.Outcome.Outcome != OutcomeAnswered {
				t.Fatalf("View delayed Lane: elapsed=%s outcome=%+v", elapsed, rec.Outcome)
			}
			pointer, err := os.ReadFile(filepath.Join(folder, "pointer"))
			if err != nil || !strings.Contains(string(pointer), "brief.md") {
				t.Fatalf("Pointer=%q error=%v", pointer, err)
			}
			env.Solo.views.Wait()
			calls, err := os.ReadFile(callsPath)
			if err != nil || strings.Count(string(calls), `"name":"close_process"`) != 1 {
				t.Fatalf("late View was not closed: %s, %v", calls, err)
			}
		})
	}
}

func TestDispatchOpensOneViewAndContinuesAfterSoloFailure(t *testing.T) {
	for _, failure := range []string{"", "spawn_process", "send_input"} {
		for _, tab := range []string{"", "Read project"} {
			t.Run(failure+"/"+tab, func(t *testing.T) {
				t.Setenv("MAESTRO_HOME", t.TempDir())
				t.Setenv("CODEX_HOME", t.TempDir())
				t.Setenv("MAESTRO_TEST_SOLO_FAIL", failure)
				client, callsPath := deferredSoloClient(t)
				root := t.TempDir()
				folder := filepath.Join(root, "project's space $literal")
				if err := os.MkdirAll(folder, 0o755); err != nil {
					t.Fatal(err)
				}
				bin := filepath.Join(t.TempDir(), "lane")
				if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\nprintf '## Answer\\nDone\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				env := &Env{Root: root, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{"codex": bin}), Solo: &SoloLink{Client: client, ProjectID: 1}}
				task := &Task{Project: root, Folder: folder, Task: "abcd", Title: "Read all project files today", Tab: tab, Access: brief.AccessRead,
					Route: &router.Route{Harness: "codex", FamilyID: "codex-sol", Model: "test"}, WallLimit: "1m", IdleLimit: "1m"}
				stderr, err := os.CreateTemp(t.TempDir(), "warnings")
				if err != nil {
					t.Fatal(err)
				}
				defer stderr.Close()
				previous := os.Stderr
				os.Stderr = stderr
				defer func() { os.Stderr = previous }()
				rec := runOnce(env, task, &brief.Brief{Goal: "Read", Done: "Report"}, 1, nil)
				env.Solo.views.Wait()
				if rec.Outcome.Outcome != OutcomeAnswered || rec.State != "ended" {
					t.Fatalf("Dispatch did not complete: %+v", rec)
				}
				if _, err := stderr.Seek(0, 0); err != nil {
					t.Fatal(err)
				}
				warnings, err := io.ReadAll(stderr)
				if err != nil {
					t.Fatal(err)
				}
				wantWarnings := 0
				if failure != "" {
					wantWarnings = 1
				}
				if strings.Count(string(warnings), "warning:") != wantWarnings {
					t.Fatalf("warnings: %s", warnings)
				}
				calls, err := os.ReadFile(callsPath)
				if err != nil {
					t.Fatal(err)
				}
				counts := map[string]int{}
				for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
					var call struct {
						Params struct {
							Name      string
							Arguments struct {
								Kind, Name, Input string
								ProcessID         int `json:"process_id"`
							}
						}
					}
					if err := json.Unmarshal([]byte(line), &call); err != nil {
						t.Fatal(err)
					}
					counts[call.Params.Name]++
					args := call.Params.Arguments
					switch call.Params.Name {
					case "spawn_process":
						wantName := tab
						if wantName == "" {
							wantName = "Read all project"
						}
						if args.Kind != "terminal" || args.Name != wantName+" · codex-sol" {
							t.Fatalf("terminal: %s", line)
						}
					case "send_input":
						self, err := os.Executable()
						if err != nil {
							t.Fatal(err)
						}
						if args.ProcessID != 8 || strings.Count(args.Input, "\n") != 1 || !strings.HasSuffix(args.Input, "\n") {
							t.Fatalf("start command: %s", line)
						}
						parsed, err := exec.Command("sh", "-c", "set -- "+strings.TrimSuffix(args.Input, "\n")+"; printf '%s\\n' \"$@\"").CombinedOutput()
						if err != nil || string(parsed) != self+"\nwatch\n"+folder+"\n" {
							t.Fatalf("command quoting: %q, %v", parsed, err)
						}
					case "close_process":
					default:
						t.Fatalf("unexpected Solo call: %s", line)
					}
				}
				wantSends := 1
				if failure == "spawn_process" {
					wantSends = 0
				}
				if counts["spawn_process"] != 1 || counts["send_input"] != wantSends {
					t.Fatalf("Solo call counts: %v", counts)
				}
			})
		}
	}
}

func TestCloseViewPolicy(t *testing.T) {
	for _, policy := range []string{"on-success", "always", "never"} {
		for _, outcome := range []string{OutcomeVerified, OutcomeFailed, OutcomeCancelled, OutcomeAnswered, OutcomeUnverified} {
			t.Run(policy+"/"+outcome, func(t *testing.T) {
				client, callsPath := deferredSoloClient(t)
				link := &SoloLink{Client: client, ProjectID: 1}
				env := &Env{Roster: &roster.Roster{Concurrency: config.Concurrency{CloseTabs: policy}}}
				link.CloseView(env, &Record{SoloProcess: 8, Outcome: Outcome{Outcome: outcome}})
				calls, err := os.ReadFile(callsPath)
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if policy == "always" || (policy == "on-success" && (outcome == OutcomeVerified || outcome == OutcomeAnswered || outcome == OutcomeUnverified)) {
					want = 1
				}
				if strings.Count(string(calls), `"name":"close_process"`) != want || (want == 0 && len(calls) != 0) || (want == 1 && !strings.Contains(string(calls), fmt.Sprintf(`"process_id":%d`, 8))) {
					t.Fatalf("calls=%s, want %d closes", calls, want)
				}
			})
		}
	}
}
