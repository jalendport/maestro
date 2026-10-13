package harness

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestClaudePermissionHookInEveryShape(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, access := range []string{"read", "edit"} {
		for _, tab := range []bool{false, true} {
			lane := Launch{Access: access, RunFolder: t.TempDir(), TaskID: "abcd", AllowRules: []string{"Bash(go run *)"}}
			c := &Claude{bin: "/bin/claude"}
			var command *Command
			if tab {
				command, err = c.BuildTab(lane)
			} else {
				command, err = c.Build(lane)
			}
			if err != nil {
				t.Fatal(err)
			}
			settings := ""
			for i, arg := range command.Cmd.Args {
				if arg == "--settings" {
					settings = command.Cmd.Args[i+1]
				}
			}
			var value struct {
				Hooks map[string][]struct {
					Matcher *string `json:"matcher"`
					Hooks   []struct {
						Type, Command string
						Timeout       int
					} `json:"hooks"`
				} `json:"hooks"`
			}
			if err := json.Unmarshal([]byte(settings), &value); err != nil {
				t.Fatal(err)
			}
			hooks := value.Hooks["PermissionRequest"]
			if len(hooks) != 1 || hooks[0].Matcher != nil || len(hooks[0].Hooks) != 1 {
				t.Fatalf("settings: %s", settings)
			}
			hook := hooks[0].Hooks[0]
			if hook.Type != "command" || hook.Timeout != 660 || !strings.Contains(hook.Command, self) || !strings.Contains(hook.Command, " relay 'abcd'") {
				t.Fatalf("hook: %+v", hook)
			}
			found := false
			for _, arg := range command.Cmd.Args {
				if arg == lane.AllowRules[0] {
					found = true
				}
			}
			if found != (access == "edit") {
				t.Fatalf("project allow applied to %s: %v", access, command.Cmd.Args)
			}
		}
	}
}

func TestGrokEditRulesAndDenyMode(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		for _, tab := range []bool{false, true} {
			g := &Grok{bin: "/bin/grok"}
			lane := Launch{Access: access, RunFolder: t.TempDir(), AllowCommit: true, AllowedCommands: []string{"go test ./..."}, AllowRules: []string{"Bash(go run *)"}}
			var command *Command
			var err error
			if tab {
				command, err = g.BuildTab(lane)
			} else {
				command, err = g.Build(lane)
			}
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Join(command.Cmd.Args, "\n")
			if !strings.Contains(args, "--permission-mode\ndontAsk") {
				t.Fatalf("mode: %s", args)
			}
			for _, rule := range []string{lane.AllowRules[0], "Bash(go test ./... *)", "Bash(git add *)", "Bash(git " + "commit *)"} {
				if strings.Contains(args, "--allow\n"+rule) != (access == "edit") {
					t.Fatalf("%s rule %s: %s", access, rule, args)
				}
			}
		}
	}
}
