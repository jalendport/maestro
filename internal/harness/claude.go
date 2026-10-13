package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Claude is the Claude Code adapter.
type Claude struct{ bin string }

func (c *Claude) Name() string { return "claude" }
func (c *Claude) Bin() string  { return c.bin }

func (c *Claude) Version() (string, error) { return versionOf(c.bin) }

// Catalog: Claude has no list command; the aliases are the catalog. The
// roster resolves a family's alias through the embedded alias table.
func (c *Claude) Catalog() ([]Model, string, error) {
	if c.bin == "" {
		return nil, "", fmt.Errorf("not installed")
	}
	efforts := []string{"low", "medium", "high", "xhigh"}
	return []Model{
		{ID: "haiku", Context: 200000},
		{ID: "sonnet", Efforts: efforts, Context: 1000000},
		{ID: "opus", Efforts: efforts, DefaultEffort: "medium", Context: 1000000},
		{ID: "fable", Efforts: efforts, DefaultEffort: "high", Context: 1000000},
	}, "", nil
}

var claudeModelID = regexp.MustCompile(`^claude-[a-z][a-z0-9]*-[0-9]+-[0-9]+(?:-[0-9]{8})?$`)

// ClaudeModelID reports whether an id has the CLI's full model-id shape.
func ClaudeModelID(id string) bool { return claudeModelID.MatchString(id) }

func (c *Claude) Enforcement(access string) string {
	switch access {
	case "read":
		return Enforced
	case "edit":
		return BestEffort
	}
	return NoShape
}

// readTools is the allowlist for a read Lane: repository read tools,
// read-only git, ls and cat, web. The Run folder is added by Build so the
// Lane can write its Report.
var readTools = []string{
	"Read", "Glob", "Grep", "LS", "WebFetch", "WebSearch", "TodoWrite",
	"Bash(git diff *)", "Bash(git log *)", "Bash(git show *)", "Bash(git status *)", "Bash(git blame *)",
	"Bash(ls *)", "Bash(cat *)", "Bash(ls)", "Bash(git status)",
}

func (c *Claude) Build(l Launch) (*Command, error) {
	return c.build(l, false)
}

func (c *Claude) BuildTab(l Launch) (*Command, error) {
	return c.build(l, true)
}

func (c *Claude) build(l Launch, tab bool) (*Command, error) {
	if c.bin == "" {
		return nil, fmt.Errorf("claude is not installed")
	}
	args := []string{"-p", "--model", l.Model, "--output-format", "stream-json", "--verbose", "--permission-prompts", "none"}
	if tab {
		args = []string{l.Pointer, "--model", l.Model}
	}
	if l.Effort != "" {
		args = append(args, "--effort", l.Effort)
	}
	var flags []string
	switch l.Access {
	case "read":
		tools := append([]string{}, readTools...)
		tools = append(tools, fmt.Sprintf("Write(//%s/**)", l.RunFolder), fmt.Sprintf("Edit(//%s/**)", l.RunFolder))
		if l.InSolo {
			tools = append(tools, "mcp__solo__whoami", "mcp__solo__get_process_status", "mcp__solo__get_process_output", "mcp__solo__todo_comment_create")
		}
		flags = append(flags, "--setting-sources", "", "--permission-mode", "dontAsk", "--allowedTools")
		flags = append(flags, tools...)
	case "edit":
		mode := "acceptEdits"
		if l.ClaudeAuto {
			mode = "auto"
		}
		tools := []string{"Bash(git diff *)", "Bash(git log *)", "Bash(git show *)", "Bash(git status *)", "Bash(git blame *)", "Bash(git status)", "Bash(git diff)", "Bash(ls *)", "Bash(cat *)", "WebFetch", "WebSearch"}
		tools = append(tools, l.AllowRules...)
		for _, cmd := range l.AllowedCommands {
			tools = append(tools, "Bash("+cmd+")", "Bash("+cmd+" *)")
		}
		if l.AllowCommit {
			tools = append(tools, "Bash(git add *)", "Bash(git commit *)")
		}
		if l.InSolo {
			tools = append(tools, "mcp__solo__*")
		}
		flags = append(flags, "--permission-mode", mode, "--allowedTools")
		flags = append(flags, tools...)
		for _, d := range l.Dirs {
			flags = append(flags, "--add-dir", d)
		}
	default:
		return nil, fmt.Errorf("claude has no %q shape", l.Access)
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	taskID := l.TaskID
	if taskID == "" {
		taskID = filepath.Base(l.RunFolder)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	settings, err := json.Marshal(map[string]any{
		"env": map[string]string{"MAESTRO_TASK_FOLDER": l.RunFolder},
		"hooks": map[string]any{"PermissionRequest": []any{map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": quote(self) + " relay " + quote(taskID), "timeout": 660}},
		}}},
	})
	if err != nil {
		return nil, err
	}
	flags = append(flags, "--settings", string(settings))
	args = append(args, flags...)
	cmd := exec.Command(c.bin, args...)
	cmd.Dir = l.Root
	cmd.Env = l.Env
	return &Command{Cmd: cmd, Flags: flags, PromptStdin: !tab}, nil
}

func (c *Claude) NewParser() Parser { return &claudeParser{out: &Outcome{}} }

type claudeParser struct{ out *Outcome }

func (p *claudeParser) Outcome() *Outcome { return p.out }

func (p *claudeParser) Line(line []byte) bool {
	var ev struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
		Model     string `json:"model"`
		IsError   bool   `json:"is_error"`
		Result    string `json:"result"`
		Usage     struct {
			Input         *int `json:"input_tokens"`
			CacheRead     *int `json:"cache_read_input_tokens"`
			CacheCreation *int `json:"cache_creation_input_tokens"`
			Output        *int `json:"output_tokens"`
			OutputDetails struct {
				Thinking *int `json:"thinking_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
		TotalCost  *float64                   `json:"total_cost_usd"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
		Denials    []struct {
			Tool  string          `json:"tool_name"`
			Input json.RawMessage `json:"tool_input"`
		} `json:"permission_denials"`
		RateLimit struct {
			Status  string `json:"status"`
			Windows map[string]struct {
				Utilization float64 `json:"utilization"`
				ResetsAt    int64   `json:"resetsAt"`
			} `json:"unifiedWindows"`
		} `json:"rate_limit_info"`
		// system/api_retry events carry the error kind here.
		ErrorKind string `json:"error"`
	}
	if err := json.Unmarshal(line, &ev); err != nil {
		return len(strings.TrimSpace(string(line))) > 0
	}
	if ev.SessionID != "" && p.out.SessionID == "" {
		p.out.SessionID = ev.SessionID
	}
	switch ev.Type {
	case "system":
		switch ev.Subtype {
		case "init":
			if ev.Model != "" {
				p.out.ModelRan = ev.Model
			}
		case "api_retry":
			switch ev.ErrorKind {
			case "rate_limit", "overloaded":
				p.out.Signals = append(p.out.Signals, Signal{Class: "transient", Text: "api_retry " + ev.ErrorKind})
			case "billing_error":
				p.out.Signals = append(p.out.Signals, Signal{Class: "lane", Text: "api_retry billing_error"})
			}
		}
		return false
	case "rate_limit_event":
		for kind, w := range ev.RateLimit.Windows {
			k := map[string]string{"five_hour": "session", "seven_day": "weekly"}[kind]
			if k == "" {
				k = kind
			}
			p.out.Windows = append(p.out.Windows, Window{Kind: k, Used: w.Utilization, ResetsAt: time.Unix(w.ResetsAt, 0)})
		}
		if ev.RateLimit.Status == "rejected" {
			p.out.Signals = append(p.out.Signals, Signal{Class: "limit", Text: "rate_limit_event rejected"})
		}
		return false
	case "assistant", "user", "tool_use", "tool_result", "stream_event":
		return true
	case "result":
		p.out.IsError = ev.IsError || (ev.Subtype != "" && ev.Subtype != "success")
		p.out.Text = ev.Result
		if p.out.IsError {
			text := ev.Result
			if text == "" {
				text = ev.Subtype
			}
			p.out.ErrorText = text
			p.out.Signals = append(p.out.Signals, classifyText("claude", text))
		}
		p.out.CostUSD = ev.TotalCost
		p.out.Tokens = Tokens{Input: ev.Usage.Input, CachedInput: ev.Usage.CacheRead, Output: ev.Usage.Output, Reasoning: ev.Usage.OutputDetails.Thinking}
		for model := range ev.ModelUsage {
			// One Lane runs one model; a safeguard downgrade shows up here.
			p.out.ModelRan = model
		}
		for _, d := range ev.Denials {
			p.out.Denials = append(p.out.Denials, Denial{Tool: d.Tool, Input: string(d.Input)})
		}
		return true
	}
	return false
}
