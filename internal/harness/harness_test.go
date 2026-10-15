package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
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

func TestGrokReadAllowsOnlyReadTools(t *testing.T) {
	g := &Grok{bin: "/bin/grok"}
	for i, build := range []func(Launch) (*Command, error){g.Build, g.BuildTab} {
		// Dirs are extra writable directories of an edit Lane; a read Lane
		// writes only its Run folder, so they do not decide its sandbox.
		command, err := build(Launch{Access: "read", Root: "/project", RunFolder: "/project/run", Dirs: []string{"/elsewhere"}, AllowRules: []string{"Bash"}, AllowedCommands: []string{"touch file"}, AllowCommit: true})
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
		// Grok takes the absolute path as written; `//` would be literal glob text.
		if !allow["Write(/project/run/**)"] || !allow["Edit(/project/run/**)"] {
			t.Fatal("Report writes must stay in the Run folder", allow)
		}
		if flags := strings.Join(command.Flags, " "); !strings.Contains(flags, "--sandbox workspace") {
			t.Fatal("read must run in the workspace sandbox", command.Flags)
		}
		if g.Enforcement("read") != Enforced || command.Enforcement != Enforced {
			t.Fatal("read must be enforced")
		}
	}
}

func TestGrokEditGrantsWritesAndSandbox(t *testing.T) {
	g := &Grok{bin: "/bin/grok"}
	command, err := g.Build(Launch{Access: "edit", Root: "/project", RunFolder: "/project/.agents/maestro/runs/r-1/t1", Dirs: []string{"/project/vendor"}, AllowedCommands: []string{"go test ./..."}, AllowCommit: true})
	if err != nil {
		t.Fatal(err)
	}
	flags := strings.Join(command.Flags, " ")
	if command.Enforcement != Enforced {
		t.Fatal(command.Enforcement)
	}
	for _, want := range []string{
		"--sandbox workspace", "--permission-mode dontAsk",
		"--allow Write(/project/**) --allow Edit(/project/**)",
		"--allow Write(/project/.agents/maestro/runs/r-1/t1/**)",
		"--allow Write(/project/vendor/**)",
		"--allow Bash(go test ./... *)", "--allow Bash(git commit *)",
	} {
		if !strings.Contains(flags, want) {
			t.Fatalf("missing %q in %s", want, flags)
		}
	}
	if strings.Contains(flags, "--deny") || strings.Contains(flags, "//") {
		t.Fatal(flags)
	}
	// An extra directory outside the project does not fit the workspace sandbox.
	command, err = g.Build(Launch{Access: "edit", Root: "/project", RunFolder: "/project/run", Dirs: []string{"/elsewhere"}})
	if err != nil {
		t.Fatal(err)
	}
	if flags := strings.Join(command.Flags, " "); strings.Contains(flags, "--sandbox") || !strings.Contains(flags, "--allow Write(/elsewhere/**)") {
		t.Fatal(flags)
	}
	if command.Enforcement != BestEffort {
		t.Fatal(command.Enforcement)
	}
}

func TestClaudeSandboxForEachShape(t *testing.T) {
	c := &Claude{bin: "/bin/claude"}
	for _, access := range []string{"read", "edit"} {
		for _, auto := range []bool{false, true} {
			for _, tab := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/auto=%t/tab=%t", access, auto, tab), func(t *testing.T) {
					launch := Launch{Access: access, Root: "/project", RunFolder: "/project/run", Dirs: []string{"/cache", "/project/vendor"}, ClaudeAuto: auto}
					build := c.Build
					if tab {
						build = c.BuildTab
					}
					command, err := build(launch)
					if err != nil {
						t.Fatal(err)
					}
					var settings struct {
						Sandbox map[string]any `json:"sandbox"`
						Env     map[string]string
						Hooks   map[string][]struct {
							Matcher string
							Hooks   []struct {
								Type, Command string
								Timeout       int
							}
						}
					}
					var denied []string
					for i, arg := range command.Cmd.Args {
						if arg == "--settings" {
							if err := json.Unmarshal([]byte(command.Cmd.Args[i+1]), &settings); err != nil {
								t.Fatal(err)
							}
						}
						if arg == "--disallowedTools" {
							denied = command.Cmd.Args[i+1 : i+3]
						}
					}
					filesystem := map[string]any{}
					wantDir, wantEnforcement := launch.RunFolder, Enforced
					if access == "edit" {
						filesystem["allowWrite"] = []any{launch.RunFolder, "/cache", "/project/vendor"}
						filesystem["denyWrite"] = []any{"/project/.git/config", "/project/.git/hooks"}
						wantDir = launch.Root
						if auto {
							wantEnforcement = BestEffort
						}
					}
					want := map[string]any{
						"enabled": true, "autoAllowBashIfSandboxed": false,
						"allowUnsandboxedCommands": false, "failIfUnavailable": true,
						"filesystem": filesystem, "network": map[string]any{"allowLocalBinding": true},
					}
					if !reflect.DeepEqual(settings.Sandbox, want) {
						t.Fatalf("sandbox: %+v, want %+v", settings.Sandbox, want)
					}
					if settings.Env["MAESTRO_TASK_FOLDER"] != launch.RunFolder {
						t.Fatalf("Lane environment: %+v", settings.Env)
					}
					hooks := settings.Hooks["PreToolUse"]
					if len(hooks) != 1 || hooks[0].Matcher != "Agent|Task|Bash|Write|Edit|MultiEdit|NotebookEdit|mcp__solo__spawn_agent|mcp__solo__spawn_process" || len(hooks[0].Hooks) != 1 {
						t.Fatalf("Lane hook: %+v", hooks)
					}
					if h := hooks[0].Hooks[0]; h.Type != "command" || !strings.HasSuffix(h.Command, " hook claude") || h.Timeout != 660 {
						t.Fatalf("Lane hook command: %+v", h)
					}
					relay := settings.Hooks["PermissionRequest"]
					if len(relay) != 1 || len(relay[0].Hooks) != 1 || !strings.Contains(relay[0].Hooks[0].Command, " relay ") || relay[0].Hooks[0].Timeout != 660 {
						t.Fatalf("PermissionRequest Relay: %+v", relay)
					}
					if !reflect.DeepEqual(denied, []string{"Edit(///project/.git/**)", "Write(///project/.git/**)"}) {
						t.Fatalf("git file tools: %v", denied)
					}
					if command.Cmd.Dir != wantDir || command.Enforcement != wantEnforcement {
						t.Fatalf("directory=%s Enforcement=%s", command.Cmd.Dir, command.Enforcement)
					}
				})
			}
		}
	}
}

func TestCodexPinsWritableRoots(t *testing.T) {
	c := &Codex{bin: "/bin/codex"}
	for _, access := range []string{"read", "edit"} {
		for _, tab := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/tab=%t", access, tab), func(t *testing.T) {
				launch := Launch{Access: access, Root: "/project", RunFolder: "/project/run", Dirs: []string{"/cache dir", "/quoted\"dir", "/back\\slash"}}
				build := c.Build
				if tab {
					build = c.BuildTab
				}
				command, err := build(launch)
				if err != nil {
					t.Fatal(err)
				}
				var roots []string
				for i, arg := range command.Cmd.Args {
					if arg == "-c" && strings.HasPrefix(command.Cmd.Args[i+1], "sandbox_workspace_write.writable_roots=") {
						var settings struct {
							Sandbox struct {
								Roots []string `toml:"writable_roots"`
							} `toml:"sandbox_workspace_write"`
						}
						if _, err := toml.Decode(command.Cmd.Args[i+1], &settings); err != nil {
							t.Fatal(err)
						}
						roots = settings.Sandbox.Roots
					}
				}
				want := []string{}
				if access == "edit" {
					want = append([]string{launch.RunFolder}, launch.Dirs...)
				}
				if !reflect.DeepEqual(roots, want) || command.Enforcement != Enforced {
					t.Fatalf("roots=%q Enforcement=%s", roots, command.Enforcement)
				}
			})
		}
	}
}

func TestCommandEnforcementForEveryHarnessAndLaunchForm(t *testing.T) {
	reg := New(map[string]string{"claude": "/bin/claude", "codex": "/bin/codex", "grok": "/bin/grok", "agy": "/bin/agy"})
	for _, name := range reg.Names() {
		h, _ := reg.Get(name)
		for _, access := range []string{"read", "edit"} {
			for _, tab := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/tab=%t", name, access, tab), func(t *testing.T) {
					root := t.TempDir()
					build := h.Build
					if tab {
						build = h.BuildTab
					}
					command, err := build(Launch{Access: access, Root: root, RunFolder: root, Dirs: []string{filepath.Join(root, "cache")}})
					if err != nil {
						t.Fatal(err)
					}
					if command.Enforcement != Enforced {
						t.Fatal(command.Enforcement)
					}
				})
			}
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
