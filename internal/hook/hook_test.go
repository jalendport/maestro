package hook

import (
	"strings"
	"testing"
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

func TestClaudeAgentWriteDenied(t *testing.T) {
	c, err := Parse("claude", []byte(`{"tool_name":"Agent","tool_input":{"subagent_type":"grunt","prompt":"x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	d := Decide(c)
	if d.Allow || !strings.Contains(string(Render("claude", d)), `"permissionDecision":"deny"`) {
		t.Fatalf("%+v", d)
	}
}

func TestClaudeScoutAllowed(t *testing.T) {
	c, _ := Parse("claude", []byte(`{"tool_name":"Agent","tool_input":{"subagent_type":"scout"}}`))
	if d := Decide(c); !d.Allow || Render("claude", d) != nil {
		t.Fatalf("%+v", d)
	}
}

func TestSoloSpawnShapes(t *testing.T) {
	c, _ := Parse("claude", []byte(`{"tool_name":"mcp__solo__spawn_agent","tool_input":{"agent_tool_id":5,"extra_args":["--model","opus"]}}`))
	if Decide(c).Allow {
		t.Fatal("a code-writing Solo spawn must be denied")
	}
	c, _ = Parse("claude", []byte(`{"tool_name":"mcp__solo__spawn_agent","tool_input":{"agent_tool_id":5,"extra_args":["--permission-mode","dontAsk"]}}`))
	if !Decide(c).Allow {
		t.Fatal("a read-only Solo spawn passes")
	}
	c, _ = Parse("", []byte(`{"hookEventName":"PreToolUse","toolName":"solo__spawn_agent","toolInput":{"agent_tool_id":13}}`))
	if c.Harness != "grok" || Decide(c).Allow || !strings.Contains(string(Render("grok", Decide(c))), `"decision":"deny"`) {
		t.Fatalf("grok: %+v", c)
	}
	c, _ = Parse("", []byte(`{"conversationId":"x","toolCall":{"name":"invoke_subagent","args":{"agent":"coder"}}}`))
	if c.Harness != "agy" || Decide(c).Allow {
		t.Fatalf("agy: %+v", c)
	}
	if string(Render("grok", Decision{Allow: true})) != `{"decision":"allow"}` {
		t.Fatal("grok allow must be explicit")
	}
}

func TestOtherToolsPass(t *testing.T) {
	c, _ := Parse("codex", []byte(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`))
	if !Decide(c).Allow {
		t.Fatal("Bash passes")
	}
}
