package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	o := feed(t, (&Grok{}).NewParser(), "testdata/grok-stream.jsonl")
	if o.SessionID != "01a1aa79-20f6-7320-a783-63b7b414f078" || o.Text != "2" || o.ModelRan != "grok-4.7-build" || o.IsError || o.ErrorText != "" || len(o.Signals) != 0 || len(o.Denials) != 0 {
		t.Fatalf("%+v", o)
	}
	if o.CostUSD == nil || *o.CostUSD != 0.01065084 {
		t.Fatalf("cost %v", o.CostUSD)
	}
	wantTokens := Tokens{Input: intp(8657), CachedInput: intp(27136), Output: intp(74), Reasoning: intp(42)}
	if !reflect.DeepEqual(o.Tokens, wantTokens) {
		t.Fatalf("tokens %+v, want %+v", o.Tokens, wantTokens)
	}
}

func TestGrokTextChunksJoinInOrder(t *testing.T) {
	p := (&Grok{}).NewParser()
	for _, line := range []string{
		`{"type":"text","data":"First "}`,
		`{"type":"tool_call","toolName":"read_file"}`,
		`{"type":"text","data":"second\n"}`,
		`{"type":"text","data":"third"}`,
		`{"type":"end","sessionId":"session"}`,
	} {
		if !p.Line([]byte(line)) {
			t.Fatalf("line must count as progress: %s", line)
		}
	}
	if o := p.Outcome(); o.Text != "First \n\nsecond\nthird" || o.IsError {
		t.Fatalf("%+v", o)
	}
}

func TestGrokConsecutiveTextChunksJoinWithoutSeparator(t *testing.T) {
	p := (&Grok{}).NewParser()
	p.Line([]byte(`{"type":"text","data":"## Ans"}`))
	p.Line([]byte(`{"type":"text","data":"wer\nSTATUS: done"}`))
	if o := p.Outcome(); o.Text != "## Answer\nSTATUS: done" || o.IsError {
		t.Fatalf("%+v", o)
	}
}

func TestGrokLateLinesKeepMetadata(t *testing.T) {
	for _, late := range []string{
		`{"type":"error","message":"usage limit reached"}`,
		`{"type":"end","error":"usage limit reached"}`,
		`{"type":"error","message":"usage limit reached","usage":{"output_tokens":0},"total_cost_usd":0}`,
	} {
		t.Run(late, func(t *testing.T) {
			p := (&Grok{}).NewParser()
			p.Line([]byte(`{"type":"end","sessionId":"session","total_cost_usd":0.25,"usage":{"input_tokens":10,"cache_read_input_tokens":20,"output_tokens":30,"reasoning_tokens":40},"modelUsage":{"grok-4.7-build":{}}}`))
			p.Line([]byte(late))
			o := p.Outcome()
			wantCost := 0.25
			wantTokens := Tokens{Input: intp(10), CachedInput: intp(20), Output: intp(30), Reasoning: intp(40)}
			if strings.Contains(late, `"output_tokens":0`) {
				wantCost = 0
				wantTokens.Output = intp(0)
			}
			if o.SessionID != "session" || o.ModelRan != "grok-4.7-build" || o.CostUSD == nil || *o.CostUSD != wantCost || !reflect.DeepEqual(o.Tokens, wantTokens) {
				t.Fatalf("metadata lost: %+v", o)
			}
			if !o.IsError || o.ErrorText != "usage limit reached" || len(o.Signals) != 1 || o.Signals[0].Class != "limit" {
				t.Fatalf("%+v", o)
			}
		})
	}
}

func TestGrokUnknownTypeIsProgressWithoutChangingOutcome(t *testing.T) {
	p := (&Grok{}).NewParser()
	o := feed(t, p, "testdata/grok-stream.jsonl")
	want := *o
	if !p.Line([]byte(`{"type":"future_event","data":{"text":"ignored"},"sessionId":"other","usage":"unknown","error":"quota exceeded"}`)) {
		t.Fatal("unknown type must count as progress")
	}
	if !reflect.DeepEqual(*p.Outcome(), want) {
		t.Fatalf("outcome changed: %+v, want %+v", p.Outcome(), want)
	}
	for _, line := range []string{"", " \t\r"} {
		if p.Line([]byte(line)) {
			t.Fatal("empty line must not count as progress")
		}
	}
	if !reflect.DeepEqual(*p.Outcome(), want) {
		t.Fatal("empty line changed the outcome")
	}
}

func TestGrokStreamErrorsKeepLimitClassification(t *testing.T) {
	for _, line := range []string{
		`{"type":"error","message":"usage limit reached"}`,
		`{"type":"end","error":"usage limit reached"}`,
	} {
		t.Run(line, func(t *testing.T) {
			p := (&Grok{}).NewParser()
			if !p.Line([]byte(line)) {
				t.Fatal("error line must count as progress")
			}
			o := p.Outcome()
			if !o.IsError || o.ErrorText != "usage limit reached" || len(o.Signals) != 1 || o.Signals[0].Class != "limit" {
				t.Fatalf("%+v", o)
			}
		})
	}
}

func TestGrokNonJSONIsAnError(t *testing.T) {
	p := (&Grok{}).NewParser()
	if !p.Line([]byte("quota exceeded")) {
		t.Fatal("non-empty stdout must count as progress")
	}
	p.Line([]byte(`{"type":"end","sessionId":"session"}`))
	o := p.Outcome()
	if !o.IsError || o.ErrorText != "quota exceeded" || len(o.Signals) != 1 || o.Signals[0].Class != "limit" {
		t.Fatalf("%+v", o)
	}
}

func TestGrokThoughtDenial(t *testing.T) {
	for _, thought := range []string{"The command was DENIED.", "I need Permission to run this."} {
		t.Run(thought, func(t *testing.T) {
			p := (&Grok{}).NewParser()
			line, err := json.Marshal(map[string]string{"type": "thought", "data": thought})
			if err != nil {
				t.Fatal(err)
			}
			if !p.Line(line) {
				t.Fatal("thought line must count as progress")
			}
			p.Line([]byte(`{"type":"text","data":"Done"}`))
			p.Line([]byte(`{"type":"end"}`))
			o := p.Outcome()
			if o.IsError || o.Text != "Done" || !reflect.DeepEqual(o.Denials, []Denial{{Tool: "thought", Input: thought}}) {
				t.Fatalf("%+v", o)
			}
			want := *o
			if !reflect.DeepEqual(*p.Outcome(), want) {
				t.Fatal("reading the outcome changed the denial signal")
			}
		})
	}
}

func TestGrokThoughtChunksGiveOneDenial(t *testing.T) {
	for _, chunks := range [][]string{
		{"The command was den", "ied."},
		{"I need per", "mission to run this."},
		{"The command was denied.", " Another command was denied."},
	} {
		t.Run(strings.Join(chunks, ""), func(t *testing.T) {
			p := (&Grok{}).NewParser()
			for _, chunk := range chunks {
				line, err := json.Marshal(map[string]string{"type": "thought", "data": chunk})
				if err != nil {
					t.Fatal(err)
				}
				if !p.Line(line) {
					t.Fatal("thought line must count as progress")
				}
			}
			o := p.Outcome()
			if o.IsError || len(o.Denials) != 1 || o.Denials[0].Tool != "thought" || !strings.Contains(o.Denials[0].Input, "denied") && !strings.Contains(o.Denials[0].Input, "permission") {
				t.Fatalf("%+v", o)
			}
		})
	}
}

func TestGrokOneShotStreamArguments(t *testing.T) {
	g := &Grok{bin: "/bin/grok"}
	for _, access := range []string{"read", "edit"} {
		t.Run(access, func(t *testing.T) {
			launch := Launch{Access: access, Root: "/project", RunFolder: "/project/run", Model: "grok-4.7-build", Effort: "high"}
			command, err := g.Build(launch)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{g.bin, "--prompt-file", "/project/run/pointer.txt", "--output-format", "streaming-json", "--no-subagents", "--model", launch.Model, "--reasoning-effort", launch.Effort, "--sandbox", "workspace", "--permission-mode", "bypassPermissions"}
			if !reflect.DeepEqual(command.Cmd.Args, want) {
				t.Fatalf("one-shot args %q, want %q", command.Cmd.Args, want)
			}
		})
	}
}

func TestAgyParserFixture(t *testing.T) {
	o := feed(t, (&Agy{}).NewParser(), "testdata/agy-out.json")
	if o.SessionID == "" || strings.TrimSpace(o.Text) != "OK" || o.IsError || o.Tokens.Input == nil {
		t.Fatalf("%+v", o)
	}
}

func TestClaudeReadShapeIsolatesSettings(t *testing.T) {
	c := &Claude{bin: "/bin/claude"}
	command, err := c.Build(Launch{Access: "read", Root: "/project", RunFolder: "/project/report"})
	if err != nil {
		t.Fatal(err)
	}
	isolated := false
	for i, arg := range command.Cmd.Args {
		if arg == "--setting-sources" && i+1 < len(command.Cmd.Args) && command.Cmd.Args[i+1] == "" {
			isolated = true
		}
		if strings.HasPrefix(arg, "mcp__solo__") {
			t.Fatalf("unexpected Solo tool: %s", arg)
		}
	}
	if !isolated || c.Enforcement("read") != Enforced {
		t.Fatal("read shape not isolated")
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
	// Grok has no prompt-free deny-by-default mode, so the Maestro hook is
	// the policy and the mode is always-approve; no allow or deny rule is passed.
	command, err := g.Build(Launch{Access: "read", Root: "/project", RunFolder: "/project/run", Dirs: []string{"/elsewhere"}})
	if err != nil {
		t.Fatal(err)
	}
	flags := strings.Join(command.Flags, " ")
	if flags != "--sandbox workspace --permission-mode bypassPermissions" || !command.ReportByText {
		t.Fatal(command.Flags, command.ReportByText)
	}
	trusted := false
	for _, arg := range command.Cmd.Args {
		if arg == "--trust" {
			trusted = true
		}
	}
	if trusted {
		t.Fatalf("--trust must not be passed to a Lane: %q", command.Cmd.Args)
	}
	if g.Enforcement("read") != Enforced || command.Enforcement != Enforced {
		t.Fatal("read must be enforced")
	}
	command, err = g.Build(Launch{Access: "edit", Root: "/project", RunFolder: "/project/.agents/maestro/runs/r-1/t1", Dirs: []string{"/project/vendor"}, AllowedCommands: []string{"go test ./..."}})
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

func TestGrokOutsideRunFolderUsesMessageReportAndKeepsSandbox(t *testing.T) {
	root, folder := t.TempDir(), t.TempDir()
	for _, access := range []string{"read", "edit"} {
		command, err := (&Grok{bin: "/fixture/grok"}).Build(Launch{
			Access: access, Root: root, RunFolder: folder, Dirs: []string{folder, filepath.Join(root, "cache")},
			Pointer: "Read the Brief at /project/run/brief.md.",
		})
		if err != nil {
			t.Fatal(err)
		}
		if flags := strings.Join(command.Flags, " "); !strings.Contains(flags, "--sandbox workspace") || command.Enforcement != Enforced || !command.ReportByText {
			t.Fatalf("%s launch: %+v", access, command)
		}
		if text, err := os.ReadFile(filepath.Join(folder, "pointer.txt")); err != nil || !strings.Contains(string(text), "/project/run/brief.md") {
			t.Fatalf("Pointer: %s, %v", text, err)
		}
	}
	command, err := (&Grok{bin: "/fixture/grok"}).Build(Launch{Access: "edit", Root: root, RunFolder: folder, Dirs: []string{folder, t.TempDir()}})
	if err != nil || command.Enforcement != BestEffort || strings.Contains(strings.Join(command.Flags, " "), "--sandbox") {
		t.Fatalf("outside writable directory: %+v, %v", command, err)
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

// A host outside the allowlist is denied without a prompt in every Claude Lane.
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
