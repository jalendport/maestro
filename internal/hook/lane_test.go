package hook

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
		{"other Harness", "", "claude", "Read `/project/.agents/maestro/runs/r-test/abcd/brief.md`", ""},
		{"first Pointer", "", "agy", "`/project/.agents/maestro/runs/r-test/abcd/brief.md` `/project/.agents/maestro/runs/r-next/efgh/brief.md`", "/project/.agents/maestro/runs/r-test/abcd"},
		{"past limit", "", "agy", strings.Repeat("x", 64*1024) + " `/project/.agents/maestro/runs/r-test/abcd/brief.md`", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAESTRO_TASK_FOLDER", tc.env)
			transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
			if err := os.WriteFile(transcript, []byte(tc.text), 0o600); err != nil {
				t.Fatal(err)
			}
			got, ok := FindLaneFolder(&Call{Harness: tc.harness, TranscriptPath: transcript})
			if got != tc.want || ok != (tc.want != "") {
				t.Fatalf("folder = %q, %v; want %q", got, ok, tc.want)
			}
		})
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
