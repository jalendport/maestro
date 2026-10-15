package harness

import (
	"encoding/json"
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

// Every Harness holds both Access values with no help from the model, so a
// named Lane on any of them takes read and edit work alike.
func TestEveryHarnessEnforcesBothShapes(t *testing.T) {
	reg := New(map[string]string{"claude": "/bin/claude", "codex": "/bin/codex", "grok": "/bin/grok", "agy": "/bin/agy"})
	for _, name := range []string{"claude", "codex", "grok", "agy"} {
		h, ok := reg.Get(name)
		if !ok {
			t.Fatal(name)
		}
		for _, access := range []string{"read", "edit"} {
			if got := h.Enforcement(access); got != Enforced {
				t.Fatalf("%s %s: %q, want %q", name, access, got, Enforced)
			}
		}
		if got := h.Enforcement("other"); got != NoShape {
			t.Fatalf("%s other: %q", name, got)
		}
	}
}

func TestGrokShapesRunAlwaysApproveUnderTheHook(t *testing.T) {
	g := &Grok{bin: "/bin/grok"}
	for i, build := range []func(Launch) (*Command, error){g.Build, g.BuildTab} {
		// Grok has no prompt-free deny-by-default mode, so the Maestro hook is
		// the policy and the mode is always-approve; no allow or deny rule is passed.
		command, err := build(Launch{Access: "read", Root: "/project", RunFolder: "/project/run", Dirs: []string{"/elsewhere"}})
		if err != nil {
			t.Fatal(err)
		}
		flags := strings.Join(command.Flags, " ")
		if flags != "--sandbox workspace --permission-mode bypassPermissions" || command.ReportByText != (i == 0) {
			t.Fatal(command.Flags, command.ReportByText)
		}
		if g.Enforcement("read") != Enforced || command.Enforcement != Enforced {
			t.Fatal("read must be enforced")
		}
	}
	command, err := g.Build(Launch{Access: "edit", Root: "/project", RunFolder: "/project/.agents/maestro/runs/r-1/t1", Dirs: []string{"/project/vendor"}, AllowedCommands: []string{"go test ./..."}, AllowCommit: true})
	if err != nil {
		t.Fatal(err)
	}
	if flags := strings.Join(command.Flags, " "); flags != "--sandbox workspace --permission-mode bypassPermissions" || command.Enforcement != Enforced {
		t.Fatal(command.Flags, command.Enforcement)
	}
	// An extra directory outside the project does not fit the workspace sandbox.
	command, err = g.Build(Launch{Access: "edit", Root: "/project", RunFolder: "/project/run", Dirs: []string{"/elsewhere"}})
	if err != nil {
		t.Fatal(err)
	}
	if flags := strings.Join(command.Flags, " "); flags != "--permission-mode bypassPermissions" || command.Enforcement != BestEffort {
		t.Fatal(command.Flags, command.Enforcement)
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

// A host outside the allowlist is denied without a prompt in every Claude Lane,
// so a tab never waits on the sandbox's network dialog.
func TestClaudeShapesDenyNetworkWithoutAPrompt(t *testing.T) {
	c := &Claude{bin: "/bin/claude"}
	for _, access := range []string{"read", "edit"} {
		command, err := c.Build(Launch{Access: access, Root: "/project", RunFolder: "/project/report"})
		if err != nil {
			t.Fatal(err)
		}
		var settings struct {
			Sandbox struct {
				Network map[string]bool `json:"network"`
			} `json:"sandbox"`
		}
		found := false
		for i, arg := range command.Cmd.Args {
			if arg == "--settings" && i+1 < len(command.Cmd.Args) {
				if err := json.Unmarshal([]byte(command.Cmd.Args[i+1]), &settings); err != nil {
					t.Fatal(err)
				}
				found = true
			}
		}
		if !found || !settings.Sandbox.Network["strictAllowlist"] || !settings.Sandbox.Network["allowLocalBinding"] {
			t.Fatalf("%s network settings = %+v", access, settings.Sandbox.Network)
		}
	}
}
