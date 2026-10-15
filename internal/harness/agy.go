package harness

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Agy is the Antigravity CLI adapter. Maestro's hook holds the Lane's Access.
type Agy struct{ bin string }

func (a *Agy) Name() string { return "agy" }
func (a *Agy) Bin() string  { return a.bin }

func (a *Agy) Version() (string, error) { return versionOf(a.bin) }

var agyModelRe = regexp.MustCompile(`^(gemini-[\w.-]+?)-(low|medium|high)\t`)

// Catalog parses the TSV of `agy models`. Effort-named rows collapse into one
// model with an effort list, which matches the family stanza shape.
func (a *Agy) Catalog() ([]Model, string, error) {
	if a.bin == "" {
		return nil, "", fmt.Errorf("not installed")
	}
	out, err := exec.Command(a.bin, "models").CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, "", err
	}
	byID := map[string]*Model{}
	var order []string
	for _, line := range strings.Split(string(out), "\n") {
		m := agyModelRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if byID[m[1]] == nil {
			byID[m[1]] = &Model{ID: m[1]}
			order = append(order, m[1])
		}
		byID[m[1]].Efforts = append(byID[m[1]].Efforts, m[2])
	}
	var models []Model
	for _, id := range order {
		models = append(models, *byID[id])
	}
	return models, "", nil
}

func (a *Agy) Enforcement(access string) string {
	if access == "read" || access == "edit" {
		return Enforced
	}
	return NoShape
}

// Build folds the effort into the model name for a one-shot Dispatch.
func (a *Agy) Build(l Launch) (*Command, error) {
	return a.build(l, false)
}

func (a *Agy) BuildTab(l Launch) (*Command, error) {
	return a.build(l, true)
}

func (a *Agy) build(l Launch, tab bool) (*Command, error) {
	if a.bin == "" {
		return nil, fmt.Errorf("agy is not installed")
	}
	if l.Access != "read" && l.Access != "edit" {
		return nil, fmt.Errorf("agy has no %q shape", l.Access)
	}
	model := l.Model
	if l.Effort != "" && !strings.HasSuffix(model, "-"+l.Effort) {
		model += "-" + l.Effort
	}
	timeout := "0s"
	if l.WallLimit > 0 {
		timeout = l.WallLimit.String()
	}
	args := []string{"--sandbox", "--output-format", "json", "--model", model, "--print-timeout", timeout, "--disable-slash-commands"}
	flags := []string{"--sandbox"}
	if tab {
		args = []string{"--sandbox", "--model", l.Model}
		if l.Effort != "" {
			args = append(args, "--effort", l.Effort)
		}
	}
	if l.Access == "edit" {
		flags = append(flags, "--mode", "accept-edits")
		for _, dir := range l.Dirs {
			flags = append(flags, "--add-dir", dir)
		}
	}
	args = append(args, flags[1:]...)
	if tab {
		args = append(args, "--prompt-interactive", l.Pointer)
	} else {
		args = append(args, "-p", l.Pointer)
	}
	cmd := exec.Command(a.bin, args...)
	cmd.Dir = l.Root
	cmd.Env = l.Env
	return &Command{Cmd: cmd, Enforcement: a.Enforcement(l.Access), Flags: flags}, nil
}

func (a *Agy) NewParser() Parser { return &agyParser{out: &Outcome{}} }

type agyParser struct {
	out *Outcome
	buf strings.Builder
}

func (p *agyParser) Outcome() *Outcome {
	text := strings.TrimSpace(p.buf.String())
	if text == "" {
		return p.out
	}
	var doc struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Response       string `json:"response"`
		Error          string `json:"error"`
		Usage          struct {
			Input     *int `json:"input_tokens"`
			Output    *int `json:"output_tokens"`
			Thinking  *int `json:"thinking_tokens"`
			CacheRead *int `json:"cache_read_tokens"`
		} `json:"usage"`
		Denied []json.RawMessage `json:"denied_actions"`
	}
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		p.out.IsError = true
		p.out.ErrorText = cutText(text, 500)
		p.out.Signals = append(p.out.Signals, classifyText("agy", text))
		return p.out
	}
	p.out.SessionID = doc.ConversationID
	p.out.Text = doc.Response
	p.out.Tokens = Tokens{Input: doc.Usage.Input, CachedInput: doc.Usage.CacheRead, Output: doc.Usage.Output, Reasoning: doc.Usage.Thinking}
	for _, d := range doc.Denied {
		p.out.Denials = append(p.out.Denials, Denial{Tool: "agy", Input: cutText(string(d), 300)})
	}
	// Denials with an empty response = a failed Dispatch, never a success.
	if len(doc.Denied) > 0 && strings.TrimSpace(doc.Response) == "" {
		p.out.IsError = true
		p.out.ErrorText = "agy denied an action and returned no response"
		p.out.Signals = append(p.out.Signals, Signal{Class: "bad", Text: p.out.ErrorText})
	}
	if doc.Status != "" && doc.Status != "SUCCESS" {
		p.out.IsError = true
		if p.out.ErrorText == "" {
			p.out.ErrorText = doc.Status + " " + doc.Error
		}
		p.out.Signals = append(p.out.Signals, classifyText("agy", doc.Status+" "+doc.Error))
	}
	return p.out
}

func (p *agyParser) Line(line []byte) bool {
	p.buf.Write(line)
	p.buf.WriteByte('\n')
	return len(strings.TrimSpace(string(line))) > 0
}
