package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/hook"
)

func runHookPayload(t *testing.T, payload string, harness string) string {
	t.Helper()
	input, err := os.CreateTemp(t.TempDir(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = previous }()
	command := (&app{}).hookCmd()
	if harness != "" {
		command.SetArgs([]string{harness})
	}
	return strings.TrimSpace(captureStdout(t, command.Execute))
}

func TestHookCommandAntigravityLane(t *testing.T) {
	for _, tc := range []struct {
		name, access, tool string
		input              map[string]any
		want               string
	}{
		{"read command", "read", "run_command", map[string]any{"CommandLine": "pwd"}, "auto_approve"},
		{"read denied command", "read", "run_command", map[string]any{"CommandLine": "go test ./..."}, "deny"},
		{"edit check", "edit", "run_command", map[string]any{"CommandLine": "go test ./... -race"}, "auto_approve"},
		{"edit denied command", "edit", "run_command", map[string]any{"CommandLine": "go run main.go"}, "deny"},
		{"read project write", "read", "write_to_file", map[string]any{"TargetFile": "/project/file.go"}, "deny"},
		{"edit project write", "edit", "write_to_file", map[string]any{"TargetFile": "/project/file.go"}, "auto_approve"},
		{"read run write", "read", "write_to_file", map[string]any{"TargetFile": "/run/result.md"}, "auto_approve"},
		{"writer", "edit", "invoke_subagent", map[string]any{"agent": "coder"}, "deny"},
		{"read agent", "read", "invoke_subagent", map[string]any{"agent": "Explore"}, "allow"},
		{"other tool", "read", "view_file", map[string]any{}, "allow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			folder := t.TempDir()
			t.Setenv("MAESTRO_TASK_FOLDER", folder)
			control := config.ControlDirForFolder(folder)
			if err := os.MkdirAll(control, 0o700); err != nil {
				t.Fatal(err)
			}
			lane := hook.Lane{Access: tc.access, Root: "/project", RunFolder: "/run", Checks: []string{"go test ./..."}}
			b, err := json.Marshal(lane)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(control, "lane.json"), b, 0o600); err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(map[string]any{"toolCall": map[string]any{"name": tc.tool, "args": tc.input}})
			if err != nil {
				t.Fatal(err)
			}
			var answer struct {
				Decision, Reason string
			}
			output := runHookPayload(t, string(payload), "")
			if err := json.Unmarshal([]byte(output), &answer); err != nil {
				t.Fatal(err)
			}
			if answer.Decision != tc.want || answer.Decision == "deny" && answer.Reason == "" {
				t.Fatalf("answer: %s, want %s", output, tc.want)
			}
		})
	}
}

func TestHookCommandAntigravityRelay(t *testing.T) {
	for _, answer := range []string{"allow", "deny"} {
		t.Run(answer, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			folder := t.TempDir()
			t.Setenv("MAESTRO_TASK_FOLDER", folder)
			control := config.ControlDirForFolder(folder)
			if err := os.MkdirAll(control, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(control, "lane.json"), []byte(`{"access":"edit","root":"/project","tab":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(control, "answer-"+answer), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			payload := `{"toolCall":{"name":"run_command","args":{"CommandLine":"go run main.go","Cwd":"/project","Blocking":true}}}`
			output := runHookPayload(t, payload, "agy")
			want := `{"decision":"auto_approve"}`
			if answer == "deny" {
				want = `{"decision":"deny","reason":"deny"}`
			}
			if output != want {
				t.Fatalf("answer: %s, want %s", output, want)
			}
			b, err := os.ReadFile(filepath.Join(control, "relay-answer.json"))
			if err != nil {
				t.Fatal(err)
			}
			var saved struct {
				Request struct {
					ToolName  string         `json:"tool_name"`
					ToolInput map[string]any `json:"tool_input"`
				}
				Answer string
			}
			if err := json.Unmarshal(b, &saved); err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"CommandLine": "go run main.go", "Cwd": "/project", "Blocking": true}
			if saved.Answer != answer || saved.Request.ToolName != "run_command" || !reflect.DeepEqual(saved.Request.ToolInput, input) {
				t.Fatalf("saved Relay: %s", b)
			}
		})
	}
}

func TestHookCommandLaneFallback(t *testing.T) {
	for _, tc := range []struct {
		name, lane, harness, payload, want string
	}{
		{"outside Lane", "", "agy", `{"toolCall":{"name":"run_command","args":{"CommandLine":"go run main.go"}}}`, `{"decision":"allow"}`},
		{"missing Lane", "", "agy", `{"toolCall":{"name":"write_to_file","args":{"TargetFile":"/outside"}}}`, `{"decision":"deny","reason":"no readable Lane policy"}`},
		{"invalid Lane", "not JSON", "agy", `{"toolCall":{"name":"write_to_file","args":{"TargetFile":"/outside"}}}`, `{"decision":"deny","reason":"no readable Lane policy"}`},
		{"Grok", `{"access":"read"}`, "grok", `{"toolName":"run_command","toolInput":{"CommandLine":"go run main.go"}}`, `{"decision":"allow"}`},
		{"Claude", `{"access":"read"}`, "claude", `{"tool_name":"Bash","tool_input":{"command":"go run main.go"}}`, ""},
		{"Codex", `{"access":"read"}`, "codex", `{"tool_name":"Bash","tool_input":{"command":"go run main.go"}}`, ""},
		{"bad payload", `{"access":"read"}`, "agy", "not JSON", `{"decision":"allow"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			folder := t.TempDir()
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("MAESTRO_TASK_FOLDER", folder)
			if tc.name == "outside Lane" {
				t.Setenv("MAESTRO_TASK_FOLDER", "")
			}
			control := config.ControlDirForFolder(folder)
			if tc.lane != "" {
				if err := os.MkdirAll(control, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(control, "lane.json"), []byte(tc.lane), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := runHookPayload(t, tc.payload, tc.harness); got != tc.want {
				t.Fatalf("answer: %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHookCommandUnreadableAntigravityPayload(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = previous }()
	command := (&app{}).hookCmd()
	command.SetArgs([]string{"agy"})
	if got := strings.TrimSpace(captureStdout(t, command.Execute)); got != `{"decision":"allow"}` {
		t.Fatalf("answer: %s", got)
	}
}

func TestHookCommandTranscriptLane(t *testing.T) {
	for _, kind := range []string{"runs", "councils"} {
		for _, policy := range []string{"missing", "invalid", "valid"} {
			t.Run(kind+"/"+policy, func(t *testing.T) {
				t.Setenv("MAESTRO_HOME", t.TempDir())
				t.Setenv("MAESTRO_TASK_FOLDER", "")
				root := t.TempDir()
				folder := filepath.Join(root, ".agents", "maestro", kind, "review", "seat")
				transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
				pointer := "Read the file `" + filepath.Join(folder, "brief.md") + "` in full, then follow it."
				if err := os.WriteFile(transcript, []byte(pointer), 0o600); err != nil {
					t.Fatal(err)
				}
				if policy != "missing" {
					control := config.ControlDirForFolder(folder)
					if err := os.MkdirAll(control, 0o700); err != nil {
						t.Fatal(err)
					}
					body := []byte("not JSON")
					if policy == "valid" {
						body, _ = json.Marshal(hook.Lane{Access: "read", Root: root, RunFolder: folder})
					}
					if err := os.WriteFile(filepath.Join(control, "lane.json"), body, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				for _, tool := range []string{"run_command", "write_to_file", "replace_file_content", "multi_replace_file_content", "invoke_subagent", "define_subagent", "manage_subagents"} {
					input := map[string]any{"CommandLine": "pwd", "Cwd": root, "TargetFile": filepath.Join(folder, "result.md"), "agent": "Explore"}
					payload, _ := json.Marshal(map[string]any{"transcriptPath": transcript, "workspacePaths": []string{root}, "toolCall": map[string]any{"name": tool, "args": input}})
					got := runHookPayload(t, string(payload), "")
					want := `{"decision":"deny","reason":"no readable Lane policy"}`
					if policy == "valid" {
						want = `{"decision":"auto_approve"}`
						if strings.Contains(tool, "subagent") {
							want = `{"decision":"allow"}`
						}
					}
					if got != want {
						t.Fatalf("%s: %s, want %s", tool, got, want)
					}
				}
			})
		}
	}
}

func TestHookCommandReminder(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("MAESTRO_LANE", "")
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProjectFile(project), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	for _, harness := range []string{"", "claude", "codex", "agy", "agy detected"} {
		for _, tc := range []struct {
			name, scope, cwd string
			inScope          bool
		}{
			{"project", "projects", project, true},
			{"outside", "projects", outside, false},
			{"all", "all", outside, true},
			{"Roster failure", "invalid TOML = [", project, false},
		} {
			t.Run(harness+"/"+tc.name, func(t *testing.T) {
				body := "[hook]\nscope = \"" + tc.scope + "\"\n"
				if tc.name == "Roster failure" {
					body = tc.scope
					if _, _, err := config.ParseFile(body); err == nil {
						t.Fatal("fixture must prevent the Roster from loading")
					}
				}
				if err := os.WriteFile(config.UserFile(), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				payload := map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": tc.cwd, "prompt": "x"}
				arg := harness
				want := ""
				if harness == "agy" || harness == "agy detected" {
					payload = map[string]any{"invocationNum": 1, "workspacePaths": []string{tc.cwd, outside}}
					want = `{}`
					if harness == "agy detected" {
						arg = ""
					}
				}
				if tc.inScope {
					var answer any = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "UserPromptSubmit", "additionalContext": adapter.Reminder}}
					if harness == "agy" || harness == "agy detected" {
						answer = map[string]any{"injectSteps": []any{map[string]any{"ephemeralMessage": adapter.Reminder}}}
					}
					b, err := json.Marshal(answer)
					if err != nil {
						t.Fatal(err)
					}
					want = string(b)
				}
				b, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				if got := runHookPayload(t, string(b), arg); got != want {
					t.Fatalf("output = %q, want %q", got, want)
				}
			})
		}
	}
}

func TestHookCommandReminderInsideLane(t *testing.T) {
	for _, harness := range []string{"", "claude", "codex", "agy", "agy detected"} {
		for _, signal := range []string{"MAESTRO_LANE", "MAESTRO_TASK_FOLDER", "prompt", "transcript"} {
			if signal == "prompt" && (harness == "agy" || harness == "agy detected") {
				continue
			}
			for _, policy := range []string{"missing", "valid"} {
				t.Run(harness+"/"+signal+"/"+policy, func(t *testing.T) {
					t.Setenv("MAESTRO_HOME", t.TempDir())
					t.Setenv("MAESTRO_LANE", "")
					t.Setenv("MAESTRO_TASK_FOLDER", "")
					if err := os.WriteFile(config.UserFile(), []byte("[hook]\nscope = \"all\"\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					folder := filepath.Join(t.TempDir(), ".agents", "maestro", "runs", "r-test", "task")
					if policy == "valid" {
						control := config.ControlDirForFolder(folder)
						if err := os.MkdirAll(control, 0o700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(control, "lane.json"), []byte(`{"access":"read"}`), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					payload := map[string]any{"hook_event_name": "UserPromptSubmit"}
					arg, transcriptKey, want := harness, "transcript_path", ""
					if harness == "agy" || harness == "agy detected" {
						payload = map[string]any{"invocationNum": 1}
						transcriptKey, want = "transcriptPath", `{}`
						if harness == "agy detected" {
							arg = ""
						}
					}
					switch signal {
					case "MAESTRO_LANE":
						t.Setenv(signal, "1")
					case "MAESTRO_TASK_FOLDER":
						t.Setenv(signal, folder)
					case "prompt":
						payload["prompt"] = "Read the file `" + filepath.Join(folder, "brief.md") + "` in full, then follow it."
						transcript := filepath.Join(t.TempDir(), "empty.jsonl")
						if err := os.WriteFile(transcript, nil, 0o600); err != nil {
							t.Fatal(err)
						}
						payload[transcriptKey] = transcript
					case "transcript":
						transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
						pointer := "Read the file `" + filepath.Join(folder, "brief.md") + "` in full, then follow it."
						body := []byte(pointer)
						if harness != "agy" && harness != "agy detected" {
							message := map[string]any{"role": "user", "content": pointer}
							entry := map[string]any{"type": "user", "message": message}
							if harness == "codex" {
								message["type"] = "message"
								message["content"] = []any{map[string]any{"type": "input_text", "text": pointer}}
								entry = map[string]any{"type": "response_item", "payload": message}
							}
							body, _ = json.Marshal(entry)
						}
						if err := os.WriteFile(transcript, body, 0o600); err != nil {
							t.Fatal(err)
						}
						payload[transcriptKey] = transcript
					}
					b, err := json.Marshal(payload)
					if err != nil {
						t.Fatal(err)
					}
					if got := runHookPayload(t, string(b), arg); got != want {
						t.Fatalf("Reminder must be answered before the Lane policy: %q, want %q", got, want)
					}
				})
			}
		}
	}
}

func TestHookCommandReminderFirstUserPrompt(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("MAESTRO_LANE", "")
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	if err := os.WriteFile(config.UserFile(), []byte("[hook]\nscope = \"all\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pointer := "Read the file `/project/.agents/maestro/runs/r-test/abcd/brief.md` in full, then follow it. Write your report as the file says."
	for _, harness := range []string{"claude", "codex"} {
		fixture, err := os.ReadFile(filepath.Join("..", "hook", "testdata", harness+"-lane.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		orchestrator := strings.Replace(string(fixture), pointer, "Explain this code.", 1)
		for _, tc := range []struct {
			name, prompt, transcript string
			lane                     bool
		}{
			{"submitted Pointer with empty transcript", pointer, "", true},
			{"leading whitespace", " \t\n" + pointer, "", true},
			{"quoted Pointer", "Explain this prompt: " + pointer, orchestrator, false},
			{"tool result Pointer", "Continue.", orchestrator, false},
			{"Lane first prompt", "Continue.", string(fixture), true},
		} {
			t.Run(harness+"/"+tc.name, func(t *testing.T) {
				transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
				if err := os.WriteFile(transcript, []byte(tc.transcript), 0o600); err != nil {
					t.Fatal(err)
				}
				payload, err := json.Marshal(map[string]any{"hook_event_name": "UserPromptSubmit", "prompt": tc.prompt, "transcript_path": transcript})
				if err != nil {
					t.Fatal(err)
				}
				want := string(hook.RenderReminder(&hook.Call{Harness: harness}, "all"))
				if tc.lane {
					want = ""
				}
				if got := runHookPayload(t, string(payload), harness); got != want {
					t.Fatalf("output = %q, want %q", got, want)
				}
			})
		}
	}
}

func TestHookCommandClaudeAndCodexToolCallsSkipTranscript(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	if err := os.WriteFile(config.UserFile(), []byte("[hook]\nscope = \"all\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"claude", "codex"} {
		t.Run(harness, func(t *testing.T) {
			transcript := filepath.Join(t.TempDir(), "missing.jsonl")
			payload, err := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "transcript_path": transcript, "tool_name": "Agent", "tool_input": map[string]any{"subagent_type": "writer"}})
			if err != nil {
				t.Fatal(err)
			}
			want := string(hook.Render(harness, hook.Decide(&hook.Call{Tool: "Agent", Input: map[string]any{"subagent_type": "writer"}}, "all")))
			if got := runHookPayload(t, string(payload), harness); got != want || !strings.Contains(got, `"permissionDecision":"deny"`) {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestHookCommandAntigravityTranscriptLimit(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	for _, offset := range []int{63 * 1024, 64 * 1024} {
		t.Run(strconv.Itoa(offset), func(t *testing.T) {
			transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
			body := strings.Repeat("x", offset) + " `/project/.agents/maestro/runs/r-test/abcd/brief.md`"
			if err := os.WriteFile(transcript, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(map[string]any{"transcriptPath": transcript, "toolCall": map[string]any{"name": "run_command", "args": map[string]any{"CommandLine": "pwd"}}})
			if err != nil {
				t.Fatal(err)
			}
			want := `{"decision":"allow"}`
			if offset < 64*1024 {
				want = `{"decision":"deny","reason":"no readable Lane policy"}`
			}
			if got := runHookPayload(t, string(payload), "agy"); got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestHookCommandAntigravityInvocationOnly(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("MAESTRO_LANE", "")
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	root := t.TempDir()
	t.Chdir(root)
	for _, harness := range []string{"", "agy"} {
		if got := runHookPayload(t, `{"invocationNum":1}`, harness); got != `{}` {
			t.Fatalf("outside project: %s", got)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProjectFile(root), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"", "agy"} {
		if got := runHookPayload(t, `{"invocationNum":1}`, harness); !strings.Contains(got, `"injectSteps"`) || !strings.Contains(got, adapter.Reminder) {
			t.Fatalf("inside project: %s", got)
		}
	}
}

func TestHookCommandToolCallPrecedesReminder(t *testing.T) {
	for _, harness := range []string{"", "agy"} {
		for _, insideLane := range []bool{false, true} {
			t.Run(harness+"/Lane="+strconv.FormatBool(insideLane), func(t *testing.T) {
				t.Setenv("MAESTRO_HOME", t.TempDir())
				t.Setenv("MAESTRO_TASK_FOLDER", "")
				if err := os.WriteFile(config.UserFile(), []byte("[hook]\nscope = \"all\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if insideLane {
					folder := t.TempDir()
					t.Setenv("MAESTRO_TASK_FOLDER", folder)
					control := config.ControlDirForFolder(folder)
					if err := os.MkdirAll(control, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(control, "lane.json"), []byte(`{"access":"read","root":"/project"}`), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				for _, tool := range []string{"invoke_subagent", "run_command", "write_to_file"} {
					toolCall := map[string]any{"name": tool, "args": map[string]any{"agent": "coder", "Cwd": "/project", "CommandLine": "pwd", "TargetFile": "/project/file.go"}}
					payload := map[string]any{"toolCall": toolCall, "workspacePaths": []string{"/project"}}
					b, _ := json.Marshal(payload)
					want := runHookPayload(t, string(b), harness)
					var answer struct{ Decision string }
					if err := json.Unmarshal([]byte(want), &answer); err != nil {
						t.Fatal(err)
					}
					expected := "allow"
					if tool == "invoke_subagent" || insideLane && tool == "write_to_file" {
						expected = "deny"
					} else if insideLane {
						expected = "auto_approve"
					}
					if answer.Decision != expected {
						t.Fatalf("tool %s: %s, want %s", tool, want, expected)
					}
					payload["invocationNum"] = 1
					b, _ = json.Marshal(payload)
					if got := runHookPayload(t, string(b), harness); got != want {
						t.Fatalf("tool %s with invocationNum: %s, want %s", tool, got, want)
					}
				}
			})
		}
	}
}
