package harness

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestClaudePermissionHookForEachAccess(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, access := range []string{"read", "edit"} {
		lane := Launch{Access: access, RunFolder: t.TempDir(), TaskID: "abcd", AllowRules: []string{"Bash(go run *)"}}
		c := &Claude{bin: "/bin/claude"}
		command, err := c.Build(lane)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(command.Cmd.Args, "\n"), "--permission-prompts\nnone") {
			t.Fatalf("one-shot must keep permission prompts off: %q", command.Cmd.Args)
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
		preTool := value.Hooks["PreToolUse"]
		if len(preTool) != 1 || preTool[0].Matcher == nil {
			t.Fatalf("missing Lane hook matcher: %s", settings)
		}
		matcher, err := regexp.Compile("^(?:" + *preTool[0].Matcher + ")$")
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range []string{"mcp__solo__list_processes", "mcp__solo__search_output", "mcp__solo__send_input", "mcp__solo__spawn_agent", "mcp__solo__future_tool"} {
			if !matcher.MatchString(tool) {
				t.Fatalf("Lane hook does not check %s", tool)
			}
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

func TestGrokLaunchPassesNoRules(t *testing.T) {
	// Grok runs in always-approve; the Maestro hook is the policy, so no
	// allow or deny rule is passed for either Access value.
	for _, access := range []string{"read", "edit"} {
		g := &Grok{bin: "/bin/grok"}
		lane := Launch{Access: access, RunFolder: t.TempDir(), AllowedCommands: []string{"go test ./..."}, AllowRules: []string{"Bash(go run *)"}}
		command, err := g.Build(lane)
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Join(command.Cmd.Args, "\n")
		if !strings.Contains(args, "--permission-mode\nbypassPermissions") || strings.Contains(args, "--allow") || strings.Contains(args, "--deny") {
			t.Fatalf("%s: %s", access, args)
		}
	}
}
