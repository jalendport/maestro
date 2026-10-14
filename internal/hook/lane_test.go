package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
)

func TestParseAntigravityCwdAndArgs(t *testing.T) {
	for _, harness := range []string{"", "agy"} {
		c, err := Parse(harness, []byte(`{"cwd":"/outer","transcriptPath":"/brain/transcript.jsonl","workspacePaths":["/project","/extra"],"toolCall":{"name":"run_command","args":{"Cwd":"/project","CommandLine":"pwd","Blocking":true}}}`))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"Cwd": "/project", "CommandLine": "pwd", "Blocking": true}
		if c.Harness != "agy" || c.CWD != "/project" || c.Tool != "run_command" || !reflect.DeepEqual(c.Input, want) || c.TranscriptPath != "/brain/transcript.jsonl" || !reflect.DeepEqual(c.WorkspacePaths, []string{"/project", "/extra"}) {
			t.Fatalf("call: %+v", c)
		}
	}
	c, err := Parse("agy", []byte(`{"cwd":"/outer","toolCall":{"name":"run_command","args":{"CommandLine":"pwd"}}}`))
	if err != nil || c.CWD != "/outer" {
		t.Fatalf("outer cwd: %+v, %v", c, err)
	}
}

func TestLoadLane(t *testing.T) {
	folder := t.TempDir()
	if lane, ok := LoadLane(folder); ok || lane != nil {
		t.Fatalf("missing Lane: %+v, %v", lane, ok)
	}
	path := filepath.Join(folder, "lane.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if lane, ok := LoadLane(folder); ok || lane != nil {
		t.Fatalf("unreadable Lane: %+v, %v", lane, ok)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"not JSON", "null", `{}`, `{"access":"other"}`, `{"access":true}`} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if lane, ok := LoadLane(folder); ok || lane != nil {
			t.Fatalf("invalid Lane %q: %+v, %v", body, lane, ok)
		}
	}
	want := &Lane{Task: "abcd", Harness: "agy", Access: "edit", Root: "/project", Dirs: []string{"/extra"}, RunFolder: "/run", Allow: []string{"Bash(go run *)"}, Checks: []string{"go test ./..."}, Commit: true, Tab: true}
	body := `{"task":"abcd","harness":"agy","access":"edit","root":"/project","dirs":["/extra"],"run_folder":"/run","allow":["Bash(go run *)"],"checks":["go test ./..."],"commit":true,"tab":true}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if lane, ok := LoadLane(folder); !ok || !reflect.DeepEqual(lane, want) {
		t.Fatalf("Lane: %+v, %v", lane, ok)
	}
}

func TestDecideLaneSpawnsAndOtherTools(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Access: access}
		for _, tool := range []string{"invoke_subagent", "define_subagent", "manage_subagents"} {
			for _, agent := range []string{"Explore", "Plan", "coder", ""} {
				call := &Call{Harness: "agy", Tool: tool, Input: map[string]any{"agent": agent}}
				want := "deny"
				if agent == "Explore" || agent == "Plan" {
					want = "allow"
				}
				got := DecideLane(call, lane)
				if got.Kind != want || got.Kind == "deny" && got.Reason == "" {
					t.Fatalf("%s %s %s: %+v", access, tool, agent, got)
				}
			}
		}
		got := DecideLane(&Call{Harness: "agy", Tool: "read_file"}, lane)
		if got.Kind != "allow" {
			t.Fatalf("%s other tool: %+v", access, got)
		}
	}
}

func TestDecideLaneWrites(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Access: access, Root: "/project", Dirs: []string{"/extra"}, RunFolder: "/run"}
		for _, tool := range []string{"write_to_file", "replace_file_content", "multi_replace_file_content"} {
			for _, tc := range []struct {
				name, target, cwd string
				edit, read        bool
			}{
				{"root", "/project", "", true, false},
				{"project", "/project/src/file.go", "", true, false},
				{"dirs", "/extra/file.go", "", true, false},
				{"run", "/run/result.md", "", true, true},
				{"relative cwd", "result.md", "/run", true, true},
				{"relative root", "src/file.go", "", true, false},
				{"relative extra", "file.go", "/extra", true, false},
				{"cleaned", "/project/../run/result.md", "", true, true},
				{"outside", "/outside/file.go", "", false, false},
				{"sibling project", "/project-other/file.go", "", false, false},
				{"sibling dirs", "/extra-other/file.go", "", false, false},
				{"sibling run", "/run-other/file.go", "", false, false},
				{"relative escape", "../outside/file.go", "", false, false},
			} {
				t.Run(access+"/"+tool+"/"+tc.name, func(t *testing.T) {
					call := &Call{Harness: "agy", Tool: tool, Input: map[string]any{"TargetFile": tc.target, "Cwd": tc.cwd}}
					want := "deny"
					if access == "edit" && tc.edit || access == "read" && tc.read {
						want = "auto_approve"
					}
					got := DecideLane(call, lane)
					if got.Kind != want {
						t.Fatalf("verdict: %+v, want %s", got, want)
					}
					if want == "deny" && access == "read" && got.Reason != "read Lane: writes go to the run folder only" {
						t.Fatal(got)
					}
					deniedPath := tc.target
					if tc.name == "relative escape" {
						deniedPath = "/outside/file.go"
					}
					if want == "deny" && access == "edit" && !strings.Contains(got.Reason, deniedPath) {
						t.Fatalf("denial must name target: %+v", got)
					}
				})
			}
		}
	}
}

func TestDecideLaneReadOnlyCommands(t *testing.T) {
	commands := []string{"cat", "ls", "head", "tail", "grep", "rg", "find", "wc", "pwd", "echo", "which", "date", "diff", "tree", "sort", "uniq", "cut", "basename", "dirname", "stat", "realpath", "git log", "git diff", "git show", "git status", "git branch", "git blame"}
	for _, access := range []string{"read", "edit"} {
		for _, command := range commands {
			for _, suffix := range []string{"", " argument"} {
				call := &Call{Harness: "agy", Tool: "run_command", Input: map[string]any{"CommandLine": command + suffix}}
				if got := DecideLane(call, &Lane{Access: access, Root: "/project"}); got.Kind != "auto_approve" {
					t.Fatalf("%s %s: %+v", access, command+suffix, got)
				}
			}
		}
	}
}

func TestDecideLaneCommandPermissions(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		for _, tab := range []bool{false, true} {
			lane := &Lane{Access: access, Root: "/project", Tab: tab, Checks: []string{"go test ./..."}, Allow: []string{"Bash(go run *)", "Bash(make build)", "Write(*)", "Bash", "Bash()"}}
			for _, tc := range []struct {
				command string
				edit    bool
			}{
				{"go test ./...", true},
				{"go test ./... -race", true},
				{"go test ./...other", false},
				{"go run main.go", true},
				{"go runner main.go", false},
				{"go run", false},
				{"make build", true},
				{"make build extra", false},
				{"rm file.go", false},
				{"ls-other", false},
				{"git", false},
				{"", false},
			} {
				want := "deny"
				if access == "edit" {
					if tc.edit {
						want = "auto_approve"
					} else if tab {
						want = "relay"
					}
				}
				call := &Call{Harness: "agy", Tool: "run_command", Input: map[string]any{"CommandLine": tc.command}}
				got := DecideLane(call, lane)
				if got.Kind != want {
					t.Fatalf("%s tab=%v command=%q: %+v, want %s", access, tab, tc.command, got, want)
				}
				if want == "deny" {
					reason := "read Lane: command not on the read-only list"
					if access == "edit" {
						reason = "outside the Lane's allow list; add it to `checks` or the project `allow` list"
					}
					if got.Reason != reason {
						t.Fatalf("denial: %+v, want %s", got, reason)
					}
				}
			}
		}
	}
}

func TestDecideLaneGitWrites(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		for _, commit := range []bool{false, true} {
			for _, subcommand := range []string{"add", "commit", "push"} {
				for _, suffix := range []string{"", " argument"} {
					command := strings.Join([]string{"git", subcommand}, " ") + suffix
					lane := &Lane{Access: access, Root: "/project", Commit: commit}
					call := &Call{Harness: "agy", Tool: "run_command", Input: map[string]any{"CommandLine": command}}
					want := "deny"
					if access == "edit" && commit && subcommand != "push" {
						want = "auto_approve"
					}
					if got := DecideLane(call, lane); got.Kind != want {
						t.Fatalf("%s commit=%v %q: %+v, want %s", access, commit, command, got, want)
					}
				}
			}
		}
	}
}

func TestDecideLaneCompoundCommands(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Access: access, Root: "/project", Checks: []string{"go test ./..."}}
		for _, separator := range []string{"&&", "||", ";", "|"} {
			for _, command := range []string{"pwd " + separator + " ls", `echo "a && b || c ; d | e" ` + separator + " pwd", `echo 'a && b || c ; d | e' ` + separator + " pwd", "pwd " + separator + " rm file", "rm file " + separator + " pwd", "go test ./... " + separator + " pwd"} {
				want := "auto_approve"
				if strings.Contains(command, "rm file") || access == "read" && strings.HasPrefix(command, "go test") {
					want = "deny"
				}
				call := &Call{Harness: "agy", Tool: "run_command", Input: map[string]any{"CommandLine": command}}
				if got := DecideLane(call, lane); got.Kind != want {
					t.Fatalf("%s %q: %+v, want %s", access, command, got, want)
				}
			}
		}
	}
}

func TestRenderAntigravityVerdicts(t *testing.T) {
	for _, tc := range []struct {
		verdict Verdict
		want    string
	}{
		{Verdict{Kind: "allow"}, `{"decision":"allow"}`},
		{Verdict{Kind: "auto_approve"}, `{"decision":"auto_approve"}`},
		{Verdict{Kind: "deny", Reason: "outside <root>"}, `{"decision":"deny","reason":"outside <root>"}`},
	} {
		if got := string(Render("agy", tc.verdict)); got != tc.want {
			t.Fatalf("Render: %s, want %s", got, tc.want)
		}
	}
}

func TestFindLaneFolder(t *testing.T) {
	for _, tc := range []struct {
		name, env, harness, text, want string
	}{
		{"environment", "/env/r-test/task", "claude", "", "/env/r-test/task"},
		{"environment takes priority", "/env/r-test/task", "agy", "Read `/project/.agents/maestro/runs/r-test/abcd/brief.md`", "/env/r-test/task"},
		{"Task transcript", "", "agy", "{\"message\":\"Read the file `/project/.agents/maestro/runs/r-test/abcd/brief.md` in full, then follow it.\"}\n", "/project/.agents/maestro/runs/r-test/abcd"},
		{"Seat transcript", "", "agy", "{\"message\":\"Read the file `/project/.agents/maestro/councils/review/agy-flash/brief.md` in full, then follow it.\"}\n", "/project/.agents/maestro/councils/review/agy-flash"},
		{"no Pointer", "", "agy", "{\"message\":\"Hello\"}\n", ""},
		{"Claude path only", "", "claude", "Read `/project/.agents/maestro/runs/r-test/abcd/brief.md`", ""},
		{"empty transcript path", "", "claude", "", ""},
		{"first Pointer", "", "agy", "`/project/.agents/maestro/runs/r-test/abcd/brief.md` `/project/.agents/maestro/runs/r-next/efgh/brief.md`", "/project/.agents/maestro/runs/r-test/abcd"},
		{"within 64 KB", "", "agy", strings.Repeat("x", 63*1024) + " `/project/.agents/maestro/runs/r-test/abcd/brief.md`", "/project/.agents/maestro/runs/r-test/abcd"},
		{"after 64 KB", "", "agy", strings.Repeat("x", 64*1024) + " `/project/.agents/maestro/runs/r-test/abcd/brief.md`", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAESTRO_TASK_FOLDER", tc.env)
			transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
			if err := os.WriteFile(transcript, []byte(tc.text), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.name == "empty transcript path" {
				transcript = ""
			}
			got, ok := FindLaneFolder(&Call{Harness: tc.harness, TranscriptPath: transcript})
			if got != tc.want || ok != (tc.want != "") {
				t.Fatalf("folder = %q, %v; want %q", got, ok, tc.want)
			}
		})
	}
}

func transcriptPromptLine(t *testing.T, harness string, content any) string {
	t.Helper()
	message := map[string]any{"role": "user", "content": content}
	entry := map[string]any{"type": "user", "message": message}
	if harness == "codex" {
		message["type"] = "message"
		entry = map[string]any{"type": "response_item", "payload": message}
	}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	return string(body) + "\n"
}

// codexContextLine is a Codex user message that carries injected context of
// the given kinds. For Claude, whose transcripts have no kinds, it is an
// ordinary user prompt, which stops the scan.
func codexContextLine(t *testing.T, harness string, kinds ...string) string {
	t.Helper()
	if harness != "codex" {
		return transcriptPromptLine(t, harness, "Explain this code.")
	}
	var list []any
	for _, kind := range kinds {
		list = append(list, kind)
	}
	entry := map[string]any{"type": "response_item", "payload": map[string]any{
		"type": "message", "role": "user",
		"content": []any{map[string]any{"type": "input_text", "text": "Explain this code."}},
		"internal_chat_message_metadata_passthrough": map[string]any{"content_item_kinds": list},
	}}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	return string(body) + "\n"
}

func TestFindLaneFolderRealTranscriptShapes(t *testing.T) {
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	folder := "/project/.agents/maestro/runs/r-test/abcd"
	for _, harness := range []string{"claude", "codex", "grok"} {
		body, err := os.ReadFile(filepath.Join("testdata", harness+"-lane.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		for _, lane := range []bool{true, false} {
			t.Run(harness+"/Lane="+strconv.FormatBool(lane), func(t *testing.T) {
				text, want := string(body), folder
				if !lane {
					text = strings.Replace(text, brief.Pointer(folder+"/brief.md"), "Explain this code.", 1)
					want = ""
				}
				transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
				if err := os.WriteFile(transcript, []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
				got, found := FindLaneFolder(&Call{Harness: harness, TranscriptPath: transcript})
				if got != want || found != lane {
					t.Fatalf("folder = %q, %v; want %q, %v", got, found, want, lane)
				}
			})
		}
	}
}

func grokChunk(t *testing.T, index int, text string) string {
	t.Helper()
	entry := map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{
		"sessionUpdate": "user_message_chunk", "content": map[string]any{"type": "text", "text": text}, "_meta": map[string]any{"promptIndex": index},
	}}}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	return string(body) + "\n"
}

func TestFindLaneFolderGrokPrompts(t *testing.T) {
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	folder := "/project/.agents/maestro/runs/r-test/abcd"
	pointer := brief.Pointer(folder + "/brief.md")
	other := `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"` + pointer + `"}}}}` + "\n"
	for _, tc := range []struct {
		name, text string
		found      bool
	}{
		{"one chunk", grokChunk(t, 0, pointer), true},
		{"two chunks", grokChunk(t, 0, pointer[:20]) + grokChunk(t, 0, pointer[20:]), true},
		{"after other updates", other + grokChunk(t, 0, pointer), true},
		{"ordinary first prompt", grokChunk(t, 0, "Explain this code.") + other + grokChunk(t, 1, pointer), false},
		{"second prompt chunks do not join", grokChunk(t, 0, pointer[:20]) + grokChunk(t, 1, pointer[20:]), false},
		{"agent text only", other, false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transcript := filepath.Join(t.TempDir(), "updates.jsonl")
			if err := os.WriteFile(transcript, []byte(tc.text), 0o600); err != nil {
				t.Fatal(err)
			}
			want := ""
			if tc.found {
				want = folder
			}
			got, found := FindLaneFolder(&Call{Harness: "grok", TranscriptPath: transcript})
			if got != want || found != tc.found {
				t.Fatalf("folder = %q, %v; want %q, %v", got, found, want, tc.found)
			}
		})
	}
}

func TestDecideLaneGrokTools(t *testing.T) {
	run := "/project/.agents/maestro/runs/r-test/abcd"
	read := &Lane{Access: "read", Root: "/project", RunFolder: run}
	edit := &Lane{Access: "edit", Root: "/project", RunFolder: run, Checks: []string{"go test ./..."}}
	for _, tc := range []struct {
		name, tool string
		lane       *Lane
		input      map[string]any
		cwd, want  string
	}{
		{"report write", "write", read, map[string]any{"file_path": run + "/result.md", "content": "x"}, "/project", "auto_approve"},
		{"project write", "write", read, map[string]any{"file_path": "/project/main.go", "content": "x"}, "/project", "deny"},
		{"relative report edit", "search_replace", read, map[string]any{"file_path": "result.md", "old_string": "a", "new_string": "b"}, run, "auto_approve"},
		{"relative project edit", "Edit", read, map[string]any{"file_path": "main.go"}, "/project", "deny"},
		{"read-only command", "run_terminal_command", read, map[string]any{"command": "ls /project"}, "/project", "auto_approve"},
		{"write command", "run_terminal_command", read, map[string]any{"command": "touch /project/x"}, "/project", "deny"},
		{"command outside the Lane", "Bash", read, map[string]any{"command": "ls", "cwd": "/elsewhere"}, "/project", "deny"},
		{"writer spawn", "spawn_subagent", read, map[string]any{"subagent_type": "writer"}, "/project", "deny"},
		{"read-only spawn", "spawn_subagent", read, map[string]any{"subagent_type": "Explore"}, "/project", "allow"},
		{"solo spawn", "solo__spawn_agent", read, map[string]any{"extra_args": []any{"-s", "workspace-write"}}, "/project", "deny"},
		{"solo read spawn", "solo__spawn_agent", read, map[string]any{"extra_args": []any{"dontAsk"}}, "/project", "allow"},
		{"file read", "read_file", read, map[string]any{"target_file": "/etc/hosts"}, "/project", "allow"},
		{"edit project write", "write", edit, map[string]any{"file_path": "/project/main.go"}, "/project", "auto_approve"},
		{"edit check", "run_terminal_cmd", edit, map[string]any{"command": "go test ./..."}, "/project", "auto_approve"},
		{"edit other command", "run_terminal_cmd", edit, map[string]any{"command": "go build ./..."}, "/project", "deny"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := &Call{Harness: "grok", Tool: tc.tool, CWD: tc.cwd, Input: tc.input}
			if got := DecideLane(call, tc.lane); got.Kind != tc.want {
				t.Fatalf("%+v, want %s", got, tc.want)
			}
		})
	}
}

func TestFindLaneFolderFirstUserPromptAndLimits(t *testing.T) {
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	folder := "/project/.agents/maestro/runs/r-test/abcd"
	line := "{\"type\":\"system\",\"text\":\"" + strings.Repeat("x", 1024) + "\"}\n"
	for _, harness := range []string{"claude", "codex"} {
		pointer := transcriptPromptLine(t, harness, brief.Pointer(folder+"/brief.md"))
		toolResult := transcriptPromptLine(t, harness, []any{map[string]any{"type": "tool_result", "content": brief.Pointer(folder + "/brief.md")}})
		textBlocks := transcriptPromptLine(t, harness, []any{map[string]any{"type": "text", "text": brief.Pointer(folder + "/brief.md")}})
		for _, tc := range []struct {
			name, text string
			found      bool
		}{
			{"after 100 KB of system entries", strings.Repeat(line, 101) + pointer, true},
			{"3 MB JSONL line", "{\"type\":\"system\",\"text\":\"" + strings.Repeat("x", 3*1024*1024) + "\"}\n" + pointer, true},
			{"over 4 MB before prompt", strings.Repeat("x", 4*1024*1024) + "\n" + pointer, false},
			{"over 4 MB prompt", transcriptPromptLine(t, harness, brief.Pointer(folder+"/brief.md")+strings.Repeat("x", 4*1024*1024)), false},
			{"after 16 MB", strings.Repeat(line, 16*1024*1024/len(line)+1) + pointer, false},
			{"stop before oversized later line", pointer + strings.Repeat("x", 4*1024*1024), true},
			{"ordinary first prompt", transcriptPromptLine(t, harness, "Explain this code.") + toolResult + pointer, false},
			{"empty first prompt", transcriptPromptLine(t, harness, "") + pointer, false},
			{"text blocks", textBlocks, true},
			{"tool result before prompt", toolResult + textBlocks, true},
			{"tool result only", toolResult, false},
			{"non-text user message", transcriptPromptLine(t, harness, []any{map[string]any{"type": "image"}}) + pointer, true},
			{"context kinds before prompt", codexContextLine(t, harness, "host_skills.instructions") + codexContextLine(t, harness, "agents_md.instructions", "environments.environment_context") + pointer, harness == "codex"},
			{"context kind beside user text", codexContextLine(t, harness, "agents_md.instructions", "user.text") + pointer, false},
			{"invalid JSON before prompt", "not JSON\n" + pointer, true},
			{"unreadable transcript", "", false},
		} {
			t.Run(harness+"/"+tc.name, func(t *testing.T) {
				transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
				if err := os.WriteFile(transcript, []byte(tc.text), 0o600); err != nil {
					t.Fatal(err)
				}
				if tc.name == "unreadable transcript" {
					transcript = t.TempDir()
				}
				want := ""
				if tc.found {
					want = folder
				}
				got, found := FindLaneFolder(&Call{Harness: harness, TranscriptPath: transcript})
				if got != want || found != tc.found {
					t.Fatalf("folder = %q, %v; want %q, %v", got, found, want, tc.found)
				}
			})
		}
	}
}

func TestFindLaneFolderPointerSentence(t *testing.T) {
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	for _, harness := range []string{"claude", "codex", "agy"} {
		for _, folder := range []string{
			"/project/.agents/maestro/runs/r-test/abcd",
			"/project/.agents/maestro/councils/review/agy-flash",
			"/project/.agents/maestro/councils/review/round-2/agy-flash",
			".agents/maestro/runs/r-test/abcd", // a headless Lane's Pointer is project-relative
		} {
			path := folder + "/brief.md"
			for _, tc := range []struct {
				name, text string
				pointer    bool
			}{
				{"path only", path, false},
				{"cat", "cat " + path, false},
				{"partial sentence", "Read the file `" + path + "` in full", false},
				{"missing period", "Read the file `" + path + "` in full, then follow it", false},
				{"Pointer", brief.Pointer(path), true},
				{"leading whitespace", " \t\n" + brief.Pointer(path), true},
				{"quoted Pointer", "Explain this prompt: " + brief.Pointer(path), false},
				{"earlier path", "/other/.agents/maestro/runs/r-other/task/brief.md\n" + brief.Pointer(path), false},
			} {
				t.Run(harness+"/"+folder+"/"+tc.name, func(t *testing.T) {
					body := transcriptPromptLine(t, harness, tc.text)
					transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
					if err := os.WriteFile(transcript, []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
					want := ""
					if tc.pointer || harness == "agy" {
						want = folder
					}
					if harness == "agy" && tc.name == "earlier path" {
						want = "/other/.agents/maestro/runs/r-other/task"
					}
					got, ok := FindLaneFolder(&Call{Harness: harness, TranscriptPath: transcript})
					if got != want || ok != (want != "") {
						t.Fatalf("folder = %q, %v; want %q", got, ok, want)
					}
				})
			}
		}
	}
}

func TestDecideLaneUnsafeShell(t *testing.T) {
	for _, tc := range []struct{ command, reason string }{
		{"pwd\nrm file", "newline"}, {"pwd & rm file", "single &"},
		{"echo $(touch file)", "$("}, {`echo "$(touch file)"`, "$("},
		{"echo `touch file`", "`"}, {"cat <(touch file)", "<("}, {"echo >(touch file)", ">("},
		{"echo x > file", ">"}, {"echo x >> file", ">>"}, {"cat < file", "<"},
		{"echo x 2> file", "2>"}, {"echo x &> file", "&>"},
	} {
		for _, access := range []string{"read", "edit"} {
			lane := &Lane{Access: access, Root: "/project", Tab: true, Allow: []string{"Bash(echo *)", "Bash(cat *)", "Bash(pwd *)"}}
			got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": tc.command}}, lane)
			if got.Kind != "deny" || !strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("%s %q = %+v, want deny naming %q", access, tc.command, got, tc.reason)
			}
		}
	}
	for _, command := range []string{`echo 'a > b < c & d'`, `echo "a >> b & c"`, `echo a\>b`, "pwd && ls || cat file | wc ; echo done"} {
		if got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": command}}, &Lane{Access: "read", Root: "/project"}); got.Kind != "auto_approve" {
			t.Fatalf("safe command %q = %+v", command, got)
		}
	}
}

func TestDecideLaneReadOnlyWriteFlags(t *testing.T) {
	commands := map[string][]string{
		"find .":     {"-exec", "-execdir", "-delete", "-fprint", "-fprintf", "-fls", "-ok", "-okdir"},
		"sort":       {"-o", "--output", "--output=file", "-ofile", "'-o'"},
		"tree":       {"-o", "-ofile"},
		"git diff":   {"--output", "--output=file", `"--output=file"`},
		"git log":    {"--output", "--output=file"},
		"git show":   {"--output", "--output=file"},
		"git branch": {"-d", "-D", "-m", "-M", "-c", "-C", "-f", "--delete", "--move", "--copy", "--force", "--set-upstream-to", "--unset-upstream", "--edit-description", "--set-upstream-to=main", "-vD"},
	}
	for command, flags := range commands {
		for _, flag := range flags {
			for _, access := range []string{"read", "edit"} {
				line := "pwd && " + command + " " + flag
				lane := &Lane{Access: access, Root: "/project", Tab: true, Checks: []string{command}}
				if got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": line}}, lane); got.Kind != "deny" || !strings.Contains(got.Reason, "flag") {
					t.Fatalf("%s %q = %+v", access, line, got)
				}
			}
		}
	}
	for _, command := range []string{"env rm file", "file -C"} {
		if got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": command}}, &Lane{Access: "read", Root: "/project"}); got.Kind != "deny" {
			t.Fatalf("removed command %q = %+v", command, got)
		}
	}
}

func TestInsideResolvesSymlinksAndMissingParents(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if inside(filepath.Join(link, "new", "file.go"), root) {
		t.Fatal("symlink escape is inside the allowed folder")
	}
	if !inside(filepath.Join(root, "new", "file.go"), root) {
		t.Fatal("missing parents inside the allowed folder were denied")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if !inside(filepath.Join(root, "new", "file.go"), alias) {
		t.Fatal("allowed folder symlink was not resolved")
	}
}

func TestInsideTmpAndPrivateTmp(t *testing.T) {
	resolved, err := filepath.EvalSymlinks("/tmp")
	if err != nil || resolved != "/private/tmp" {
		t.Skip("/tmp is not a symlink to /private/tmp")
	}
	root, err := os.MkdirTemp("/tmp", "maestro-inside-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	private := filepath.Join("/private/tmp", filepath.Base(root))
	if !inside(filepath.Join(private, "new", "file"), root) || !inside(filepath.Join(root, "new", "file"), private) {
		t.Fatal("/tmp and /private/tmp did not match")
	}
}

func TestDecideLaneCommandCwd(t *testing.T) {
	root, extra, run, outside := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Access: access, Root: root, Dirs: []string{extra}, RunFolder: run, Tab: true}
		for _, cwd := range []string{root, extra, run, outside, link} {
			got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": "pwd", "Cwd": cwd}}, lane)
			want := "auto_approve"
			if cwd == outside || cwd == link {
				want = "deny"
			}
			if got.Kind != want {
				t.Fatalf("%s Cwd=%q = %+v, want %s", access, cwd, got, want)
			}
		}
	}
}

func TestRenderOtherHarnessLaneVerdicts(t *testing.T) {
	for _, harness := range []string{"claude", "codex", "grok"} {
		if got, want := string(Render(harness, Verdict{Kind: "auto_approve"})), string(Render(harness, Verdict{Kind: "allow"})); got != want {
			t.Fatalf("%s auto_approve = %s, want %s", harness, got, want)
		}
		got := string(Render(harness, Verdict{Kind: "relay"}))
		if !strings.Contains(got, "deny") || !strings.Contains(got, "relay is only for Antigravity") {
			t.Fatalf("%s relay = %s", harness, got)
		}
	}
}

func TestFindLaneFolderChallengeRound(t *testing.T) {
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	transcript := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(transcript, []byte("{\"text\":\"Read the file `/p/.agents/maestro/councils/slug/round-2/grok/brief.md` in full\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := FindLaneFolder(&Call{Harness: "agy", TranscriptPath: transcript})
	if !ok || got != "/p/.agents/maestro/councils/slug/round-2/grok" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}
