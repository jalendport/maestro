// Package hook is the PreToolUse entry point the Adapters call: it reads the
// Harness's payload shape, applies one rule, and writes the Harness's answer
// shape. The rule: read-only sub-agent spawns pass; code-writing sub-agent
// and Solo spawns are denied with "use maestro dispatch". Inside a Lane
// (MAESTRO_LANE=1) the same rule applies.
package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Decision is the harness-neutral result.
type Decision struct {
	Allow  bool
	Reason string
}

// Call is the harness-neutral view of a tool call.
type Call struct {
	Harness string
	Tool    string
	Input   map[string]any
}

// readOnlySubagents are Claude agent types that only read.
var readOnlySubagents = map[string]bool{"scout": true, "Explore": true, "explore": true, "Plan": true, "plan": true, "claude-code-guide": true}

// Parse reads a payload. harness selects the shape; "" detects it from the keys.
func Parse(harness string, payload []byte) (*Call, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("payload is not JSON: %w", err)
	}
	if harness == "" {
		switch {
		case raw["toolCall"] != nil:
			harness = "agy"
		case raw["toolName"] != nil || raw["hookEventName"] != nil && raw["tool_name"] == nil:
			harness = "grok"
		default:
			harness = "claude"
		}
	}
	c := &Call{Harness: harness, Input: map[string]any{}}
	switch harness {
	case "claude", "codex":
		c.Tool, _ = raw["tool_name"].(string)
		if in, ok := raw["tool_input"].(map[string]any); ok {
			c.Input = in
		}
	case "grok":
		c.Tool, _ = raw["toolName"].(string)
		if in, ok := raw["toolInput"].(map[string]any); ok {
			c.Input = in
		}
	case "agy":
		if tc, ok := raw["toolCall"].(map[string]any); ok {
			c.Tool, _ = tc["name"].(string)
			if in, ok := tc["args"].(map[string]any); ok {
				c.Input = in
			}
		}
	default:
		return nil, fmt.Errorf("unknown harness %q", harness)
	}
	return c, nil
}

// Decide applies the rule.
func Decide(c *Call) Decision {
	tool := c.Tool
	switch {
	case isSoloSpawn(tool):
		if readOnlyArgs(c.Input) {
			return Decision{Allow: true}
		}
		return Decision{Allow: false, Reason: "Code-writing Solo lanes go through Maestro: write a brief file and run `maestro dispatch <brief.md>` (or `maestro run <plan.md>`). A read-only spawn passes when extra_args carries a read shape (`--permission-mode dontAsk`, `-s read-only`)."}
	case tool == "Agent" || tool == "Task" || tool == "spawn_subagent" || tool == "invoke_subagent":
		sub, _ := c.Input["subagent_type"].(string)
		if sub == "" {
			sub, _ = c.Input["agent"].(string)
		}
		if readOnlySubagents[sub] {
			return Decision{Allow: true}
		}
		return Decision{Allow: false, Reason: fmt.Sprintf("Sub-agent %q may write code; Maestro routes that work. Write a brief file and run `maestro dispatch <brief.md>`. Read-only legwork may stay on a read-only agent type (scout, Explore, Plan).", nonEmpty(sub, tool))}
	}
	return Decision{Allow: true}
}

func isSoloSpawn(tool string) bool {
	t := strings.ToLower(tool)
	return strings.HasSuffix(t, "spawn_agent") || strings.HasSuffix(t, "spawn_process")
}

// readOnlyArgs looks for a read shape in a Solo spawn's extra_args.
func readOnlyArgs(in map[string]any) bool {
	args, _ := in["extra_args"].([]any)
	var parts []string
	for _, a := range args {
		if s, ok := a.(string); ok {
			parts = append(parts, s)
		}
	}
	joined := " " + strings.Join(parts, " ") + " "
	if kind, _ := in["kind"].(string); kind == "terminal" {
		return true
	}
	return strings.Contains(joined, " dontAsk ") || strings.Contains(joined, " read-only ") || strings.Contains(joined, " --deny Write ")
}

// Render writes the answer in the Harness's shape. An allow is written
// explicitly for Grok, whose hooks fail open on silence.
func Render(harness string, d Decision) []byte {
	var out any
	switch harness {
	case "claude", "codex":
		if d.Allow {
			return nil // silence = allow; the Harness's own permission flow continues
		}
		out = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": d.Reason}}
	case "grok", "agy":
		if d.Allow {
			out = map[string]any{"decision": "allow"}
		} else {
			out = map[string]any{"decision": "deny", "reason": d.Reason}
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
	return bytes.TrimSpace(buf.Bytes())
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
