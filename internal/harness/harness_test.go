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
}

func TestGrokReadAllowsOnlyReadTools(t *testing.T) {
	g := &Grok{bin: "/bin/grok"}
	for i, build := range []func(Launch) (*Command, error){g.Build, g.BuildTab} {
		command, err := build(Launch{Access: "read", Root: ".", RunFolder: "/project/run", AllowRules: []string{"Bash"}, AllowedCommands: []string{"touch file"}, AllowCommit: true})
		if err != nil {
			t.Fatal(err)
		}
		allow, deny := map[string]bool{}, map[string]bool{}
		for i, arg := range command.Flags {
			if arg == "--allow" {
				allow[command.Flags[i+1]] = true
			}
			if arg == "--deny" {
				deny[command.Flags[i+1]] = true
			}
		}
		// Grok lets a deny rule win over every allow rule, so a deny for Write
		// would cancel the Run folder rule and the Lane could never write its
		// Report; the Maestro hook holds the Access instead.
		if len(allow) != len(readTools)+2 || len(deny) != 0 || command.ReportByText != (i == 0) {
			t.Fatal(command)
		}
		if flags := strings.Join(command.Flags, " "); !strings.Contains(flags, "--permission-mode dontAsk") {
			t.Fatal("read must run under dontAsk", command.Flags)
		}
		for _, rule := range readTools {
			if !allow[rule] {
				t.Fatal("missing", rule)
			}
		}
		if !allow["Write(///project/run/**)"] || !allow["Edit(///project/run/**)"] {
			t.Fatal("Report writes must stay in the Run folder", allow)
		}
		if g.Enforcement("read") != Enforced {
			t.Fatal("read must be enforced")
		}
	}
}

func TestAgyPrintUsesSandbox(t *testing.T) {
	command, err := (&Agy{bin: "/bin/agy"}).Build(Launch{Access: "read", Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(command.Flags, " "), "--sandbox") || !strings.Contains(strings.Join(command.Cmd.Args, " "), "--sandbox") {
		t.Fatal(command)
	}
}

func TestEnvAlwaysStripsJevKey(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "private-key")
	t.Setenv("OPENAI_API_KEY", "vendor-key")
	for _, set := range []map[string]string{nil, {"TYPESAFE_API_KEY": "replacement"}} {
		env := Env(nil, set)
		vendor := false
		for _, entry := range env {
			if strings.HasPrefix(entry, "TYPESAFE_API_KEY=") {
				t.Fatal("Jev key reached Lane")
			}
			if entry == "OPENAI_API_KEY=vendor-key" {
				vendor = true
			}
		}
		if !vendor {
			t.Fatal("vendor key removed from metered Lane")
		}
	}
}
