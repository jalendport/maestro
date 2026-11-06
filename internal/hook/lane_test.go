package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/harness"
)

func TestRecordJudgmentUsesRunFolderOnlyWhenControlWriteFails(t *testing.T) {
	for _, writable := range []bool{true, false} {
		t.Run(strconv.FormatBool(writable), func(t *testing.T) {
			control, folder := t.TempDir(), t.TempDir()
			if !writable {
				if os.Geteuid() == 0 {
					t.Skip("root can write a folder with no write permission")
				}
				if err := os.Chmod(control, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(control, 0o700) })
			}
			started := time.Now()
			call := &Call{Harness: "grok", Tool: "run_terminal_command"}
			if err := RecordJudgment(control, folder, call, Verdict{Kind: "auto_approve"}); err != nil {
				t.Fatal(err)
			}
			found, absent := control, folder
			if !writable {
				found, absent = folder, control
			}
			b, err := os.ReadFile(filepath.Join(found, FileJudgment))
			var judgment Judgment
			if err != nil || json.Unmarshal(b, &judgment) != nil || judgment.Harness != "grok" || judgment.Tool != call.Tool || judgment.Verdict != "auto_approve" || judgment.At.Before(started) {
				t.Fatalf("judgment = %+v, error = %v", judgment, err)
			}
			if _, err := os.Stat(filepath.Join(absent, FileJudgment)); !os.IsNotExist(err) {
				t.Fatalf("second record: %v", err)
			}
			if err := RecordJudgment(control, folder, call, Verdict{Kind: "deny"}); err != nil {
				t.Fatal(err)
			}
			kept, err := os.ReadFile(filepath.Join(found, FileJudgment))
			if err != nil || string(kept) != string(b) {
				t.Fatalf("first judgment changed: %s, %v", kept, err)
			}
		})
	}
}

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
	want := &Lane{Task: "abcd", Harness: "agy", Access: "edit", Root: "/project", Dirs: []string{"/extra"}, RunFolder: "/run", Allow: []string{"Bash(go run *)"}, Checks: []string{"go test ./..."}}
	body := `{"task":"abcd","harness":"agy","access":"edit","root":"/project","dirs":["/extra"],"run_folder":"/run","allow":["Bash(go run *)"],"checks":["go test ./..."],"commit":true}`
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
					if want == "deny" && access == "edit" && !strings.Contains(got.Reason, deniedPath) {
						t.Fatalf("denial must name target: %+v", got)
					}
				})
			}
		}
	}
}

func TestDecideLaneReadOnlyCommands(t *testing.T) {
	commands := []string{"cat file", "ls .", "head file", "tail file", "grep pattern file", "rg pattern .", "find . -name '*.go'", "wc file", "pwd", "echo text", "which go", "date", "diff a b", "tree .", "sort file", "uniq file", "cut -f 1 file", "basename file", "dirname file", "stat file", "realpath file", "git log -- file", "git diff -- file", "git show HEAD", "git status", "git branch --list 'feature*'", "git blame file"}
	for _, access := range []string{"read", "edit"} {
		for _, command := range commands {
			call := &Call{Harness: "agy", Tool: "run_command", Input: map[string]any{"CommandLine": command}}
			if got := DecideLane(call, &Lane{Access: access, Root: "/project"}); got.Kind != "auto_approve" {
				t.Fatalf("%s %s: %+v", access, command, got)
			}
		}
	}
}

func TestDecideLaneCommandPermissions(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Access: access, Root: "/project", Checks: []string{"go test ./..."}, Allow: []string{"Bash(go run *)", "Bash(make build)", "Write(*)", "Bash", "Bash()"}}
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
				} else {
					want = "relay"
				}
			}
			call := &Call{Harness: "agy", Tool: "run_command", Input: map[string]any{"CommandLine": tc.command}}
			got := DecideLane(call, lane)
			if got.Kind != want {
				t.Fatalf("%s command=%q: %+v, want %s", access, tc.command, got, want)
			}
			if want == "deny" {
				reason := "read Lane: command not on the read-only list; accepted commands: " + readOnlyCommandNames()
				if access == "edit" {
					reason = "outside the Lane's allow list; add it to `checks` or the project `allow` list; accepted read-only commands: " + readOnlyCommandNames()
				}
				if got.Reason != reason {
					t.Fatalf("denial: %+v, want %s", got, reason)
				}
			}
		}
	}
}

func TestDecideLaneEnvironmentPrefixes(t *testing.T) {
	for _, prefix := range []string{
		"env ", "env -u MAESTRO_HOME ", "env -i ", "env CI=1 ", "CI=1 NO_COLOR=1 ",
		"CI=1 env -i -u MAESTRO_HOME -u OTHER NO_COLOR=1 env -u THIRD ",
		`TERM="a b" env -u "MAESTRO_HOME" LANG='c d' `, "TERM=a\\ b\tenv\t-i\t",
	} {
		for _, access := range []string{"read", "edit"} {
			for _, tc := range []struct {
				command string
				read    bool
			}{
				{"go test ./...", false}, {"go vet ./...", false}, {"go build ./...", false},
				{"make build", false}, {"npm test", false},
				{"git status", true}, {"go env GOPATH", true}, {"[ -f main.go ]", true},
			} {
				t.Run(access+"/"+prefix+tc.command, func(t *testing.T) {
					lane := &Lane{Access: access, Root: "/project", Checks: []string{"npm test"}, Allow: []string{
						"Bash(go test *)", "Bash(go vet *)", "Bash(go build *)", "Bash(make build)",
					}}
					command := prefix + tc.command
					call := &Call{Harness: "grok", Tool: "Bash", Input: map[string]any{"command": command}}
					want := "deny"
					if access == "edit" || tc.read {
						want = "auto_approve"
					}
					if got := DecideLane(call, lane); got.Kind != want {
						t.Fatalf("verdict = %+v, want %s", got, want)
					}
					if call.Input["command"] != command {
						t.Fatal("command changed during matching")
					}
				})
			}
		}
	}
}

func TestDecideLaneHarmlessEnvironmentNames(t *testing.T) {
	for _, name := range []string{"CI", "NO_COLOR", "FORCE_COLOR", "TERM", "COLUMNS", "LINES", "LANG", "LC_ALL", "TZ", "CGO_ENABLED", "GOOS", "GOARCH", "MAESTRO_HOME"} {
		command := name + "=1 git status"
		got := DecideLane(&Call{Tool: "Bash", Input: map[string]any{"command": command}}, &Lane{Access: "read", Root: "/project"})
		if got.Kind != "auto_approve" {
			t.Fatalf("%s: %+v", command, got)
		}
	}
}

func TestDecideLaneOtherEnvironmentNames(t *testing.T) {
	for _, tc := range []struct{ command, variable string }{
		{"GIT_EXTERNAL_DIFF=x git diff", "GIT_EXTERNAL_DIFF"},
		{`GIT_EXTERNAL_DIFF='sh x.sh' git diff`, "GIT_EXTERNAL_DIFF"},
		{"GIT_CONFIG_COUNT=1 git status", "GIT_CONFIG_COUNT"},
		{`GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=core.fsmonitor GIT_CONFIG_VALUE_0='sh x.sh' git status`, "GIT_CONFIG_COUNT"},
		{"GOFLAGS=-toolexec=x go list ./...", "GOFLAGS"},
		{"env GOFLAGS=-toolexec=x go list ./...", "GOFLAGS"},
		{"CI=1 env -i -u MAESTRO_HOME GOFLAGS=-toolexec=x go list ./...", "GOFLAGS"},
		{"PATH=./bin git status", "PATH"},
		{"NODE_OPTIONS=--require=./x.js node --version", "NODE_OPTIONS"},
		{"FOO=1 git status", "FOO"},
		{"ci=1 git status", "ci"},
		{"env -u GOFLAGS CI=1 GOFLAGS= go list ./...", "GOFLAGS"},
		{"pwd && GOFLAGS=-toolexec=x go list ./...", "GOFLAGS"},
	} {
		for _, harness := range []string{"claude", "agy", "grok", "codex"} {
			for _, access := range []string{"read", "edit"} {
				t.Run(harness+"/"+access+"/"+tc.command, func(t *testing.T) {
					lane := &Lane{Harness: harness, Access: access, Root: "/project", Checks: []string{"go list ./..."}, Allow: []string{"Bash(git *)", "Bash(node *)"}}
					got := DecideLane(&Call{Harness: harness, Tool: "Bash", Input: map[string]any{"command": tc.command}}, lane)
					want := "deny"
					if access == "edit" && (harness == "claude" || harness == "agy") {
						want = "relay"
					}
					if got.Kind != want || !strings.Contains(got.Reason, "environment variable is not on the harmless list: "+tc.variable) {
						t.Fatalf("verdict = %+v, want %s naming %s", got, want, tc.variable)
					}
				})
			}
		}
	}
	command := "GOFLAGS=-toolexec=x go list ./..."
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Access: access, Root: "/project", Checks: []string{command}}
		got := DecideLane(&Call{Tool: "Bash", Input: map[string]any{"command": command}}, lane)
		want := "deny"
		if access == "edit" {
			want = "auto_approve"
		}
		if got.Kind != want {
			t.Fatalf("explicit environment check %s: %+v, want %s", access, got, want)
		}
	}
}

func TestDecideLaneEnvironmentPrefixLimits(t *testing.T) {
	lane := &Lane{Access: "edit", Root: "/project", Allow: []string{"Bash(go test *)", "Bash(make build)"}}
	for _, command := range []string{
		"sudo go test ./...", "nohup go test ./...", "time go test ./...",
		"env sudo go test ./...", "FOO=1 nohup go test ./...", "env -i time go test ./...",
		"env -S 'go test ./...'", "env --chdir=/other go test ./...",
		"1FOO=1 go test ./...", "FOO-BAR=1 go test ./...", "=1 go test ./...",
		"env", "env -u", "env -i", "FOO=1 BAR=2", "env -u MAESTRO_HOME",
		"env go testing ./...", "env go test", "FOO=1 make build extra",
		`env "go test" ./...`, `FOO=1 make "build"`,
		"env go test ./... && sudo go test ./...",
	} {
		t.Run(command, func(t *testing.T) {
			call := &Call{Harness: "grok", Tool: "Bash", Input: map[string]any{"command": command}}
			if got := DecideLane(call, lane); got.Kind != "deny" {
				t.Fatalf("verdict = %+v, want deny", got)
			}
		})
	}
	command := "env -u MAESTRO_HOME go test ./... && CGO_ENABLED=0 go test ./..."
	if got := DecideLane(&Call{Tool: "Bash", Input: map[string]any{"command": command}}, lane); got.Kind != "auto_approve" {
		t.Fatalf("compound command = %+v", got)
	}
}

func TestDecideLaneEnvironmentPrefixWriteFlags(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		for _, command := range []string{
			"env -u MAESTRO_HOME git diff --output=file", "CI=1 sort '-o' file",
			"env -i go env -w GOPROXY=off", "env CI=1 find . -delete",
			"CI=1 env -i node --version --eval 'write()'", "env -i sed 'w output' file",
			`CI=1 cat "$FILE"`, `CI=$VALUE cat file`, `CI=$VALUE [ -f file ]`,
		} {
			t.Run(access+"/"+command, func(t *testing.T) {
				lane := &Lane{Access: access, Root: "/project", Checks: []string{command}, Allow: []string{"Bash(" + command + ")"}}
				got := DecideLane(&Call{Tool: "Bash", Input: map[string]any{"command": command}}, lane)
				if got.Kind != "deny" || !strings.Contains(got.Reason, "read-only command carries") {
					t.Fatalf("verdict = %+v, want a write flag or form denial", got)
				}
			})
		}
	}
}

func TestDecideLaneGitWrites(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		// The Lane never commits, so no Lane file approves these commands.
		for _, subcommand := range []string{"add", "commit", "push"} {
			for _, suffix := range []string{"", " argument"} {
				command := strings.Join([]string{"git", subcommand}, " ") + suffix
				lane := &Lane{Access: access, Root: "/project"}
				call := &Call{Harness: "agy", Tool: "run_command", Input: map[string]any{"CommandLine": command}}
				want := "deny"
				if access == "edit" {
					want = "relay"
				}
				if got := DecideLane(call, lane); got.Kind != want {
					t.Fatalf("%s %q: %+v, want %s", access, command, got, want)
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
					if access == "edit" {
						want = "relay"
					}
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
		{"Task transcript", "", "agy", transcriptPromptLine(t, "agy", brief.Pointer("/project/.agents/maestro/runs/r-test/abcd/brief.md")), "/project/.agents/maestro/runs/r-test/abcd"},
		{"Seat transcript", "", "agy", transcriptPromptLine(t, "agy", brief.Pointer("/project/.agents/maestro/councils/review/agy-flash/brief.md")), "/project/.agents/maestro/councils/review/agy-flash"},
		{"no Pointer", "", "agy", "{\"message\":\"Hello\"}\n", ""},
		{"Claude path only", "", "claude", "Read `/project/.agents/maestro/runs/r-test/abcd/brief.md`", ""},
		{"empty transcript path", "", "claude", "", ""},
		{"bare paths", "", "agy", "`/project/.agents/maestro/runs/r-test/abcd/brief.md` `/project/.agents/maestro/runs/r-next/efgh/brief.md`", ""},
		{"within 64 KB", "", "agy", strings.Repeat("x", 63*1024) + " `/project/.agents/maestro/runs/r-test/abcd/brief.md`", ""},
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
			got, ok, err := FindLaneFolder(&Call{Harness: tc.harness, TranscriptPath: transcript})
			if err != nil || got != tc.want || ok != (tc.want != "") {
				t.Fatalf("folder = %q, %v, %v; want %q", got, ok, err, tc.want)
			}
		})
	}
}

func TestFindOneShotLaneForEachHarness(t *testing.T) {
	for _, name := range []string{"claude", "codex", "grok", "agy"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			folder := filepath.Join(root, ".agents", "maestro", "runs", "r-test", "abcd")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			reg := harness.New(map[string]string{name: "/bin/" + name})
			h, _ := reg.Get(name)
			command, err := h.Build(harness.Launch{
				Access: "read", Root: root, RunFolder: folder, ReportPath: filepath.Join(folder, "result.md"),
				Pointer: brief.Pointer(filepath.Join(folder, "brief.md")),
				Env:     []string{"MAESTRO_TASK_FOLDER=" + folder},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, arg := range command.Cmd.Args {
				if arg == "--trust" || arg == "--prompt-interactive" || strings.Contains(arg, "trust_level") {
					t.Fatalf("interactive launch argument: %s", arg)
				}
			}
			envFolder := ""
			for _, kv := range command.Cmd.Env {
				if value, ok := strings.CutPrefix(kv, "MAESTRO_TASK_FOLDER="); ok {
					envFolder = value
				}
			}
			if envFolder != folder {
				t.Fatalf("one-shot environment has no Lane folder: %q", command.Cmd.Env)
			}
			if name == "claude" {
				var settings struct {
					Env map[string]string `json:"env"`
				}
				for i, arg := range command.Cmd.Args {
					if arg == "--settings" {
						if err := json.Unmarshal([]byte(command.Cmd.Args[i+1]), &settings); err != nil {
							t.Fatal(err)
						}
					}
				}
				envFolder = settings.Env["MAESTRO_TASK_FOLDER"]
				if envFolder != folder {
					t.Fatal("Claude settings have no Lane folder")
				}
			}
			transcript := ""
			if name == "grok" {
				// A Grok hook can run without the parent Lane environment.
				// Its payload still names the one-shot session transcript.
				envFolder = ""
				transcript = filepath.Join(t.TempDir(), "updates.jsonl")
				pointer, err := os.ReadFile(filepath.Join(folder, "pointer.txt"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(transcript, []byte(grokChunk(t, 0, string(pointer))), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("MAESTRO_TASK_FOLDER", envFolder)
			payload := map[string]any{"cwd": root, "transcript_path": transcript}
			if name == "grok" || name == "agy" {
				payload = map[string]any{"cwd": root, "transcriptPath": transcript}
				if name == "agy" {
					payload["toolCall"] = map[string]any{"name": "view_file", "args": map[string]any{}}
				}
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			call, err := Parse(name, body)
			if err != nil {
				t.Fatal(err)
			}
			got, found, err := FindLaneFolder(call)
			if err != nil || !found || got != folder {
				t.Fatalf("one-shot Lane folder = %q, %v, %v; want %q", got, found, err, folder)
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
	if harness == "agy" {
		if text, ok := content.(string); ok {
			content = "<USER_REQUEST>\n" + text + "\n</USER_REQUEST>"
		}
		entry = map[string]any{"type": "USER_INPUT", "source": "USER_EXPLICIT", "content": content}
	}
	if harness == "grok" {
		text, _ := content.(string)
		return grokChunk(t, 0, text)
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
	for _, harness := range []string{"claude", "codex", "grok", "agy"} {
		folder := "/project/.agents/maestro/runs/r-test/abcd"
		pointer := brief.Pointer(folder + "/brief.md")
		if harness == "agy" {
			folder = "/project/.agents/maestro/runs/r-0004/9319"
			pointer = brief.Pointer(".agents/maestro/runs/r-0004/9319/brief.md")
		}
		body, err := os.ReadFile(filepath.Join("testdata", harness+"-lane.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		for _, lane := range []bool{true, false} {
			t.Run(harness+"/Lane="+strconv.FormatBool(lane), func(t *testing.T) {
				text, want := string(body), folder
				if !lane {
					text = strings.Replace(text, pointer, "Explain this code.", 1)
					want = ""
				}
				transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
				if err := os.WriteFile(transcript, []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
				got, found, err := FindLaneFolder(&Call{Harness: harness, CWD: "/project/sub", WorkspacePaths: []string{"/project"}, TranscriptPath: transcript})
				if err != nil || got != want || found != lane {
					t.Fatalf("folder = %q, %v, %v; want %q, %v", got, found, err, want, lane)
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
		{"stop before oversized later line", grokChunk(t, 0, pointer) + strings.Repeat("x", 4*1024*1024), true},
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
			got, found, err := FindLaneFolder(&Call{Harness: "grok", TranscriptPath: transcript})
			if err != nil || got != want || found != tc.found {
				t.Fatalf("folder = %q, %v, %v; want %q, %v", got, found, err, want, tc.found)
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
	for _, harness := range []string{"claude", "codex", "agy"} {
		pointer := transcriptPromptLine(t, harness, brief.Pointer(folder+"/brief.md"))
		toolResult := transcriptPromptLine(t, harness, []any{map[string]any{"type": "tool_result", "content": brief.Pointer(folder + "/brief.md")}})
		textBlocks := transcriptPromptLine(t, harness, []any{map[string]any{"type": "text", "text": brief.Pointer(folder + "/brief.md")}})
		if harness == "agy" {
			textBlocks = pointer
			toolResult = "{\"type\":\"GENERIC\",\"source\":\"MODEL\",\"content\":\"tool result\"}\n"
		}
		for _, tc := range []struct {
			name, text string
			found      bool
		}{
			{"after 100 KB of system entries", strings.Repeat(line, 101) + pointer, true},
			{"100 KB first user prompt", transcriptPromptLine(t, harness, strings.Repeat(" ", 101*1024)+brief.Pointer(folder+"/brief.md")), true},
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
			{"non-text user message", transcriptPromptLine(t, harness, []any{map[string]any{"type": "image"}}) + pointer, harness != "agy"},
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
				got, found, err := FindLaneFolder(&Call{Harness: harness, TranscriptPath: transcript})
				wantError := strings.HasPrefix(tc.name, "over 4 MB") || tc.name == "after 16 MB" || tc.name == "unreadable transcript"
				if got != want || found != tc.found || (err != nil) != wantError {
					t.Fatalf("folder = %q, %v, %v; want %q, %v, error=%v", got, found, err, want, tc.found, wantError)
				}
			})
		}
	}
}

func TestFindLaneFolderPointerSentence(t *testing.T) {
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	for _, harness := range []string{"claude", "codex", "grok", "agy"} {
		for _, folder := range []string{
			"/project/.agents/maestro/runs/r-test/abcd",
			"/project/.agents/maestro/councils/review/agy-flash",
			"/project/.agents/maestro/councils/review/round-2/agy-flash",
			"/project with spaces/quotes '\"/.agents/maestro/runs/r-test/abcd",
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
					if tc.pointer {
						want = folder
						if !filepath.IsAbs(want) {
							want = filepath.Join("/project", want)
						}
					}
					got, ok, err := FindLaneFolder(&Call{Harness: harness, CWD: "/project", TranscriptPath: transcript})
					if err != nil || got != want || ok != (want != "") {
						t.Fatalf("folder = %q, %v, %v; want %q", got, ok, err, want)
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
		{"echo `touch file`", "`"}, {"cat <(touch file)", "<("}, {"echo >(touch file)", ">("}, {"cat =(touch file)", "=("},
		{"echo x > file", ">"}, {"echo x >> file", ">>"}, {"cat < file", "<"},
		{"echo x 2> file", "2>"}, {"echo x &> file", "&>"}, {"echo x 2>&1 > file", ">"},
	} {
		for _, access := range []string{"read", "edit"} {
			lane := &Lane{Access: access, Root: "/project", Allow: []string{"Bash(echo *)", "Bash(cat *)", "Bash(pwd *)"}}
			got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": tc.command}}, lane)
			if got.Kind != "deny" || !strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("%s %q = %+v, want deny naming %q", access, tc.command, got, tc.reason)
			}
		}
	}
	for _, command := range []string{`echo 'a > b < c & d'`, `echo "a >> b & c"`, `echo a\>b`, "pwd && ls || cat file | wc ; echo done", "cat file 2>&1 | wc -l"} {
		if got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": command}}, &Lane{Access: "read", Root: "/project"}); got.Kind != "auto_approve" {
			t.Fatalf("safe command %q = %+v", command, got)
		}
	}
}

func TestDecideLaneReadOnlyWriteFlags(t *testing.T) {
	commands := map[string][]string{
		"find .":     {"-exec", "-execdir", "-delete", "-fprint", "-fprint0", "-fprintf", "-fls", "-ok", "-okdir"},
		"rg":         {"--pre", "--pre=python3", "--pre-glob", "--hostname-bin"},
		"sort":       {"-o", "--output", "--output=file", "-ofile", "'-o'", "--compress-program"},
		"tree":       {"-o", "-ofile", "-R", "-H"},
		"git diff":   {"--output", "--output=file", `"--output=file"`, "--ext-diff", "--textconv"},
		"git log":    {"--output", "--output=file", "--ext-diff", "--textconv"},
		"git show":   {"--output", "--output=file", "--ext-diff", "--textconv"},
		"git branch": {"-d", "-D", "-m", "-M", "-c", "-C", "-f", "--delete", "--move", "--copy", "--force", "--set-upstream-to", "--unset-upstream", "--edit-description", "--set-upstream-to=main", "-vD"},
	}
	for command, flags := range commands {
		for _, flag := range flags {
			for _, access := range []string{"read", "edit"} {
				line := "pwd && " + command + " " + flag
				lane := &Lane{Access: access, Root: "/project", Checks: []string{command}}
				if got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": line}}, lane); got.Kind != "deny" || !strings.Contains(got.Reason, strings.Trim(flag, "'\"")) {
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

func TestDecideLaneLongWriteFlagPrefixes(t *testing.T) {
	for name, flags := range readOnlyCommands {
		for _, flag := range flags {
			if !strings.HasPrefix(flag, "--") {
				continue
			}
			for length := 3; length <= len(flag); length++ {
				option := flag[:length]
				if option == "--text" {
					continue
				}
				for _, suffix := range []string{"", "=value"} {
					command := name + " " + option + suffix
					for _, access := range []string{"read", "edit"} {
						lane := &Lane{Access: access, Root: "/project", Checks: []string{name}}
						got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": command}}, lane)
						if got.Kind != "deny" || !strings.Contains(got.Reason, option) {
							t.Fatalf("%s %q = %+v", access, command, got)
						}
					}
				}
			}
		}
	}
}

func TestDecideLaneReadOnlyCommandForms(t *testing.T) {
	for _, tc := range []struct {
		command, reason string
	}{
		{"uniq input output", "two operands"},
		{"uniq -f 1 input output", "two operands"},
		{"uniq -- input output", "two operands"},
		{"uniq -- input --skip-fields=output", "two operands"},
		{"git branch new", "new"},
		{"git branch old new", "old"},
		{"git branch --show-current new", "new"},
		{"git branch -a new", "new"},
		{"git branch --contains HEAD new", "new"},
		{"git branch --list pattern other", "other"},
		{"git branch -- new", "new"},
	} {
		for _, access := range []string{"read", "edit"} {
			lane := &Lane{Access: access, Root: "/project", Checks: []string{"uniq", "git branch"}}
			got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": tc.command}}, lane)
			if got.Kind != "deny" || !strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("%s %q = %+v, want deny naming %q", access, tc.command, got, tc.reason)
			}
		}
	}
	for _, command := range []string{
		"uniq input", "uniq -f 1 input", "uniq -s 2 -w 3 input", "uniq -- --skip-fields",
		"rg --text pattern", "rg -- --pre", "sort -- --output", "sort -- --outp=file",
		"git diff -- file", "git log -- path", "git show --text",
		"git diff --no-ext-diff", "git log --no-ext-diff", "git show --no-ext-diff",
		"git diff -- --output=file", "git log -- --ext-diff", "git show -- --textconv",
		"git branch", "git branch --list", "git branch --list 'feature*'", "git branch --show-current",
		"git branch -a", "git branch -r", "git branch -v", "git branch -vv",
		"git branch --contains HEAD", "git branch --merged HEAD", "git branch --no-merged HEAD", "git branch --points-at HEAD",
	} {
		for _, access := range []string{"read", "edit"} {
			got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": command}}, &Lane{Access: access, Root: "/project"})
			if got.Kind != "auto_approve" {
				t.Fatalf("%s %q = %+v", access, command, got)
			}
		}
	}
}

func TestDecideLaneProjectToolchainCommands(t *testing.T) {
	for _, command := range []string{
		"yarn --version", "pnpm --version", "pnpm ls", "pnpm ls --depth 0",
		"npm --version", "npm ls", "npm view react version", "cargo --version", "rustc --version",
	} {
		for _, access := range []string{"read", "edit"} {
			lane := &Lane{Access: access, Root: "/project"}
			call := &Call{Harness: "agy", Tool: "Bash", Input: map[string]any{"command": command}}
			got := DecideLane(call, lane)
			want := "deny"
			if access == "edit" {
				want = "relay"
			}
			if got.Kind != want {
				t.Fatalf("%s %s: %+v, want %s", access, command, got, want)
			}
			lane.Checks = []string{command}
			if access == "edit" {
				want = "auto_approve"
			}
			if got := DecideLane(call, lane); got.Kind != want {
				t.Fatalf("explicit check %s %s: %+v, want %s", access, command, got, want)
			}
		}
	}
	for _, access := range []string{"read", "edit"} {
		for _, command := range []string{
			"composer show", "composer show --format=json", "composer show -- --no-plugins",
			"composer show --no-plugins=0", "composer show --no-scripts",
		} {
			lane := &Lane{Access: access, Root: "/project", Checks: []string{command}, Allow: []string{"Bash(composer *)"}}
			got := DecideLane(&Call{Tool: "Bash", Input: map[string]any{"command": command}}, lane)
			if got.Kind != "deny" || !strings.Contains(got.Reason, "composer show requires --no-plugins") {
				t.Fatalf("%s %s: %+v", access, command, got)
			}
		}
		for _, command := range []string{
			"composer show --no-plugins", "composer show --no-plugins --format=json",
			"composer --no-plugins show", "CI=1 env composer show --no-plugins",
		} {
			got := DecideLane(&Call{Tool: "Bash", Input: map[string]any{"command": command}}, &Lane{Access: access, Root: "/project"})
			if got.Kind != "auto_approve" {
				t.Fatalf("%s %s: %+v", access, command, got)
			}
		}
		for _, command := range []string{"pip list --python=./x", "pip show --python ./x requests"} {
			got := DecideLane(&Call{Tool: "Bash", Input: map[string]any{"command": command}}, &Lane{Access: access, Root: "/project", Checks: []string{command}})
			if got.Kind != "deny" || !strings.Contains(got.Reason, "--python") {
				t.Fatalf("%s %s: %+v", access, command, got)
			}
		}
	}
	got := readOnlyCommandNames()
	if !strings.Contains(got, "composer show --no-plugins") {
		t.Fatalf("accepted commands omit --no-plugins: %s", got)
	}
	for _, removed := range []string{"yarn", "pnpm", "npm", "cargo", "rustc"} {
		if strings.Contains(got, removed) {
			t.Fatalf("accepted commands include %s: %s", removed, got)
		}
	}
}

func TestDecideLaneReadOnlyShellExpansion(t *testing.T) {
	for _, tc := range []struct{ command, reason string }{
		{`find -- . -exec sh -c id \;`, "-exec"},
		{"find -- .git -delete", "-delete"},
		{`find . ${X:--exec} touch /tmp/x \;`, "$"},
		{`find . $'-exec' sh -c id \;`, "$"},
		{`cat "$HOME/x"`, "$"},
		{"cat \"a`id`\"", "`"},
		{"echo $(id)", "shell construct is not allowed: $("},
		{"echo `id`", "shell construct is not allowed: `"},
		{"head -n 1 -- $FILE", "$"},
		{"cat README.md(e:'touch x':)", "("},
		{"cat file(e:'id':)", "read-only command carries a write flag or form: shell expansion ("},
		{"sort {-o,victim,README.md}", "shell expansion {"},
		{`find . -{print,exec} sh -c id \;`, "shell expansion {"},
		{"rg --pr{e,e}=sh x", "shell expansion {"},
		{"git diff --ou{t,t}put=f", "shell expansion {"},
		{"echo {a,b}", "shell expansion {"},
		{"echo ] }", "shell expansion }"},
		{"rg x *", "glob can match a file name that starts with -; write ./ before it"},
		{"ls *_test.go", "glob can match a file name that starts with -; write ./ before it"},
		{"ls *.go", "glob can match a file name that starts with -; write ./ before it"},
		{"cat ?.go", "glob can match a file name that starts with -; write ./ before it"},
		{"cat ?.txt", "glob can match a file name that starts with -; write ./ before it"},
		{"ls [ab].txt", "glob can match a file name that starts with -; write ./ before it"},
		{`ls ""*`, "glob can match a file name that starts with -; write ./ before it"},
		{`ls ''?`, "glob can match a file name that starts with -; write ./ before it"},
		{"ls\t*.go", "glob can match a file name that starts with -; write ./ before it"},
	} {
		for _, access := range []string{"read", "edit"} {
			got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": tc.command}}, &Lane{Access: access, Root: "/project"})
			if got.Kind != "deny" || !strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("%s %q = %+v, want deny naming %q", access, tc.command, got, tc.reason)
			}
		}
	}
	for _, command := range []string{
		"grep -r --include=*.go x .", "ls ./*_test.go", "cat src/**/*.php", "ls ./[ab].txt", "cat ./?.go",
		`ls "./"*`, `ls '.'*`, `ls \**`, `echo \{a,b\}`, `echo \}`, `echo '{a,b}'`, `echo "}"`,
		"find . -name '*.go'", "rg 'x*' -g '*.go' src", "echo '$HOME'", `echo \$HOME`, "git log -- '*.go'",
		"find . -type f -print", `grep -r "a{2}" .`, "cat file 2>&1", `find . \( -name a -o -name b \)`, "grep 'f(x)' main.go",
	} {
		for _, access := range []string{"read", "edit"} {
			if got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": command}}, &Lane{Access: access, Root: "/project"}); got.Kind != "auto_approve" {
				t.Fatalf("%s %q = %+v", access, command, got)
			}
		}
	}
	// A check or an allow rule of an edit Lane is judged as written; the flag table does not apply.
	lane := &Lane{Access: "edit", Root: "/project", Checks: []string{"go test ./..."}, Allow: []string{"Bash(go build *)"}}
	for _, command := range []string{"go test ./...", "go build ./cmd/*"} {
		if got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": command}}, lane); got.Kind != "auto_approve" {
			t.Fatalf("%q = %+v", command, got)
		}
	}
}

func TestDecideLaneFlagWordGlobs(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Access: access, Root: "/project"}
		for _, command := range []string{
			"sort -*", "rg --pr* x", "ls -?", "ls -[ab]",
			`sort "-"*`, `sort '-'*`, `sort \-*`, `sort ''-*`,
			"grep --include=*.go x -*", "rg --pr*=value x",
			"ls -- -*", "ls\t-?",
		} {
			t.Run(access+"/"+command, func(t *testing.T) {
				got := DecideLane(&Call{Tool: "Bash", Input: map[string]any{"command": command}}, lane)
				want := "read-only command carries a write flag or form: shell expansion glob can match a file name that starts with -; write ./ before it"
				if got.Kind != "deny" || got.Reason != want {
					t.Fatalf("verdict = %+v, want deny with reason %q", got, want)
				}
			})
		}
		for _, command := range []string{
			"grep -r --include=*.go x .", "grep --include=a?.go x .",
			"grep --include=[ab].go x .", `grep "--include="*.go x .`,
			`grep --include\=*.go x .`, `ls '-*'`, `ls "-?"`, `ls -\*`,
			"ls ./-*", "cat src/-*.go",
		} {
			t.Run(access+"/"+command, func(t *testing.T) {
				got := DecideLane(&Call{Tool: "Bash", Input: map[string]any{"command": command}}, lane)
				if got.Kind != "auto_approve" {
					t.Fatalf("verdict = %+v, want auto_approve", got)
				}
			})
		}
	}
}

func TestDecideLaneKnownReadTools(t *testing.T) {
	tools := []string{
		"read_file", "list_dir", "grep", "web_search", "web_fetch", "todo_write",
		"get_command_or_subagent_output", "kill_command_or_subagent", "ask_user_question",
		"Read", "Glob", "Grep", "LS", "WebFetch", "WebSearch", "TodoWrite",
		"Skill", "skill", "use_skill", "invoke_skill",
		"view_file", "grep_search", "find_by_name", "view_code_item", "search_web", "read_url_content",
	}
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Access: access}
		for _, tool := range tools {
			for _, prefix := range []string{"", "hashline_"} {
				if got := DecideLane(&Call{Tool: prefix + tool}, lane); got.Kind != "allow" {
					t.Fatalf("%s %s = %+v", access, prefix+tool, got)
				}
			}
		}
		for _, prefix := range []string{"solo__", "mcp__solo__"} {
			for _, tool := range []string{"whoami", "get_process_status", "list_projects", "todo_get", "todo_list", "todo_comment_create", "scratchpad_read", "help"} {
				if got := DecideLane(&Call{Tool: prefix + tool}, lane); got.Kind != "allow" {
					t.Fatalf("%s %s = %+v", access, prefix+tool, got)
				}
			}
		}
		for _, tool := range []string{"hashline_write", "multi_edit", "apply_patch", "files__delete_file", "read_file_extra", "mcp__files__get_file", "hashline_hashline_Read", "solo__scratchpad_edit"} {
			if got := DecideLane(&Call{Tool: tool}, lane); got.Kind != "deny" || !strings.Contains(got.Reason, tool) {
				t.Fatalf("%s %s = %+v", access, tool, got)
			}
		}
		if got := DecideLane(&Call{Tool: "write_to_file"}, lane); got.Kind != "deny" || !strings.Contains(got.Reason, "empty") {
			t.Fatalf("%s empty target = %+v", access, got)
		}
	}
}

func TestClaudeLaneSoloAllowList(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Harness: "claude", Access: access, Allow: []string{"mcp__solo__*"}}
		for _, tool := range []string{
			"mcp__solo__list_processes", "mcp__solo__get_process_status",
			"mcp__solo__get_process_output", "mcp__solo__search_output",
			"mcp__solo__get_process_ports", "mcp__solo__todo_comment_create",
		} {
			if got := DecideLane(&Call{Harness: "claude", Tool: tool}, lane); got.Kind != "allow" {
				t.Fatalf("%s %s = %+v", access, tool, got)
			}
		}
		for _, tool := range []string{
			"mcp__solo__spawn_agent", "mcp__solo__spawn_process", "mcp__solo__send_input",
			"mcp__solo__close_process", "mcp__solo__whoami", "mcp__solo__list_projects",
			"mcp__solo__todo_create", "mcp__solo__todo_comment_create_extra",
		} {
			if got := DecideLane(&Call{Harness: "claude", Tool: tool, Input: map[string]any{"extra_args": []any{"dontAsk"}}}, lane); got.Kind != "deny" {
				t.Fatalf("%s %s = %+v", access, tool, got)
			}
		}
	}
}

func TestDecideLaneGitDirectoryWrites(t *testing.T) {
	root := t.TempDir()
	lane := &Lane{Access: "edit", Root: root, RunFolder: filepath.Join(root, ".agents", "run")}
	for _, tc := range []struct{ target, want string }{
		{".git/config", "deny"},
		{root + "/.git/hooks/pre-commit", "deny"},
		{".GIT/config", "deny"},
		{root + "/.Git/hooks/pre-commit", "deny"},
		{"src/x.go", "auto_approve"},
		{"src/.git/x.go", "auto_approve"},
		{".gitignore", "auto_approve"},
	} {
		got := DecideLane(&Call{Tool: "write", CWD: root, Input: map[string]any{"file_path": tc.target}}, lane)
		if got.Kind != tc.want {
			t.Fatalf("%s = %+v, want %s", tc.target, got, tc.want)
		}
	}
}

// A `.git` that is a symlink into the project is still the git directory,
// through either name.
func TestDecideLaneGitDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "metadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "metadata"), filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	lane := &Lane{Access: "edit", Root: root, RunFolder: filepath.Join(root, ".agents", "run")}
	for _, tc := range []struct{ target, want string }{
		{".git/config", "deny"},
		{root + "/.git/hooks/pre-commit", "deny"},
		{"metadata/config", "deny"},
		{root + "/metadata/hooks/pre-commit", "deny"},
		{"src/x.go", "auto_approve"},
	} {
		got := DecideLane(&Call{Tool: "write", CWD: root, Input: map[string]any{"file_path": tc.target}}, lane)
		if got.Kind != tc.want {
			t.Fatalf("%s = %+v, want %s", tc.target, got, tc.want)
		}
	}
}

func TestDecideLaneSymlinkBeforeParent(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Access: access, Root: root, RunFolder: root}
		for _, target := range []string{"link/../x", root + "/link/../x"} {
			call := &Call{Tool: "write", CWD: root, Input: map[string]any{"file_path": target}}
			if got := DecideLane(call, lane); got.Kind != "deny" {
				t.Fatalf("%s %s = %+v", access, target, got)
			}
		}
	}
}

func TestDecideLaneRelayByHarnessAndAccess(t *testing.T) {
	for _, harness := range []string{"claude", "agy", "grok", "codex"} {
		for _, access := range []string{"read", "edit"} {
			t.Run(harness+"/"+access, func(t *testing.T) {
				call := &Call{Harness: harness, Tool: "Bash", CWD: "/project", Input: map[string]any{"command": "go build ./..."}}
				got := DecideLane(call, &Lane{Harness: harness, Access: access, Root: "/project"})
				want := "deny"
				if access == "edit" && (harness == "claude" || harness == "agy") {
					want = "relay"
				}
				if got.Kind != want || want == "deny" && got.Reason == "" {
					t.Fatalf("verdict = %+v, want %s", got, want)
				}
			})
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
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "temp-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("cannot make a symlink in the temp folder: %v", err)
	}
	if !inside(filepath.Join(alias, "new", "file"), root) || !inside(filepath.Join(root, "new", "file"), alias) {
		t.Fatal("temp path and its symlink did not match")
	}
}

func TestDecideLaneCommandCwd(t *testing.T) {
	root, extra, run, outside := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Access: access, Root: root, Dirs: []string{extra}, RunFolder: run}
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
	for _, harness := range []string{"codex", "grok"} {
		if got, want := string(Render(harness, Verdict{Kind: "auto_approve"})), string(Render(harness, Verdict{Kind: "allow"})); got != want {
			t.Fatalf("%s auto_approve = %s, want %s", harness, got, want)
		}
		got := string(Render(harness, Verdict{Kind: "relay"}))
		if !strings.Contains(got, "deny") || !strings.Contains(got, "relay is only for Antigravity") {
			t.Fatalf("%s relay = %s", harness, got)
		}
	}
}

func TestRenderClaudeLaneVerdicts(t *testing.T) {
	for _, kind := range []string{"allow", "auto_approve", "relay"} {
		if got := Render("claude", Verdict{Kind: kind}); got != nil {
			t.Fatalf("%s = %s, want no output", kind, got)
		}
	}
	want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"Lane policy"}}`
	if got := string(Render("claude", Verdict{Kind: "deny", Reason: "Lane policy"})); got != want {
		t.Fatalf("deny = %s, want %s", got, want)
	}
}

func TestDecideLaneClaudeTools(t *testing.T) {
	for _, tc := range []struct {
		name, access, tool string
		input              map[string]any
		want               string
	}{
		{"read", "read", "Read", map[string]any{"file_path": "/project/main.go"}, "allow"},
		{"read command", "read", "Bash", map[string]any{"command": "git log --oneline"}, "auto_approve"},
		{"write command", "read", "Bash", map[string]any{"command": "git log --output=x"}, "deny"},
		{"report write", "read", "Write", map[string]any{"file_path": "/run/result.md"}, "auto_approve"},
		{"project edit", "edit", "Edit", map[string]any{"file_path": "/project/main.go"}, "auto_approve"},
		{"git multi edit", "edit", "MultiEdit", map[string]any{"file_path": "/project/.git/config"}, "deny"},
		{"read notebook", "read", "NotebookEdit", map[string]any{"notebook_path": "/project/book.ipynb"}, "deny"},
		{"edit notebook", "edit", "NotebookEdit", map[string]any{"notebook_path": "/project/book.ipynb"}, "auto_approve"},
		{"edit notebook without a path", "edit", "NotebookEdit", map[string]any{"new_source": "x"}, "deny"},
		{"Agent reader", "read", "Agent", map[string]any{"subagent_type": "Explore"}, "allow"},
		{"Agent writer", "edit", "Agent", map[string]any{"subagent_type": "writer"}, "deny"},
		{"Task reader", "read", "Task", map[string]any{"subagent_type": "Explore"}, "allow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lane := &Lane{Harness: "claude", Access: tc.access, Root: "/project", RunFolder: "/run"}
			call := &Call{Harness: "claude", CWD: lane.Root, Tool: tc.tool, Input: tc.input}
			if got := DecideLane(call, lane); got.Kind != tc.want {
				t.Fatalf("verdict: %+v, want %s", got, tc.want)
			}
		})
	}
}

func TestFindLaneFolderChallengeRound(t *testing.T) {
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	transcript := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(transcript, []byte(transcriptPromptLine(t, "agy", brief.Pointer("/p/.agents/maestro/councils/slug/round-2/grok/brief.md"))), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok, err := FindLaneFolder(&Call{Harness: "agy", TranscriptPath: transcript})
	if err != nil || !ok || got != "/p/.agents/maestro/councils/slug/round-2/grok" {
		t.Fatalf("got %q ok=%v err=%v", got, ok, err)
	}
}

func TestFindLaneFolderRelativePointerBase(t *testing.T) {
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	folder := ".agents/maestro/runs/r-test/abcd"
	for _, tc := range []struct {
		harness, cwd string
		workspaces   []string
		base         string
	}{
		{"claude", "/project", nil, "/project"},
		{"codex", "/project", nil, "/project"},
		{"grok", "/project", nil, "/project"},
		{"agy", "/project/sub", []string{"/project", "/other"}, "/project"},
		{"agy", "/project/sub", nil, "/project/sub"},
	} {
		t.Run(tc.harness+"/"+tc.base, func(t *testing.T) {
			transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
			body := transcriptPromptLine(t, tc.harness, brief.Pointer(folder+"/brief.md"))
			if err := os.WriteFile(transcript, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			call := &Call{Harness: tc.harness, CWD: tc.cwd, WorkspacePaths: tc.workspaces, TranscriptPath: transcript}
			got, found, err := FindLaneFolder(call)
			want := filepath.Join(tc.base, folder)
			if err != nil || !found || got != want || !filepath.IsAbs(got) {
				t.Fatalf("folder = %q, %v, %v; want %q", got, found, err, want)
			}
		})
	}
}

func TestFindLaneFolderFirstPromptNearReaderLimit(t *testing.T) {
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	folder := "/project/.agents/maestro/runs/r-test/abcd"
	line := "{\"type\":\"system\",\"text\":\"" + strings.Repeat("x", 1024) + "\"}\n"
	for _, harness := range []string{"claude", "codex", "grok", "agy"} {
		t.Run(harness, func(t *testing.T) {
			pointer := transcriptPromptLine(t, harness, brief.Pointer(folder+"/brief.md"))
			padding := strings.Repeat(line, (transcriptReadLimit-len(pointer))/len(line))
			body := padding + pointer + strings.Repeat(line, 20)
			transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
			if err := os.WriteFile(transcript, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			got, found, err := FindLaneFolder(&Call{Harness: harness, TranscriptPath: transcript})
			if err != nil || !found || got != folder {
				t.Fatalf("folder = %q, %v, %v; want %q", got, found, err, folder)
			}
		})
	}
}

func TestFindLaneFolderAntigravityFirstExplicitUserPrompt(t *testing.T) {
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	folder := "/project/.agents/maestro/runs/r-test/abcd"
	pointer := transcriptPromptLine(t, "agy", brief.Pointer(folder+"/brief.md"))
	for _, tc := range []struct {
		name, body string
		found      bool
	}{
		{"first explicit prompt", "{\"type\":\"USER_INPUT\",\"source\":\"SYSTEM\",\"content\":\"context\"}\n" + pointer, true},
		{"later quoted Brief", transcriptPromptLine(t, "agy", "Explain this code.") + transcriptPromptLine(t, "agy", "Look at "+folder+"/brief.md"), false},
		{"later Pointer", transcriptPromptLine(t, "agy", "Explain this code.") + pointer, false},
		{"quoted first Pointer", transcriptPromptLine(t, "agy", "Explain this prompt: "+brief.Pointer(folder+"/brief.md")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
			if err := os.WriteFile(transcript, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			got, found, err := FindLaneFolder(&Call{Harness: "agy", TranscriptPath: transcript})
			want := ""
			if tc.found {
				want = folder
			}
			if err != nil || got != want || found != tc.found {
				t.Fatalf("folder = %q, %v, %v; want %q, %v", got, found, err, want, tc.found)
			}
		})
	}
}
