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
	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/hook"
)

func runHookPayload(t *testing.T, payload string, harness string) string {
	t.Helper()
	return strings.TrimSpace(runHookPayloadRaw(t, payload, harness))
}

func runHookPayloadRaw(t *testing.T, payload string, harness string) string {
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
	return captureStdout(t, command.Execute)
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
			control := controlFolder(t, folder)
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
			control := controlFolder(t, folder)
			if err := os.MkdirAll(control, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(control, "lane.json"), []byte(`{"access":"edit","root":"/project","tab":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			reply := answerOpenRelay(control, answer)
			payload := `{"toolCall":{"name":"run_command","args":{"CommandLine":"go run main.go","Cwd":"/project","Blocking":true}}}`
			output := runHookPayload(t, payload, "agy")
			want := `{"decision":"auto_approve"}`
			if answer == "deny" {
				want = `{"decision":"deny","reason":"deny"}`
			}
			if output != want {
				t.Fatalf("answer: %s, want %s", output, want)
			}
			got := <-reply
			if got.err != nil {
				t.Fatal(got.err)
			}
			b, err := os.ReadFile(filepath.Join(control, "relay-answer-"+got.id+".json"))
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
		{"Grok", `{"access":"read","root":"/project"}`, "grok", `{"toolName":"run_terminal_command","toolInput":{"command":"go run main.go"},"cwd":"/project"}`, `{"decision":"deny","reason":"read Lane: command not on the read-only list"}`},
		{"Claude", `{"access":"read","root":"/project"}`, "claude", `{"tool_name":"Bash","tool_input":{"command":"go run main.go"},"cwd":"/project"}`, `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"read Lane: command not on the read-only list"}}`},
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
			control := controlFolder(t, folder)
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
				entry, err := json.Marshal(map[string]any{"type": "USER_INPUT", "source": "USER_EXPLICIT", "content": "<USER_REQUEST>\n" + pointer + "\n</USER_REQUEST>"})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(transcript, entry, 0o600); err != nil {
					t.Fatal(err)
				}
				if policy != "missing" {
					control := controlFolder(t, folder)
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
						control := controlFolder(t, folder)
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
						if harness == "agy" || harness == "agy detected" {
							body, _ = json.Marshal(map[string]any{"type": "USER_INPUT", "source": "USER_EXPLICIT", "content": "<USER_REQUEST>\n" + pointer + "\n</USER_REQUEST>"})
						}
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

func TestHookCommandGrokLane(t *testing.T) {
	for _, tc := range []struct {
		name, signal, tool string
		policy             bool
		input              map[string]any
		want               string
	}{
		{"environment report write", "env", "write", true, map[string]any{"file_path": "<run>/result.md", "content": "x"}, "allow"},
		{"environment project write", "env", "write", true, map[string]any{"file_path": "<project>/main.go", "content": "x"}, "deny"},
		{"transcript report write", "transcript", "search_replace", true, map[string]any{"file_path": "<run>/result.md"}, "allow"},
		{"transcript project write", "transcript", "search_replace", true, map[string]any{"file_path": "<project>/main.go"}, "deny"},
		{"transcript read command", "transcript", "run_terminal_command", true, map[string]any{"command": "ls <project>"}, "allow"},
		{"transcript write command", "transcript", "run_terminal_command", true, map[string]any{"command": "touch <project>/x"}, "deny"},
		{"transcript spawn", "transcript", "spawn_subagent", true, map[string]any{"subagent_type": "writer"}, "deny"},
		{"transcript no policy", "transcript", "read_file", false, map[string]any{"target_file": "<project>/main.go"}, "deny"},
		{"orchestrator write", "none", "write", true, map[string]any{"file_path": "<project>/main.go"}, "allow"},
		{"orchestrator spawn", "none", "spawn_subagent", true, map[string]any{"subagent_type": "writer"}, "deny"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The control dir is named after the run folder's last two parts,
			// so each case gets its own Maestro home.
			t.Setenv("MAESTRO_HOME", t.TempDir())
			if err := os.WriteFile(config.UserFile(), []byte("[hook]\nscope = \"all\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			project := t.TempDir()
			run := filepath.Join(project, ".agents", "maestro", "runs", "r-test", "task")
			if err := os.MkdirAll(run, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MAESTRO_TASK_FOLDER", "")
			if tc.signal == "env" {
				t.Setenv("MAESTRO_TASK_FOLDER", run)
			}
			if tc.policy {
				control := controlFolder(t, run)
				if err := os.MkdirAll(control, 0o700); err != nil {
					t.Fatal(err)
				}
				lane := hook.Lane{Access: "read", Harness: "grok", Root: project, RunFolder: run}
				b, err := json.Marshal(lane)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(control, "lane.json"), b, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			prompt := "Explain this code."
			if tc.signal == "transcript" {
				prompt = brief.Pointer(filepath.Join(run, "brief.md"))
			}
			transcript := filepath.Join(t.TempDir(), "updates.jsonl")
			line, err := json.Marshal(map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{
				"sessionUpdate": "user_message_chunk", "content": map[string]any{"type": "text", "text": prompt}, "_meta": map[string]any{"promptIndex": 0},
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(transcript, append(line, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			input := map[string]any{}
			for key, value := range tc.input {
				if text, ok := value.(string); ok {
					value = strings.NewReplacer("<run>", run, "<project>", project).Replace(text)
				}
				input[key] = value
			}
			payload, err := json.Marshal(map[string]any{"hookEventName": "pre_tool_use", "cwd": project, "transcriptPath": transcript, "toolName": tc.tool, "toolInput": input})
			if err != nil {
				t.Fatal(err)
			}
			for _, harness := range []string{"grok", ""} {
				var answer struct{ Decision, Reason string }
				output := runHookPayload(t, string(payload), harness)
				if err := json.Unmarshal([]byte(output), &answer); err != nil {
					t.Fatalf("output %q: %v", output, err)
				}
				if answer.Decision != tc.want {
					t.Fatalf("harness %q: %s, want %s", harness, output, tc.want)
				}
				if tc.name == "transcript no policy" && answer.Reason != "no readable Lane policy" {
					t.Fatal(output)
				}
			}
		})
	}
}

func TestHookCommandClaudeLane(t *testing.T) {
	for _, signal := range []string{"env", "transcript"} {
		for _, tc := range []struct {
			name, access, tool, policy string
			input                      map[string]any
			tab                        bool
			reason                     string
		}{
			{"read write flag", "read", "Bash", "valid", map[string]any{"command": "git log --output=x"}, false, "--output=x"},
			{"read report write", "read", "Write", "valid", map[string]any{"file_path": "<run>/result.md"}, false, ""},
			{"read tool", "read", "Read", "valid", map[string]any{"file_path": "<project>/main.go"}, false, ""},
			{"edit git write", "edit", "Edit", "valid", map[string]any{"file_path": "<project>/.git/config"}, false, ".git"},
			{"edit tab off-list command", "edit", "Bash", "valid", map[string]any{"command": "go run main.go"}, true, ""},
			{"missing policy", "read", "Write", "missing", map[string]any{"file_path": "<run>/result.md"}, false, "no readable Lane policy"},
			{"invalid policy", "read", "Write", "invalid", map[string]any{"file_path": "<run>/result.md"}, false, "no readable Lane policy"},
			{"reader spawn", "read", "Agent", "valid", map[string]any{"subagent_type": "Explore"}, false, ""},
			{"writer spawn", "edit", "Agent", "valid", map[string]any{"subagent_type": "writer"}, false, "Sub-agent"},
			{"Orchestrator Bash", "", "Bash", "valid", map[string]any{"command": "go run main.go"}, false, ""},
			{"Orchestrator spawn", "", "Agent", "valid", map[string]any{"subagent_type": "writer"}, false, "Sub-agent"},
		} {
			t.Run(signal+"/"+tc.name, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				t.Setenv("CLAUDE_CONFIG_DIR", "")
				t.Setenv("MAESTRO_HOME", t.TempDir())
				t.Setenv("MAESTRO_TASK_FOLDER", "")
				if err := os.WriteFile(config.UserFile(), []byte("[hook]\nscope = \"all\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				project := t.TempDir()
				folder := filepath.Join(project, ".agents", "maestro", "runs", "r-test", "task")
				control := controlFolder(t, folder)
				if tc.policy != "missing" {
					if err := os.MkdirAll(control, 0o700); err != nil {
						t.Fatal(err)
					}
					access := tc.access
					if access == "" {
						access = "read"
					}
					body, err := json.Marshal(hook.Lane{Harness: "claude", Access: access, Root: project, RunFolder: folder, Tab: tc.tab})
					if err != nil {
						t.Fatal(err)
					}
					if tc.policy == "invalid" {
						body = []byte("not JSON")
					}
					if err := os.WriteFile(filepath.Join(control, "lane.json"), body, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				prompt := "Explain this code."
				if tc.access != "" {
					if signal == "env" {
						t.Setenv("MAESTRO_TASK_FOLDER", folder)
					} else {
						prompt = brief.Pointer(filepath.Join(folder, "brief.md"))
					}
				}
				transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
				entry, err := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": prompt}})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(transcript, append(entry, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
				input := map[string]any{}
				for key, value := range tc.input {
					if text, ok := value.(string); ok {
						value = strings.NewReplacer("<run>", folder, "<project>", project).Replace(text)
					}
					input[key] = value
				}
				payload, err := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": project, "session_id": "test", "transcript_path": transcript, "tool_name": tc.tool, "tool_input": input})
				if err != nil {
					t.Fatal(err)
				}
				for _, harness := range []string{"claude", ""} {
					output := runHookPayloadRaw(t, string(payload), harness)
					if tc.reason == "" {
						if output != "" {
							t.Fatalf("harness %q: %q, want no output", harness, output)
						}
					} else {
						var answer struct {
							HookSpecificOutput struct {
								HookEventName, PermissionDecision, PermissionDecisionReason string
							}
						}
						if err := json.Unmarshal([]byte(output), &answer); err != nil {
							t.Fatalf("output %q: %v", output, err)
						}
						h := answer.HookSpecificOutput
						if h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" || !strings.Contains(h.PermissionDecisionReason, tc.reason) {
							t.Fatalf("harness %q: %s, want deny with %q", harness, output, tc.reason)
						}
					}
				}
				for _, name := range []string{"relay.json", "relay.lock"} {
					if _, err := os.Stat(filepath.Join(control, name)); !os.IsNotExist(err) {
						t.Fatalf("Claude must use its PermissionRequest Relay: %s, %v", name, err)
					}
				}
			})
		}
	}
}

func TestHookCommandCodexToolCallsSkipTranscript(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	if err := os.WriteFile(config.UserFile(), []byte("[hook]\nscope = \"all\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The transcript names a real Lane whose policy would answer differently
	// from the plain spawn rule: a read Lane denies this command, and a Lane
	// without a policy denies everything. Either answer means the transcript was read.
	for _, harness := range []string{"codex"} {
		for _, tc := range []struct {
			name, tool string
			input      map[string]any
			policy     bool
		}{
			{"read Lane policy", "run_command", map[string]any{"CommandLine": "go test ./..."}, true},
			{"no Lane policy", "Agent", map[string]any{"subagent_type": "writer"}, false},
		} {
			t.Run(harness+"/"+tc.name, func(t *testing.T) {
				folder := filepath.Join(t.TempDir(), ".agents", "maestro", "runs", "r-test", "task")
				if err := os.MkdirAll(folder, 0o755); err != nil {
					t.Fatal(err)
				}
				if tc.policy {
					control := controlFolder(t, folder)
					if err := os.MkdirAll(control, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(control, "lane.json"), []byte(`{"access":"read","root":"/project","run_folder":"`+folder+`"}`), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
				line := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": brief.Pointer(filepath.Join(folder, "brief.md"))}}
				if harness == "codex" {
					line = map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": brief.Pointer(filepath.Join(folder, "brief.md"))}}}}
				}
				body, err := json.Marshal(line)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(transcript, append(body, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
				call := &hook.Call{Harness: harness, TranscriptPath: transcript}
				if got, found, err := hook.FindLaneFolder(call); err != nil || !found || got != folder {
					t.Fatalf("the fixture must name a Lane: %q, %v, %v", got, found, err)
				}
				payload, err := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "transcript_path": transcript, "tool_name": tc.tool, "tool_input": tc.input})
				if err != nil {
					t.Fatal(err)
				}
				want := string(hook.Render(harness, hook.Decide(&hook.Call{Harness: harness, Tool: tc.tool, Input: tc.input}, "all")))
				if got := runHookPayload(t, string(payload), harness); got != want || strings.Contains(got, "Lane") {
					t.Fatalf("output = %q, want %q", got, want)
				}
			})
		}
	}
}

func TestHookCommandAntigravityTranscriptLimit(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	for _, tc := range []struct {
		name, text, want string
	}{
		{"bare path before 64 KiB", strings.Repeat("x", 63*1024) + " `/project/.agents/maestro/runs/r-test/abcd/brief.md`", `{"decision":"allow"}`},
		{"bare path after 64 KiB", strings.Repeat("x", 64*1024) + " `/project/.agents/maestro/runs/r-test/abcd/brief.md`", `{"decision":"allow"}`},
		{"first user prompt over 100 KiB", strings.Repeat(" ", 101*1024) + brief.Pointer("/project/.agents/maestro/runs/r-test/abcd/brief.md"), `{"decision":"deny","reason":"no readable Lane policy"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
			body, err := json.Marshal(map[string]any{"type": "USER_INPUT", "source": "USER_EXPLICIT", "content": "<USER_REQUEST>\n" + tc.text + "\n</USER_REQUEST>"})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(transcript, body, 0o600); err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(map[string]any{"transcriptPath": transcript, "toolCall": map[string]any{"name": "run_command", "args": map[string]any{"CommandLine": "pwd"}}})
			if err != nil {
				t.Fatal(err)
			}
			if got := runHookPayload(t, string(payload), "agy"); got != tc.want {
				t.Fatalf("output = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHookCommandTranscriptErrorsDeny(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	for _, harness := range []string{"agy", "grok", "claude"} {
		for _, failure := range []string{"unreadable", "cannot open", "oversized line", "reader limit", "missing"} {
			t.Run(harness+"/"+failure, func(t *testing.T) {
				transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
				var body string
				reason := ""
				switch failure {
				case "unreadable":
					transcript = t.TempDir()
					reason = "cannot read Lane transcript"
				case "cannot open":
					if err := os.WriteFile(transcript, nil, 0o000); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = os.Chmod(transcript, 0o600) })
					if f, err := os.Open(transcript); err == nil {
						f.Close()
						t.Skip("this user can open mode 000 files")
					}
					reason = "cannot open Lane transcript"
				case "oversized line":
					body = strings.Repeat("x", 4*1024*1024+1) + "\n"
					reason = "token too long"
				case "reader limit":
					line := "{\"type\":\"system\",\"text\":\"" + strings.Repeat("x", 1024) + "\"}\n"
					body = strings.Repeat(line, 16*1024*1024/len(line)+1)
					reason = "16 MiB reader limit"
				}
				if body != "" {
					if err := os.WriteFile(transcript, []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				payload := map[string]any{"transcriptPath": transcript, "cwd": "/project"}
				if harness == "grok" {
					payload["hookEventName"] = "PreToolUse"
					payload["toolName"] = "write"
					payload["toolInput"] = map[string]any{"file_path": "/project/x"}
				} else if harness == "claude" {
					payload = map[string]any{"hook_event_name": "PreToolUse", "transcript_path": transcript, "cwd": "/project", "tool_name": "Write", "tool_input": map[string]any{"file_path": "/project/x"}}
				} else {
					payload["workspacePaths"] = []string{"/project"}
					payload["toolCall"] = map[string]any{"name": "write_to_file", "args": map[string]any{"TargetFile": "/project/x"}}
				}
				b, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				for _, arg := range []string{harness, ""} {
					out := runHookPayload(t, string(b), arg)
					if harness == "claude" && failure == "missing" {
						if out != "" {
							t.Fatalf("%s = %s, want no output", arg, out)
						}
						continue
					}
					var answer struct {
						Decision, Reason   string
						HookSpecificOutput struct {
							PermissionDecision       string
							PermissionDecisionReason string
						}
					}
					if err := json.Unmarshal([]byte(out), &answer); err != nil {
						t.Fatal(err)
					}
					if harness == "claude" {
						answer.Decision = answer.HookSpecificOutput.PermissionDecision
						answer.Reason = answer.HookSpecificOutput.PermissionDecisionReason
					}
					want := "deny"
					if failure == "missing" {
						want = "allow"
					}
					if answer.Decision != want || reason != "" && (!strings.Contains(answer.Reason, reason) || !strings.Contains(answer.Reason, transcript)) {
						t.Fatalf("%s = %s, want %s with %q", arg, out, want, reason)
					}
				}
			})
		}
	}
}

func TestHookCommandGrokOtherEventsAreSilent(t *testing.T) {
	t.Setenv("MAESTRO_TASK_FOLDER", t.TempDir())
	for _, event := range []string{"UserPromptSubmit", "user_prompt_submit", "PostToolUse", "Stop", ""} {
		for _, tool := range []string{"", "write"} {
			payload := map[string]any{"hookEventName": event}
			if tool != "" {
				payload["toolName"] = tool
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			for _, arg := range []string{"grok", ""} {
				if got := runHookPayload(t, string(body), arg); got != "" {
					t.Fatalf("%s %s = %q", arg, event, got)
				}
			}
		}
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
					control := controlFolder(t, folder)
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
