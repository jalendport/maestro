package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Grok is the Grok CLI adapter. Its read shape uses a tool allowlist.
type Grok struct{ bin string }

func (g *Grok) Name() string { return "grok" }
func (g *Grok) Bin() string  { return g.bin }

func (g *Grok) Version() (string, error) { return versionOf(g.bin) }

var grokModelRe = regexp.MustCompile(`(?m)^\s+[*-]\s+(\S+)`)

// Catalog parses the plain text of `grok models`.
func (g *Grok) Catalog() ([]Model, string, error) {
	if g.bin == "" {
		return nil, "", fmt.Errorf("not installed")
	}
	out, err := exec.Command(g.bin, "models").CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, "", err
	}
	var models []Model
	for _, m := range grokModelRe.FindAllStringSubmatch(string(out), -1) {
		models = append(models, Model{ID: m[1], Efforts: []string{"low", "medium", "high", "xhigh"}})
	}
	hint := ""
	if m := regexp.MustCompile(`logged in with (\S+)`).FindStringSubmatch(string(out)); m != nil {
		hint = strings.TrimSuffix(m[1], ".")
	}
	return models, hint, nil
}

// Enforcement: the Maestro hook and workspace sandbox hold both shapes.
// Grok warns and continues if the kernel cannot apply a built-in profile.
// A launch with an extra directory outside the project is best-effort.
func (g *Grok) Enforcement(access string) string {
	if access == "read" || access == "edit" {
		return Enforced
	}
	return NoShape
}

// Build: the Pointer goes through --prompt-file (Grok reads no stdin prompt).
func (g *Grok) Build(l Launch) (*Command, error) {
	if g.bin == "" {
		return nil, fmt.Errorf("grok is not installed")
	}
	promptFile := filepath.Join(l.RunFolder, "pointer.txt")
	if l.RunFolder != "" && l.Pointer != "" {
		if err := os.WriteFile(promptFile, []byte(l.Pointer+"\n"), 0o644); err != nil {
			return nil, err
		}
	}
	args := []string{"--prompt-file", promptFile, "--output-format", "streaming-json", "--no-subagents", "--model", l.Model}
	if l.Effort != "" {
		args = append(args, "--reasoning-effort", l.Effort)
	}
	// Grok has no mode that is prompt-free and deny-by-default: `dontAsk` is a
	// Claude name Grok accepts but maps to `default`. Headless mode runs
	// with full tool access. So a Lane runs under
	// always-approve, and the Maestro hook is the policy: it runs before
	// every call and denies what the Lane policy in lane.json does not allow
	// (hooks and deny rules still apply in always-approve). The workspace
	// sandbox backs the hook at the kernel level; it fits only when every
	// writable directory is inside the project root.
	var flags []string
	reportByText := l.Access == "read" || !within(l.RunFolder, l.Root)
	writable := []string{}
	if !reportByText {
		writable = append(writable, l.RunFolder)
	}
	if l.Access == "edit" {
		for _, dir := range l.Dirs {
			// Dispatch adds the Run folder for Worktrees. A message Report
			// leaves that folder outside the Lane's writable directories.
			if reportByText && dir == l.RunFolder {
				continue
			}
			writable = append(writable, dir)
		}
	}
	sandboxed := l.Root != ""
	for _, dir := range writable {
		if dir != "" && !within(dir, l.Root) {
			sandboxed = false
		}
	}
	if sandboxed {
		flags = append(flags, "--sandbox", "workspace")
	}
	switch l.Access {
	case "read", "edit":
		flags = append(flags, "--permission-mode", "bypassPermissions")
	default:
		return nil, fmt.Errorf("grok has no %q shape", l.Access)
	}
	args = append(args, flags...)
	cmd := exec.Command(g.bin, args...)
	cmd.Dir = l.Root
	cmd.Env = l.Env
	enforcement := Enforced
	if !sandboxed {
		enforcement = BestEffort
	}
	return &Command{Cmd: cmd, Enforcement: enforcement, Flags: flags, PromptStdin: false, ReportByText: reportByText}, nil
}

// within reports whether path sits inside root, by path text alone.
func within(path, root string) bool {
	if root == "" {
		return false
	}
	absPath, err1 := filepath.Abs(path)
	absRoot, err2 := filepath.Abs(root)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func (g *Grok) NewParser() Parser { return &grokParser{out: &Outcome{}} }

// Grok's event list is open, so every non-empty line counts as progress.
type grokParser struct {
	toolCalls
	out           *Outcome
	text          strings.Builder
	thought       strings.Builder
	thoughtDenied bool
	textBreak     bool
}

func (p *grokParser) Outcome() *Outcome { return p.out }

func (p *grokParser) Line(line []byte) bool {
	text := strings.TrimSpace(string(line))
	if text == "" {
		return false
	}
	var event struct {
		Type       string `json:"type"`
		ToolCallID string `json:"toolCallId"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		p.out.IsError = true
		p.out.ErrorText = cutText(text, 500)
		p.out.Signals = append(p.out.Signals, classifyText("grok", text))
		return true
	}
	switch event.Type {
	case "text", "thought", "end", "error":
	case "tool_call_update":
		if event.Status == "completed" || event.Status == "failed" {
			p.finish(event.ToolCallID)
		}
		return true
	case "tool_call", "usage":
		if event.Type == "tool_call" {
			p.start(event.ToolCallID)
		}
		// A tool call or a response boundary ends one stretch of text. Other
		// events can arrive in the middle of a response and must not split it.
		p.textBreak = true
		return true
	default:
		return true
	}
	var doc struct {
		Data      string  `json:"data"`
		SessionID *string `json:"sessionId"`
		Usage     struct {
			Input     *int `json:"input_tokens"`
			CacheRead *int `json:"cache_read_input_tokens"`
			Output    *int `json:"output_tokens"`
			Reasoning *int `json:"reasoning_tokens"`
		} `json:"usage"`
		Cost       *float64                   `json:"total_cost_usd"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
		Error      string                     `json:"error"`
		Message    string                     `json:"message"`
	}
	if err := json.Unmarshal(line, &doc); err != nil {
		p.out.IsError = true
		p.out.ErrorText = cutText(text, 500)
		p.out.Signals = append(p.out.Signals, classifyText("grok", text))
		return true
	}
	switch event.Type {
	case "text":
		if p.textBreak && p.text.Len() > 0 {
			p.text.WriteString("\n\n")
		}
		p.textBreak = false
		p.text.WriteString(doc.Data)
		p.out.Text = p.text.String()
	case "thought":
		// Grok exits 0 on a denial; the thought text is the only trace.
		p.thought.WriteString(doc.Data)
		thought := strings.ToLower(p.thought.String())
		if !p.thoughtDenied && (strings.Contains(thought, "denied") || strings.Contains(thought, "permission")) {
			p.out.Denials = append(p.out.Denials, Denial{Tool: "thought", Input: cutText(p.thought.String(), 300)})
			p.thoughtDenied = true
		}
	case "end", "error":
		if doc.SessionID != nil {
			p.out.SessionID = *doc.SessionID
		}
		if doc.Cost != nil {
			p.out.CostUSD = doc.Cost
		}
		if doc.Usage.Input != nil {
			p.out.Tokens.Input = doc.Usage.Input
		}
		if doc.Usage.CacheRead != nil {
			p.out.Tokens.CachedInput = doc.Usage.CacheRead
		}
		if doc.Usage.Output != nil {
			p.out.Tokens.Output = doc.Usage.Output
		}
		if doc.Usage.Reasoning != nil {
			p.out.Tokens.Reasoning = doc.Usage.Reasoning
		}
		for model := range doc.ModelUsage {
			p.out.ModelRan = model
		}
		if event.Type == "error" || doc.Error != "" {
			errorText := doc.Error
			if errorText == "" {
				errorText = doc.Message
			}
			p.out.IsError = true
			p.out.ErrorText = errorText
			p.out.Signals = append(p.out.Signals, classifyText("grok", errorText))
		}
	}
	return true
}

func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
