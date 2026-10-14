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
		c, err := Parse(harness, []byte(`{"cwd":"/outer","toolCall":{"name":"run_command","args":{"Cwd":"/project","CommandLine":"pwd","Blocking":true}}}`))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"Cwd": "/project", "CommandLine": "pwd", "Blocking": true}
		if c.Harness != "agy" || c.CWD != "/project" || c.Tool != "run_command" || !reflect.DeepEqual(c.Input, want) {
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
	for _, body := range []string{"not JSON", "null", `{"access":true}`} {
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
	commands := []string{"cat", "ls", "head", "tail", "grep", "rg", "find", "wc", "pwd", "echo", "which", "env", "date", "diff", "tree", "sort", "uniq", "cut", "basename", "dirname", "file", "stat", "realpath", "git log", "git diff", "git show", "git status", "git branch", "git blame"}
	for _, access := range []string{"read", "edit"} {
		for _, command := range commands {
			for _, suffix := range []string{"", " argument"} {
				call := &Call{Harness: "agy", Tool: "run_command", Input: map[string]any{"CommandLine": command + suffix}}
				if got := DecideLane(call, &Lane{Access: access}); got.Kind != "auto_approve" {
					t.Fatalf("%s %s: %+v", access, command+suffix, got)
				}
			}
		}
	}
}

func TestDecideLaneCommandPermissions(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		for _, tab := range []bool{false, true} {
			lane := &Lane{Access: access, Tab: tab, Checks: []string{"go test ./..."}, Allow: []string{"Bash(go run *)", "Bash(make build)", "Write(*)", "Bash", "Bash()"}}
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
					lane := &Lane{Access: access, Commit: commit}
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
		lane := &Lane{Access: access, Checks: []string{"go test ./..."}}
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
