// Package hook reads each Harness's tool calls and Reminder events, applies the scoped spawn rule and Antigravity Lane permissions, and renders the Harness's answer or Reminder.
package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jalendport/maestro/internal/config"
	"gopkg.in/yaml.v3"
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

// Call is the harness-neutral view of a tool call or Reminder event.
type Call struct {
	Harness        string
	Event          string
	Prompt         string
	CWD            string
	Tool           string
	Input          map[string]any
	TranscriptPath string
	WorkspacePaths []string
}

// readOnlyAgents are the built-in Claude agent types that only read. A custom
// agent counts as read-only when its tools are all on the read-only list.
var readOnlyAgents = []string{"Explore", "Plan", "claude-code-guide"}

var writeTools = []string{"Edit", "Write", "NotebookEdit"}

// Parse reads a payload. harness selects the shape; "" detects it from the keys.
func Parse(harness string, payload []byte) (*Call, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("payload is not JSON: %w", err)
	}
	if harness == "" {
		_, toolCall := raw["toolCall"]
		_, workspacePaths := raw["workspacePaths"]
		_, invocationNum := raw["invocationNum"]
		switch {
		case toolCall || workspacePaths || invocationNum:
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
		c.Prompt, _ = raw["prompt"].(string)
		c.TranscriptPath, _ = raw["transcript_path"].(string)
		c.Tool, _ = raw["tool_name"].(string)
		if in, ok := raw["tool_input"].(map[string]any); ok {
			c.Input = in
		}
	case "grok":
		if event, ok := raw["hookEventName"]; ok && event != "PreToolUse" && event != "pre_tool_use" {
			c.Event = "ignore"
		}
		c.Tool, _ = raw["toolName"].(string)
		c.TranscriptPath, _ = raw["transcriptPath"].(string)
		if in, ok := raw["toolInput"].(map[string]any); ok {
			c.Input = in
		}
	case "agy":
		c.TranscriptPath, _ = raw["transcriptPath"].(string)
		if paths, ok := raw["workspacePaths"].([]any); ok {
			for _, path := range paths {
				if path, ok := path.(string); ok {
					c.WorkspacePaths = append(c.WorkspacePaths, path)
				}
			}
		}
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
	if isReminderEvent(c, raw) {
		c.Event = "reminder"
		if c.Harness == "agy" {
			c.CWD = ""
			if len(c.WorkspacePaths) > 0 {
				c.CWD = c.WorkspacePaths[0]
			}
		}
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
		return Decision{Allow: false, Reason: "Code-writing Solo lanes go through Maestro: write a Brief file and run `maestro dispatch <brief.md>`, or a Plan file and `maestro run <plan.md>`. A read-only spawn passes when its kind is terminal, or when extra_args carries a read token: `dontAsk`, `read-only`, or `--deny Write`."}
	case tool == "Agent" || tool == "Task" || tool == "spawn_subagent" || tool == "invoke_subagent":
		sub, _ := c.Input["subagent_type"].(string)
		if sub == "" {
			sub, _ = c.Input["agent"].(string)
		}
		if readOnlyAgent(sub, c.CWD) {
			return Decision{Allow: true}
		}
		return Decision{Allow: false, Reason: fmt.Sprintf("Sub-agent %q may write code; Maestro routes that work. Write a Brief file and run `maestro dispatch <brief.md>`. Read-only legwork may stay on a read-only agent type: %s, or a custom agent whose definition file keeps %s out of its tools.", nonEmpty(sub, tool), strings.Join(readOnlyAgents, ", "), strings.Join(writeTools, ", "))}
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
	if harness != "agy" {
		switch v.Kind {
		case "auto_approve":
			v.Kind = "allow"
		case "relay":
			v = Verdict{Kind: "deny", Reason: "relay is only for Antigravity"}
		}
	}
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

// Custom agents need an explicit read-only list because omitted tools can
// inherit Bash or MCP tools.
func readOnlyAgent(sub, cwd string) bool {
	for _, agent := range readOnlyAgents {
		if strings.EqualFold(sub, agent) {
			return true
		}
	}
	tools, _, ok := agentTools(sub, cwd)
	if !ok || tools == nil {
		return false
	}
	for _, tool := range tools {
		switch tool {
		case "Read", "Glob", "Grep", "LS", "WebFetch", "WebSearch", "TodoWrite":
		default:
			return false
		}
	}
	return true
}

// agentTools reads the `tools` and `disallowedTools` front matter of the
// agent definition `<name>.md`, looked up under `.claude/agents` from cwd
// upward and then in the Claude home. A missing list stays nil.
func agentTools(name, cwd string) (tools, disallowed []string, ok bool) {
	if name == "" || strings.ContainsAny(name, `/\`) {
		return nil, nil, false
	}
	var raw []byte
	for _, dir := range agentDirs(cwd) {
		b, err := os.ReadFile(filepath.Join(dir, name+".md"))
		if err == nil {
			raw = b
			break
		}
	}
	if raw == nil {
		return nil, nil, false
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, nil, false
	}
	end := 0
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			end = i + 1
			break
		}
	}
	if end == 0 {
		return nil, nil, false
	}
	var front struct {
		Tools      yaml.Node `yaml:"tools"`
		Disallowed yaml.Node `yaml:"disallowedTools"`
	}
	if yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &front) != nil {
		return nil, nil, false
	}
	return toolList(front.Tools), toolList(front.Disallowed), true
}

func agentDirs(cwd string) []string {
	var dirs []string
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if dir, err := filepath.Abs(cwd); err == nil {
		for {
			dirs = append(dirs, filepath.Join(dir, ".claude", "agents"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(config.ClaudeHome(home), "agents"))
	}
	return dirs
}

func toolList(value yaml.Node) []string {
	if value.Kind == yaml.SequenceNode {
		out := []string{}
		for _, item := range value.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
				return nil
			}
			out = append(out, item.Value)
		}
		return out
	}
	if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
		return nil
	}
	out := []string{}
	for _, item := range strings.Split(value.Value, ",") {
		if item = strings.Trim(strings.TrimSpace(item), `"'`); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func hasAny(list, wanted []string) bool {
	for _, w := range wanted {
		for _, item := range list {
			if strings.EqualFold(item, w) {
				return true
			}
		}
	}
	return false
}

func hasAll(list, wanted []string) bool {
	for _, w := range wanted {
		if !hasAny(list, []string{w}) {
			return false
		}
	}
	return true
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
