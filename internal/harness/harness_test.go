package harness

import (
	"os"
	"strings"
	"testing"
)

func feed(t *testing.T, p Parser, path string) *Outcome {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skip("no fixture", path)
	}
	for _, line := range strings.Split(string(b), "\n") {
		p.Line([]byte(line))
	}
	return p.Outcome()
}

func TestClaudeParserFixture(t *testing.T) {
	o := feed(t, (&Claude{}).NewParser(), "testdata/claude-stream.jsonl")
	if o.SessionID == "" || o.ModelRan == "" || o.Text != "OK" || o.IsError {
		t.Fatalf("%+v", o)
	}
	if len(o.Windows) != 2 || o.Tokens.Output == nil || *o.Tokens.Output != 4 {
		t.Fatalf("windows %v tokens %+v", o.Windows, o.Tokens)
	}
}

func TestCodexParserFixture(t *testing.T) {
	o := feed(t, (&Codex{}).NewParser(), "testdata/codex-stream.jsonl")
	if o.SessionID == "" || o.Text != "OK" || o.IsError || o.Tokens.Input == nil {
		t.Fatalf("%+v", o)
	}
}

func TestVersionLess(t *testing.T) {
	if !VersionLess("0.152.1", "0.156.1") || VersionLess("0.162.0", "0.156.1") || VersionLess("2.1.296", "2.1.296") {
		t.Fatal("version compare")
	}
}

func TestClassify(t *testing.T) {
	if classifyText("codex", "You've hit your usage limit").Class != "limit" {
		t.Fatal("limit")
	}
	if classifyText("codex", "something odd").Class != "transient" {
		t.Fatal("unknown must be transient")
	}
}

func TestGrokParserFixture(t *testing.T) {
	o := feed(t, (&Grok{}).NewParser(), "testdata/grok-out.json")
	if o.SessionID == "" || o.Text != "OK" || o.ModelRan != "grok-4.7-build" || o.IsError || o.CostUSD == nil {
		t.Fatalf("%+v", o)
	}
}

func TestAgyParserFixture(t *testing.T) {
	o := feed(t, (&Agy{}).NewParser(), "testdata/agy-out.json")
	if o.SessionID == "" || strings.TrimSpace(o.Text) != "OK" || o.IsError || o.Tokens.Input == nil {
		t.Fatalf("%+v", o)
	}
}

func TestClaudeReadShapeIsolatesSettingsAndSoloTools(t *testing.T) {
	c := &Claude{bin: "/bin/claude"}
	command, err := c.Build(Launch{Access: "read", Root: "/project", RunFolder: "/project/report", InSolo: true})
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{"mcp__solo__whoami": false, "mcp__solo__get_process_status": false, "mcp__solo__get_process_output": false, "mcp__solo__todo_comment_create": false}
	isolated := false
	for i, arg := range command.Cmd.Args {
		if arg == "--setting-sources" && i+1 < len(command.Cmd.Args) && command.Cmd.Args[i+1] == "" {
			isolated = true
		}
		if strings.HasPrefix(arg, "mcp__solo__") {
			if _, ok := required[arg]; !ok {
				t.Fatalf("unexpected Solo tool: %s", arg)
			}
			required[arg] = true
		}
	}
	if !isolated || c.Enforcement("read") != Enforced {
		t.Fatal("read shape not isolated")
	}
	for name, found := range required {
		if !found {
			t.Fatalf("missing %s", name)
		}
	}
	if (&Agy{}).Enforcement("read") != BestEffort {
		t.Fatal("agy read shape claims enforcement")
	}
}
