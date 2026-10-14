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

// Enforcement: the Maestro hook holds both shapes, as it does for Antigravity.
func (g *Grok) Enforcement(access string) string {
	if access == "read" || access == "edit" {
		return Enforced
	}
	return NoShape
}

// Build: the Pointer goes through --prompt-file (Grok reads no stdin prompt).
func (g *Grok) Build(l Launch) (*Command, error) {
	return g.build(l, false)
}

func (g *Grok) BuildTab(l Launch) (*Command, error) {
	return g.build(l, true)
}

func (g *Grok) build(l Launch, tab bool) (*Command, error) {
	if g.bin == "" {
		return nil, fmt.Errorf("grok is not installed")
	}
	promptFile := filepath.Join(l.RunFolder, "pointer.txt")
	if !tab && l.RunFolder != "" && l.Pointer != "" {
		if err := os.WriteFile(promptFile, []byte(l.Pointer+"\n"), 0o644); err != nil {
			return nil, err
		}
	}
	args := []string{"--prompt-file", promptFile, "--output-format", "json", "--no-subagents", "--model", l.Model}
	if tab {
		args = []string{l.Pointer, "--model", l.Model, "--no-subagents", "--cwd", l.Root}
	}
	if l.Effort != "" {
		args = append(args, "--reasoning-effort", l.Effort)
	}
	// Grok runs a call under dontAsk only when an allow rule or its built-in
	// read-only list approves it; the Maestro hook can deny but never
	// approve. A deny rule here would win over every allow rule, so the
	// shapes pass allow rules only. Grok path rules take the absolute path
	// as written: a leading `//` is literal glob text there, not an anchor.
	// The workspace sandbox confines every subprocess write to the project
	// at the kernel level; it fits only when every writable directory is
	// inside the project root.
	var flags []string
	writable := []string{l.RunFolder}
	if l.Access == "edit" {
		writable = append(writable, l.Dirs...)
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
	case "read":
		flags = append(flags, "--permission-mode", "dontAsk")
		for _, rule := range readTools {
			flags = append(flags, "--allow", rule)
		}
		// Like Claude, a read Lane can write its Report in its Run folder.
		if l.RunFolder != "" {
			flags = append(flags, grokWriteRules(l.RunFolder)...)
		}
	case "edit":
		flags = append(flags, "--permission-mode", "dontAsk")
		for _, dir := range append([]string{l.Root}, writable...) {
			if dir != "" {
				flags = append(flags, grokWriteRules(dir)...)
			}
		}
		for _, cmd := range l.AllowedCommands {
			flags = append(flags, "--allow", "Bash("+cmd+" *)", "--allow", "Bash("+cmd+")")
		}
		if l.AllowCommit {
			for _, verb := range []string{"add", "commit"} {
				flags = append(flags, "--allow", "Bash(git "+verb+" *)")
			}
		}
		for _, rule := range l.AllowRules {
			flags = append(flags, "--allow", rule)
		}
	default:
		return nil, fmt.Errorf("grok has no %q shape", l.Access)
	}
	args = append(args, flags...)
	cmd := exec.Command(g.bin, args...)
	cmd.Dir = l.Root
	cmd.Env = l.Env
	return &Command{Cmd: cmd, Flags: flags, PromptStdin: false, ReportByText: l.Access == "read" && !tab}, nil
}

// grokWriteRules grants file writes under dir, as an absolute path.
func grokWriteRules(dir string) []string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return []string{"--allow", fmt.Sprintf("Write(%s/**)", dir), "--allow", fmt.Sprintf("Edit(%s/**)", dir)}
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

// grokParser reads the one JSON object Grok prints at the end; progress is
// any non-empty line.
type grokParser struct {
	out *Outcome
	buf strings.Builder
}

func (p *grokParser) Outcome() *Outcome {
	text := strings.TrimSpace(p.buf.String())
	if text == "" {
		return p.out
	}
	var doc struct {
		Text      string `json:"text"`
		Stop      string `json:"stopReason"`
		SessionID string `json:"sessionId"`
		Thought   string `json:"thought"`
		Usage     struct {
			Input     *int `json:"input_tokens"`
			CacheRead *int `json:"cache_read_input_tokens"`
			Output    *int `json:"output_tokens"`
			Reasoning *int `json:"reasoning_tokens"`
		} `json:"usage"`
		Cost       *float64                   `json:"total_cost_usd"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
		Error      string                     `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		// Not JSON: an error message on stdout.
		p.out.IsError = true
		p.out.ErrorText = cutText(text, 500)
		p.out.Signals = append(p.out.Signals, classifyText("grok", text))
		return p.out
	}
	p.out.SessionID = doc.SessionID
	p.out.Text = doc.Text
	p.out.CostUSD = doc.Cost
	p.out.Tokens = Tokens{Input: doc.Usage.Input, CachedInput: doc.Usage.CacheRead, Output: doc.Usage.Output, Reasoning: doc.Usage.Reasoning}
	for model := range doc.ModelUsage {
		p.out.ModelRan = model
	}
	if doc.Error != "" {
		p.out.IsError = true
		p.out.ErrorText = doc.Error
		p.out.Signals = append(p.out.Signals, classifyText("grok", doc.Error))
	}
	// Grok exits 0 on a denial; the thought text is the only trace.
	if strings.Contains(strings.ToLower(doc.Thought), "denied") || strings.Contains(strings.ToLower(doc.Thought), "permission") {
		p.out.Denials = append(p.out.Denials, Denial{Tool: "thought", Input: cutText(doc.Thought, 300)})
	}
	return p.out
}

func (p *grokParser) Line(line []byte) bool {
	p.buf.Write(line)
	p.buf.WriteByte('\n')
	return len(strings.TrimSpace(string(line))) > 0
}

func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
