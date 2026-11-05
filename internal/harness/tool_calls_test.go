package harness

import "testing"

func TestParsersTrackToolCalls(t *testing.T) {
	for _, tc := range []struct {
		name      string
		parser    Parser
		start     string
		second    string
		end       string
		endSecond string
		unmatched string
		other     []string
	}{
		{
			name: "claude", parser: (&Claude{}).NewParser(),
			start:     `{"type":"assistant","message":{"content":[{"type":"text","text":"run tests"},{"type":"tool_use","id":"a","name":"Bash"}]}}`,
			second:    `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"b","name":"Read"}]}}`,
			end:       `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"ok"}]}}`,
			endSecond: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b","is_error":true}]}}`,
			unmatched: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"other"}]}}`,
			other: []string{
				`{"type":"system","subtype":"init"}`,
				`{"type":"assistant","message":{"content":[{"type":"text","text":"still working"}]}}`,
				`{"type":"user","message":{"content":"continue"}}`,
				`{"type":"assistant","message":{"content":[{"type":"tool_use"}]}}`,
			},
		},
		{
			name: "codex", parser: (&Codex{}).NewParser(),
			start:     `{"type":"item.started","item":{"id":"a","type":"command_execution"}}`,
			second:    `{"type":"item.started","item":{"id":"b","type":"mcp_tool_call"}}`,
			end:       `{"type":"item.completed","item":{"id":"a","type":"command_execution"}}`,
			endSecond: `{"type":"item.completed","item":{"id":"b","type":"mcp_tool_call","status":"failed"}}`,
			unmatched: `{"type":"item.completed","item":{"id":"other","type":"command_execution"}}`,
			other: []string{
				`{"type":"thread.started","thread_id":"session"}`,
				`{"type":"item.started","item":{"id":"text","type":"agent_message"}}`,
				`{"type":"item.started","item":{"id":"thought","type":"reasoning"}}`,
				`{"type":"item.updated","item":{"id":"a","type":"command_execution"}}`,
				`{"type":"item.started","item":{"type":"command_execution"}}`,
			},
		},
		{
			name: "grok", parser: (&Grok{}).NewParser(),
			start:     `{"type":"tool_call","toolCallId":"a","status":"pending"}`,
			second:    `{"type":"tool_call","toolCallId":"b","status":"pending"}`,
			end:       `{"type":"tool_call_update","toolCallId":"a","status":"completed"}`,
			endSecond: `{"type":"tool_call_update","toolCallId":"b","status":"failed"}`,
			unmatched: `{"type":"tool_call_update","toolCallId":"other","status":"completed"}`,
			other: []string{
				`{"type":"text","data":"still working"}`,
				`{"type":"tool_call_update","toolCallId":"a","status":null}`,
				`{"type":"tool_call_update","toolCallId":"a","status":"in_progress"}`,
				`{"type":"available_commands","tools":[]}`,
				`{"type":"tool_call","status":"pending"}`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := func(line string, want bool) {
				t.Helper()
				tc.parser.Line([]byte(line))
				if got := tc.parser.ToolInProgress(); got != want {
					t.Fatalf("ToolInProgress() = %v, want %v after %s", got, want, line)
				}
			}
			if tc.parser.ToolInProgress() {
				t.Fatal("new parser has a tool call in progress")
			}
			for _, line := range tc.other {
				check(line, false)
			}
			check(tc.start, true)
			check(tc.start, true) // A repeated start must not count twice.
			for _, line := range tc.other {
				check(line, true)
			}
			check(tc.unmatched, true)
			check(tc.second, true)
			check(tc.end, true) // The second call still runs.
			check(tc.end, true)
			check(tc.endSecond, false)
			for _, line := range tc.other {
				check(line, false)
			}
		})
	}
}

func TestCodexToolItemTypes(t *testing.T) {
	for _, kind := range []string{"command_execution", "mcp_tool_call", "web_search", "file_change"} {
		t.Run(kind, func(t *testing.T) {
			p := (&Codex{}).NewParser()
			p.Line([]byte(`{"type":"item.started","item":{"id":"a","type":"` + kind + `"}}`))
			if !p.ToolInProgress() {
				t.Fatal("tool start was not tracked")
			}
			p.Line([]byte(`{"type":"item.completed","item":{"id":"a","type":"` + kind + `"}}`))
			if p.ToolInProgress() {
				t.Fatal("completed tool is still in progress")
			}
		})
	}
}

func TestAgyHasNoToolCallEvents(t *testing.T) {
	p := (&Agy{}).NewParser()
	p.Line([]byte(`{"status":"SUCCESS","response":"OK"}`))
	if p.ToolInProgress() {
		t.Fatal("Antigravity reported a tool call in progress")
	}
}
