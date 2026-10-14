package harness

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Codex is the Codex CLI adapter.
type Codex struct{ bin string }

func (c *Codex) Name() string { return "codex" }
func (c *Codex) Bin() string  { return c.bin }

func (c *Codex) Version() (string, error) { return versionOf(c.bin) }

// Catalog runs `codex debug models`, which prints JSON.
func (c *Codex) Catalog() ([]Model, string, error) {
	if c.bin == "" {
		return nil, "", fmt.Errorf("not installed")
	}
	out, err := exec.Command(c.bin, "debug", "models").Output()
	if err != nil {
		return nil, "", fmt.Errorf("codex debug models: %w", err)
	}
	var doc struct {
		Models []struct {
			Slug          string `json:"slug"`
			Default       string `json:"default_reasoning_level"`
			ContextWindow int    `json:"context_window"`
			Levels        []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, "", fmt.Errorf("codex debug models: %w", err)
	}
	var models []Model
	for _, m := range doc.Models {
		var efforts []string
		for _, l := range m.Levels {
			efforts = append(efforts, l.Effort)
		}
		models = append(models, Model{ID: m.Slug, Efforts: efforts, DefaultEffort: m.Default, Context: m.ContextWindow})
	}
	return models, "", nil
}

// Enforcement: the sandbox holds both shapes. read-only blocks every write,
// and workspace-write confines writes to the project, the Run folder, and
// the extra directories.
func (c *Codex) Enforcement(access string) string {
	if access == "read" || access == "edit" {
		return Enforced
	}
	return NoShape
}

func (c *Codex) Build(l Launch) (*Command, error) {
	return c.build(l, false)
}

func (c *Codex) BuildTab(l Launch) (*Command, error) {
	return c.build(l, true)
}

func (c *Codex) build(l Launch, tab bool) (*Command, error) {
	if c.bin == "" {
		return nil, fmt.Errorf("codex is not installed")
	}
	args := []string{"exec", "--json", "-m", l.Model, "--skip-git-repo-check", "-C", l.Root, "-c", "approval_policy=never"}
	root := l.Root
	if tab {
		if l.Access == "read" {
			root = l.RunFolder
		}
		args = []string{l.Pointer, "-m", l.Model, "-C", root, "-c", "approval_policy=never"}
		projectRoot := l.Root
		if resolved, err := filepath.EvalSymlinks(projectRoot); err == nil {
			projectRoot = resolved
		}
		// An inline table preserves dots and quotes in the project path; the
		// CLI splits dotted override keys without parsing quoted TOML keys.
		args = append(args, "-c", fmt.Sprintf("projects={%q={trust_level=\"trusted\"}}", projectRoot))
	}
	if l.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+l.Effort)
	}
	var flags []string
	reportByText := false
	switch l.Access {
	case "read":
		// The read-only sandbox blocks every write, the Report included, so
		// the Lane's last message is the Report and Codex writes it for us.
		flags = append(flags, "-s", "read-only", "-o", l.ReportPath)
		reportByText = true
		if tab {
			flags = []string{"-s", "workspace-write"}
			reportByText = false
		}
	case "edit":
		// The sandbox protects .agents/ like .git/, so the Run folder is opened by name.
		flags = append(flags, "-s", "workspace-write", "-c", "sandbox_workspace_write.network_access=true", "--add-dir", l.RunFolder)
		for _, d := range l.Dirs {
			flags = append(flags, "--add-dir", d)
		}
	default:
		return nil, fmt.Errorf("codex has no %q shape", l.Access)
	}
	args = append(args, flags...)
	if !tab {
		args = append(args, "-")
	}
	cmd := exec.Command(c.bin, args...)
	cmd.Dir = root
	cmd.Env = l.Env
	return &Command{Cmd: cmd, Flags: flags, PromptStdin: !tab, ReportByText: reportByText}, nil
}

func (c *Codex) NewParser() Parser { return &codexParser{out: &Outcome{}} }

type codexParser struct{ out *Outcome }

func (p *codexParser) Outcome() *Outcome { return p.out }

func (p *codexParser) Line(line []byte) bool {
	var ev struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread_id"`
		Item     struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
		Usage struct {
			Input     *int `json:"input_tokens"`
			Cached    *int `json:"cached_input_tokens"`
			Output    *int `json:"output_tokens"`
			Reasoning *int `json:"reasoning_output_tokens"`
		} `json:"usage"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(line, &ev); err != nil {
		return len(strings.TrimSpace(string(line))) > 0
	}
	switch ev.Type {
	case "thread.started":
		p.out.SessionID = ev.ThreadID
		return false
	case "item.completed", "item.started", "item.updated":
		if ev.Item.Type == "agent_message" && ev.Item.Text != "" {
			p.out.Text = ev.Item.Text
		}
		return true
	case "turn.completed":
		p.out.Tokens = Tokens{Input: ev.Usage.Input, CachedInput: ev.Usage.Cached, Output: ev.Usage.Output, Reasoning: ev.Usage.Reasoning}
		return true
	case "turn.failed", "error":
		text := ev.Error.Message
		if text == "" {
			text = ev.Message
		}
		p.out.IsError = true
		p.out.ErrorText = text
		p.out.Signals = append(p.out.Signals, classifyText("codex", text))
		return true
	}
	return false
}
