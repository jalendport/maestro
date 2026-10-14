package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
