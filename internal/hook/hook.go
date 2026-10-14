// Package hook reads each Harness's tool calls, applies the scoped spawn rule and Antigravity Lane permissions, and renders the Harness's answer.
package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jalendport/maestro/internal/config"
)

// Decision is the harness-neutral result.
type Decision struct {
	Allow  bool
	Reason string
}

func (d Decision) verdict() Verdict {
	if d.Allow {
		return Verdict{Kind: "allow"}
	}
	return Verdict{Kind: "deny", Reason: d.Reason}
}

// Call is the harness-neutral view of a tool call.
type Call struct {
	Harness string
	CWD     string
	Tool    string
	Input   map[string]any
}

// readOnlySubagents are Claude agent types that only read.
var readOnlySubagents = map[string]bool{"Explore": true, "explore": true, "Plan": true, "plan": true, "claude-code-guide": true}

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
	cwd, _ := raw["cwd"].(string)
	c := &Call{Harness: harness, CWD: cwd, Input: map[string]any{}}
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
				if cwd, ok := in["Cwd"].(string); ok {
					c.CWD = cwd
				}
			}
		}
	default:
		return nil, fmt.Errorf("unknown harness %q", harness)
	}
	return c, nil
}

// Decide applies the rule only in the configured scope.
func Decide(c *Call, scope string) Decision {
	if scope != "all" && !inProject(c.CWD) {
		return Decision{Allow: true}
	}
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
		return Decision{Allow: false, Reason: fmt.Sprintf("Sub-agent %q may write code; Maestro routes that work. Write a brief file and run `maestro dispatch <brief.md>`. Read-only legwork may stay on a read-only agent type (Explore, Plan).", nonEmpty(sub, tool))}
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
func Render(harness string, d interface{ verdict() Verdict }) []byte {
	v := d.verdict()
	var out any
	switch harness {
	case "claude", "codex":
		if v.Kind == "allow" {
			return nil // silence = allow; the Harness's own permission flow continues
		}
		out = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": v.Reason}}
	case "grok":
		if v.Kind == "allow" {
			out = map[string]any{"decision": "allow"}
		} else {
			out = map[string]any{"decision": "deny", "reason": v.Reason}
		}
	case "agy":
		if v.Kind == "deny" {
			out = map[string]any{"decision": "deny", "reason": v.Reason}
		} else {
			out = map[string]any{"decision": v.Kind}
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

// inProject walks past repository markers to find a project file.
func inProject(cwd string) bool {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}
	for {
		if info, err := os.Stat(config.ProjectFile(dir)); err == nil && !info.IsDir() {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}
