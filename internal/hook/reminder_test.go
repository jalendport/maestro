package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/config"
)

func TestParseReminderEvents(t *testing.T) {
	for _, tc := range []struct {
		name, harness, payload, wantHarness, event, cwd, transcript string
	}{
		{"Claude detected", "", `{"hook_event_name":"UserPromptSubmit","cwd":"/project","transcript_path":"/transcript"}`, "claude", "reminder", "/project", "/transcript"},
		{"Claude named", "claude", `{"hook_event_name":"UserPromptSubmit","cwd":"/project","transcript_path":"/transcript"}`, "claude", "reminder", "/project", "/transcript"},
		{"Codex named", "codex", `{"hook_event_name":"UserPromptSubmit","cwd":"/project","transcript_path":"/transcript"}`, "codex", "reminder", "/project", "/transcript"},
		{"Antigravity detected", "", `{"invocationNum":1,"workspacePaths":["/first","/second"],"transcriptPath":"/transcript"}`, "agy", "reminder", "/first", "/transcript"},
		{"Antigravity named", "agy", `{"workspacePaths":["/first","/second"]}`, "agy", "reminder", "/first", ""},
		{"Antigravity workspace detected", "", `{"workspacePaths":["/first"]}`, "agy", "reminder", "/first", ""},
		{"Antigravity invocation detected", "", `{"invocationNum":1}`, "agy", "reminder", "", ""},
		{"Antigravity invocation named", "agy", `{"invocationNum":1}`, "agy", "reminder", "", ""},
		{"Antigravity named without common fields", "agy", `{}`, "agy", "reminder", "", ""},
		{"Claude tool call", "claude", `{"hook_event_name":"PreToolUse","transcript_path":"/transcript"}`, "claude", "", "", "/transcript"},
		{"Codex tool call", "codex", `{"hook_event_name":"PreToolUse","transcript_path":"/transcript"}`, "codex", "", "", "/transcript"},
		{"Grok tool call", "", `{"hookEventName":"PreToolUse","toolName":"read_file"}`, "grok", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call, err := Parse(tc.harness, []byte(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			if call.Harness != tc.wantHarness || call.Event != tc.event || call.CWD != tc.cwd || call.TranscriptPath != tc.transcript {
				t.Fatalf("call = %+v", call)
			}
		})
	}
}

func TestReminderPromptPointer(t *testing.T) {
	t.Setenv("MAESTRO_LANE", "")
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	transcript := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(transcript, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"", "claude", "codex"} {
		for _, folder := range []string{
			"/project/.agents/maestro/runs/r-test/task",
			"/project/.agents/maestro/councils/review/seat",
			"/project/.agents/maestro/councils/review/round-2/seat",
		} {
			path := folder + "/brief.md"
			pointer := "Read the file `" + path + "` in full, then follow it."
			for _, tc := range []struct {
				name, prompt string
				lane         bool
			}{
				{"empty", "", false},
				{"ordinary prompt", "Explain this code.", false},
				{"path only", path, false},
				{"cat", "cat " + path, false},
				{"partial sentence", "Read the file `" + path + "` in full", false},
				{"missing period", strings.TrimSuffix(pointer, "."), false},
				{"Pointer", pointer, true},
				{"leading whitespace", " \t\n" + pointer, true},
				{"quoted Pointer", "Explain this prompt: " + pointer, false},
				{"Pointer with Report instruction", pointer + " Write your report as the file says.", true},
			} {
				t.Run(harness+"/"+folder+"/"+tc.name, func(t *testing.T) {
					payload, err := json.Marshal(map[string]any{"hook_event_name": "UserPromptSubmit", "prompt": tc.prompt, "transcript_path": transcript})
					if err != nil {
						t.Fatal(err)
					}
					call, err := Parse(harness, payload)
					if err != nil {
						t.Fatal(err)
					}
					if call.Prompt != tc.prompt {
						t.Fatalf("prompt = %q, want %q", call.Prompt, tc.prompt)
					}
					out := RenderReminder(call, "all")
					if tc.lane && out != nil || !tc.lane && !strings.Contains(string(out), adapter.Reminder) {
						t.Fatalf("Lane = %v, output = %s", tc.lane, out)
					}
				})
			}
		}
	}
}

func TestParseToolCallPrecedesReminder(t *testing.T) {
	for _, harness := range []string{"", "agy"} {
		for _, fields := range []string{"", `"invocationNum":1,"hook_event_name":"UserPromptSubmit",`} {
			payload := `{` + fields + `"workspacePaths":["/workspace"],"toolCall":{"name":"invoke_subagent","args":{"agent":"coder","Cwd":"/tool"}}}`
			call, err := Parse(harness, []byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			if call.Event != "" || call.Harness != "agy" || call.Tool != "invoke_subagent" || call.CWD != "/tool" {
				t.Fatalf("call = %+v", call)
			}
			if Decide(call, "all").Allow {
				t.Fatal("tool call must reach the deny rule")
			}
			if verdict := DecideLane(call, &Lane{Access: "read"}); verdict.Kind != "deny" {
				t.Fatalf("Lane verdict = %+v", verdict)
			}
		}
	}
}

func TestReminderShapesAndScope(t *testing.T) {
	t.Setenv("MAESTRO_LANE", "")
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProjectFile(project), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(project, "nested", ".git")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	for _, harness := range []string{"", "claude", "codex", "agy"} {
		for _, tc := range []struct {
			name, scope, cwd string
			inScope          bool
		}{
			{"project", "projects", filepath.Dir(nested), true},
			{"outside", "projects", outside, false},
			{"all", "all", outside, true},
			{"no Roster", "", project, false},
		} {
			t.Run(harness+"/"+tc.name, func(t *testing.T) {
				payload := map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": tc.cwd, "prompt": "x"}
				if harness == "agy" {
					payload = map[string]any{"invocationNum": 1, "workspacePaths": []string{tc.cwd, outside}, "cwd": outside}
				}
				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				for _, arg := range []string{harness, ""} {
					call, err := Parse(arg, body)
					if err != nil {
						t.Fatal(err)
					}
					out := RenderReminder(call, tc.scope)
					var want any
					if call.Harness == "agy" {
						want = map[string]any{}
						if tc.inScope {
							want = map[string]any{"injectSteps": []any{map[string]any{"ephemeralMessage": adapter.Reminder}}}
						}
					} else if tc.inScope {
						want = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "UserPromptSubmit", "additionalContext": adapter.Reminder}}
					} else {
						if out != nil {
							t.Fatalf("out of scope: %s", out)
						}
						continue
					}
					var got any
					if err := json.Unmarshal(out, &got); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("output = %s, want %+v", out, want)
					}
				}
			})
		}
	}
}

func TestReminderAntigravityProcessDirectory(t *testing.T) {
	t.Setenv("MAESTRO_LANE", "")
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"", "agy"} {
		call, err := Parse(harness, []byte(`{"invocationNum":1}`))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(RenderReminder(call, "projects")); got != `{}` {
			t.Fatalf("outside project: %s", got)
		}
	}
	if err := os.WriteFile(config.ProjectFile(root), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"", "agy"} {
		call, err := Parse(harness, []byte(`{"invocationNum":1}`))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(RenderReminder(call, "projects")); !strings.Contains(got, `"injectSteps"`) {
			t.Fatalf("inside project: %s", got)
		}
	}
}

func TestFindLaneFolderClaudeAndCodexTranscript(t *testing.T) {
	for _, harness := range []string{"claude", "codex"} {
		t.Run(harness, func(t *testing.T) {
			t.Setenv("MAESTRO_TASK_FOLDER", "")
			folder := filepath.Join(t.TempDir(), ".agents", "maestro", "runs", "r-test", "task")
			transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
			body := transcriptPromptLine(t, harness, "Read the file `"+filepath.Join(folder, "brief.md")+"` in full, then follow it.")
			if err := os.WriteFile(transcript, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(map[string]any{"hook_event_name": "UserPromptSubmit", "transcript_path": transcript})
			call, err := Parse(harness, payload)
			if err != nil {
				t.Fatal(err)
			}
			if got, found := FindLaneFolder(call); !found || got != folder {
				t.Fatalf("folder = %q, %v; want %q", got, found, folder)
			}
			t.Setenv("MAESTRO_TASK_FOLDER", "/environment")
			if got, found := FindLaneFolder(call); !found || got != "/environment" {
				t.Fatalf("environment must take priority: %q, %v", got, found)
			}
		})
	}
}

func TestDenyReasons(t *testing.T) {
	solo := Decide(&Call{Tool: "mcp__solo__spawn_agent"}, "all")
	wantSolo := "Code-writing Solo lanes go through Maestro: write a Brief file and run `maestro dispatch <brief.md>`, or a Plan file and `maestro run <plan.md>`. A read-only spawn passes when its kind is terminal, or when extra_args carries a read token: `dontAsk`, `read-only`, or `--deny Write`."
	if solo.Allow || solo.Reason != wantSolo {
		t.Fatalf("Solo decision = %+v", solo)
	}
	sub := Decide(&Call{Tool: "Agent", Input: map[string]any{"subagent_type": "writer"}}, "all")
	wantSub := `Sub-agent "writer" may write code; Maestro routes that work. Write a Brief file and run ` + "`maestro dispatch <brief.md>`. Read-only legwork may stay on a read-only agent type: Explore, Plan, claude-code-guide, or a custom agent whose definition file keeps Edit, Write, NotebookEdit out of its tools."
	if sub.Allow || sub.Reason != wantSub {
		t.Fatalf("sub-agent decision = %+v", sub)
	}
	if !reflect.DeepEqual(readOnlyAgents, []string{"Explore", "Plan", "claude-code-guide"}) {
		t.Fatalf("read-only agent order = %v", readOnlyAgents)
	}
	for _, agent := range readOnlyAgents {
		if count := strings.Count(sub.Reason, agent); count != 1 {
			t.Fatalf("deny reason names %s %d times", agent, count)
		}
		for _, name := range []string{agent, strings.ToLower(agent), strings.ToUpper(agent)} {
			call := &Call{Tool: "Agent", Input: map[string]any{"subagent_type": name}}
			if !Decide(call, "all").Allow {
				t.Fatalf("read-only agent %s must pass", name)
			}
		}
	}
	for _, token := range []string{"dontAsk", "read-only", "--deny Write"} {
		call := &Call{Tool: "spawn_agent", Input: map[string]any{"extra_args": []any{token}}}
		if !Decide(call, "all").Allow || !strings.Contains(solo.Reason, fmt.Sprintf("`%s`", token)) {
			t.Fatalf("read token %s must pass and appear in the deny reason", token)
		}
	}
}
