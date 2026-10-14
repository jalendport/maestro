package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/config"
)

func TestParseDetectsHarness(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		harness string
		wantErr bool
	}{
		{name: "claude", payload: `{"tool_name":"Agent"}`, harness: "claude"},
		{name: "grok", payload: `{"toolName":"spawn_subagent","hookEventName":"PreToolUse"}`, harness: "grok"},
		{name: "agy", payload: `{"toolCall":{"name":"invoke_subagent"}}`, harness: "agy"},
		{name: "non-JSON", payload: "not JSON", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse("", []byte(tc.payload))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error for a non-JSON payload")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Harness != tc.harness {
				t.Fatalf("Harness = %q, want %q", c.Harness, tc.harness)
			}
		})
	}
}

func TestPayloadDecisions(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		allow   bool
	}{
		{name: "claude writer", payload: `{"hook_event_name":"PreToolUse","tool_name":"Agent","tool_input":{"subagent_type":"writer"}}`, allow: false},
		{name: "claude Explore", payload: `{"hook_event_name":"PreToolUse","tool_name":"Agent","tool_input":{"subagent_type":"Explore"}}`, allow: true},
		{name: "claude solo edit", payload: `{"hook_event_name":"PreToolUse","tool_name":"mcp__solo__spawn_agent","tool_input":{"extra_args":["--permission-mode","acceptEdits"]}}`, allow: false},
		{name: "claude solo process", payload: `{"hook_event_name":"PreToolUse","tool_name":"mcp__solo__spawn_process","tool_input":{"kind":"terminal"}}`, allow: true},
		{name: "claude read", payload: `{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{}}`, allow: true},
		{name: "codex write", payload: `{"hook_event_name":"PreToolUse","tool_name":"Agent","tool_input":{"subagent_type":"default","prompt":"write code"}}`, allow: false},
		{name: "grok writer", payload: `{"hookEventName":"PreToolUse","toolName":"spawn_subagent","toolInput":{"subagent_type":"writer"}}`, allow: false},
		{name: "grok Explore", payload: `{"hookEventName":"PreToolUse","toolName":"spawn_subagent","toolInput":{"subagent_type":"Explore"}}`, allow: true},
		{name: "grok solo edit", payload: `{"hookEventName":"PreToolUse","toolName":"solo__spawn_agent","toolInput":{"extra_args":["-s","workspace-write"]}}`, allow: false},
		{name: "grok solo process", payload: `{"hookEventName":"PreToolUse","toolName":"solo__spawn_process","toolInput":{"kind":"terminal"}}`, allow: true},
		{name: "grok read", payload: `{"hookEventName":"PreToolUse","toolName":"read_file","toolInput":{}}`, allow: true},
		{name: "antigravity subagent", payload: `{"toolCall":{"name":"invoke_subagent","args":{"agent":"coder"}}}`, allow: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse("", []byte(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			if d := Decide(c, "all"); d.Allow != tc.allow {
				t.Fatalf("Decide = %+v, want Allow = %v", d, tc.allow)
			}
		})
	}
}

func TestRenderAllow(t *testing.T) {
	cases := []struct {
		harness string
		want    string
	}{
		{harness: "grok", want: `{"decision":"allow"}`},
		{harness: "claude", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.harness, func(t *testing.T) {
			if got := string(Render(tc.harness, Decision{Allow: true})); got != tc.want {
				t.Fatalf("Render = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClaudeAgentWriteDenied(t *testing.T) {
	c, err := Parse("claude", []byte(`{"tool_name":"Agent","tool_input":{"subagent_type":"writer","prompt":"x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	d := Decide(c, "all")
	if d.Allow || !strings.Contains(string(Render("claude", d)), `"permissionDecision":"deny"`) {
		t.Fatalf("%+v", d)
	}
}

func TestClaudeExploreAllowed(t *testing.T) {
	c, _ := Parse("claude", []byte(`{"tool_name":"Agent","tool_input":{"subagent_type":"Explore"}}`))
	if d := Decide(c, "all"); !d.Allow || Render("claude", d) != nil {
		t.Fatalf("%+v", d)
	}
}

func TestCustomReadOnlyAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	project := t.TempDir()
	write := func(dir, name, front string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\nname: "+name+"\n"+front+"---\n\nBody.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	userAgents := filepath.Join(home, ".claude", "agents")
	projectAgents := filepath.Join(project, ".claude", "agents")
	write(userAgents, "scout", "disallowedTools: Edit, Write, NotebookEdit, Agent\nmodel: haiku\n")
	write(userAgents, "grunt", "disallowedTools: Agent\nmodel: sonnet\n")
	write(userAgents, "reader", "tools: Read, Grep, Glob, Bash\n")
	write(userAgents, "quoted", "tools: [\"Read\", \"Grep\"]\n")
	write(userAgents, "editor", "tools: Read, Edit\n")
	write(userAgents, "partial", "disallowedTools: Edit, Write\n")
	write(userAgents, "plain", "model: sonnet\n")
	write(projectAgents, "local", "tools: Read\n")
	write(userAgents, "shadowed", "tools: Read\n")
	write(projectAgents, "shadowed", "tools: Read, Write\n")
	if err := os.WriteFile(filepath.Join(userAgents, "nofront.md"), []byte("tools: Read\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		allow bool
	}{
		{"scout", true}, {"SCOUT", true}, {"grunt", false}, {"reader", true}, {"quoted", true}, {"editor", false},
		{"partial", false}, {"plain", false}, {"local", true}, {"shadowed", false}, {"nofront", false},
		{"missing", false}, {"../scout", false}, {"", false},
	} {
		for _, cwd := range []string{project, filepath.Join(project, "sub", "dir")} {
			call := &Call{Tool: "Agent", CWD: cwd, Input: map[string]any{"subagent_type": tc.name}}
			if d := Decide(call, "all"); d.Allow != tc.allow {
				t.Fatalf("%q from %s: %+v, want allow=%v", tc.name, cwd, d, tc.allow)
			}
		}
	}
	// The project file wins over the Claude home for the same name; outside the project only the home counts.
	if d := Decide(&Call{Tool: "Agent", CWD: t.TempDir(), Input: map[string]any{"subagent_type": "shadowed"}}, "all"); !d.Allow {
		t.Fatalf("home definition outside the project: %+v", d)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "other"))
	if d := Decide(&Call{Tool: "Agent", CWD: project, Input: map[string]any{"subagent_type": "scout"}}, "all"); d.Allow {
		t.Fatalf("CLAUDE_CONFIG_DIR must move the lookup: %+v", d)
	}
}

func TestSoloSpawnShapes(t *testing.T) {
	c, _ := Parse("claude", []byte(`{"tool_name":"mcp__solo__spawn_agent","tool_input":{"agent_tool_id":5,"extra_args":["--model","opus"]}}`))
	if Decide(c, "all").Allow {
		t.Fatal("a code-writing Solo spawn must be denied")
	}
	c, _ = Parse("claude", []byte(`{"tool_name":"mcp__solo__spawn_agent","tool_input":{"agent_tool_id":5,"extra_args":["--permission-mode","dontAsk"]}}`))
	if !Decide(c, "all").Allow {
		t.Fatal("a read-only Solo spawn passes")
	}
	c, _ = Parse("", []byte(`{"hookEventName":"PreToolUse","toolName":"solo__spawn_agent","toolInput":{"agent_tool_id":13}}`))
	if c.Harness != "grok" || Decide(c, "all").Allow || !strings.Contains(string(Render("grok", Decide(c, "all"))), `"decision":"deny"`) {
		t.Fatalf("grok: %+v", c)
	}
	c, _ = Parse("", []byte(`{"conversationId":"x","toolCall":{"name":"invoke_subagent","args":{"agent":"coder"}}}`))
	if c.Harness != "agy" || Decide(c, "all").Allow {
		t.Fatalf("agy: %+v", c)
	}
	if string(Render("grok", Decision{Allow: true})) != `{"decision":"allow"}` {
		t.Fatal("grok allow must be explicit")
	}
}

func TestOtherToolsPass(t *testing.T) {
	c, _ := Parse("codex", []byte(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`))
	if !Decide(c, "all").Allow {
		t.Fatal("Bash passes")
	}
}

func TestProjectScope(t *testing.T) {
	outside := t.TempDir()
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProjectFile(project), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// An inner repository marker must not stop the project-file search.
	nested := filepath.Join(project, "nested", ".git")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"claude", "codex"} {
		for _, tc := range []struct {
			cwd   string
			allow bool
		}{{outside, true}, {filepath.Dir(nested), false}} {
			payload, _ := json.Marshal(map[string]any{"cwd": tc.cwd, "tool_name": "Agent", "tool_input": map[string]any{"subagent_type": "writer"}})
			call, err := Parse(harness, payload)
			if err != nil {
				t.Fatal(err)
			}
			if d := Decide(call, "projects"); d.Allow != tc.allow {
				t.Fatalf("%s cwd %s: %+v", harness, tc.cwd, d)
			}
			if Decide(call, "all").Allow {
				t.Fatalf("scope all allowed %s at %s", harness, tc.cwd)
			}
		}
	}
}

func TestProjectScopeFallsBackToProcessDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	call := &Call{Harness: "grok", Tool: "spawn_subagent", Input: map[string]any{}}
	if !Decide(call, "projects").Allow {
		t.Fatal("a directory with only .agents must be outside scope")
	}
	if err := os.WriteFile(config.ProjectFile(root), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if Decide(call, "projects").Allow {
		t.Fatal("a project file must put the process directory in scope")
	}
}
