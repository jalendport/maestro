package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/hook"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/roster"
)

func TestDoctorCanaryHarness(t *testing.T) {
	if os.Getenv("MAESTRO_TEST_CANARY_HELPER") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println("codex " + os.Getenv("MAESTRO_TEST_CANARY_VERSION"))
		os.Exit(0)
	}
	if len(args) > 0 && args[0] == "debug" {
		fmt.Println(`{"models":[{"slug":"canary-cheap","supported_reasoning_levels":[{"effort":"low"}]},{"slug":"canary-costly","supported_reasoning_levels":[{"effort":"low"}]}]}`)
		os.Exit(0)
	}
	for _, arg := range args {
		if arg == "--mcp-config" || strings.HasPrefix(arg, "mcp__solo__") {
			t.Fatalf("Canary has a Solo MCP argument: %s", arg)
		}
	}
	folder := os.Getenv("MAESTRO_TASK_FOLDER")
	if err := os.WriteFile(os.Getenv("MAESTRO_TEST_CANARY_STARTED"), []byte(folder), 0o600); err != nil {
		t.Fatal(err)
	}
	task, err := dispatch.LoadTask(folder)
	if err != nil || task.Access != "read" || task.InSolo || !task.Canary || task.MaxAttempt != 1 || task.Route.Model != "canary-cheap" {
		t.Fatalf("Canary Task = %+v, error = %v", task, err)
	}
	idle, _ := time.ParseDuration(task.IdleLimit)
	wall, _ := time.ParseDuration(task.WallLimit)
	if wall <= 0 || (idle > 0 && idle < wall) {
		t.Fatalf("Canary has an Idle limit shorter than its Wall limit: %+v", task)
	}
	mode := os.Getenv("MAESTRO_TEST_CANARY_MODE")
	name := os.Getenv("MAESTRO_TEST_CANARY_HARNESS")
	if mode == "run record" {
		if err := hook.RecordJudgment(filepath.Join(folder, "no-control-folder"), folder, &hook.Call{Harness: name, Tool: "run_terminal_command"}, hook.Verdict{Kind: "auto_approve"}); err != nil {
			t.Fatal(err)
		}
	} else if mode != "hook" && name != "codex" {
		call := map[string]any{
			"tool_name": "exec_command", "tool_input": map[string]any{"command": "cat " + filepath.Join(task.Project, "canary.txt")}, "cwd": task.Project,
		}
		switch name {
		case "claude":
			call["tool_name"] = "Bash"
		case "grok":
			call = map[string]any{"toolName": "run_terminal_command", "toolInput": call["tool_input"], "cwd": task.Project}
		case "agy":
			call = map[string]any{"toolCall": map[string]any{"name": "run_command", "args": map[string]any{"CommandLine": "cat " + filepath.Join(task.Project, "canary.txt"), "Cwd": task.Project}}}
		}
		payload, _ := json.Marshal(call)
		runHookPayloadRaw(t, string(payload), name)
	} else if name == "codex" {
		runHookPayloadRaw(t, `{"hook_event_name":"UserPromptSubmit","prompt":"Return a Report"}`, "codex")
	}
	report := "## Answer\nThe file was read.\n\nSTATUS: done\n"
	if mode == "report" {
		report = "The hook ran and the report is fine."
	}
	if name != "agy" {
		if err := os.WriteFile(filepath.Join(folder, dispatch.FileReport), []byte(report), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "process" {
		os.Exit(7)
	}
	if mode == "stream error" {
		fmt.Println(`{"type":"turn.failed","error":{"message":"Harness failed after the Report"}}`)
	} else if name == "agy" {
		b, err := json.Marshal(map[string]string{"status": "SUCCESS", "response": report})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println(string(b))
	}
	os.Exit(0)
}

func setupDoctorCanary(t *testing.T) string {
	t.Helper()
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	t.Setenv("PATH", "")
	t.Setenv("SOLO_PROCESS_ID", "")
	t.Setenv("MAESTRO_TEST_CANARY_HELPER", "1")
	t.Setenv("MAESTRO_TEST_CANARY_HARNESS", "codex")
	t.Setenv("MAESTRO_TEST_CANARY_VERSION", "9.0.0")
	t.Setenv("MAESTRO_TEST_CANARY_MODE", "pass")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_TEST_CANARY_EXE", self)
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("MAESTRO_TEST_CANARY_STARTED", started)
	bin := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\nexec \"$MAESTRO_TEST_CANARY_EXE\" -test.run=^TestDoctorCanaryHarness$ -- \"$@\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf(`[harness.codex]
bin = %q
[families.canary-cheap]
harness = "codex"
models = ["canary-cheap"]
levels = { none = "low" }
[families.canary-costly]
harness = "codex"
models = ["canary-costly"]
levels = { none = "low" }
[families.canary-disabled]
harness = "codex"
models = ["canary-costly"]
levels = { none = "low" }
enabled = false
[prices.canary-cheap]
input = 1
output = 2
[prices.canary-costly]
input = 10
output = 20
`, bin)
	if err := os.WriteFile(config.UserFile(), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return started
}

func doctorOutput(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := (&app{}).build()
	root.SetArgs(append([]string{"doctor"}, args...))
	var commandErr error
	out := captureStdout(t, func() error { commandErr = root.Execute(); return nil })
	return out, commandErr
}

func TestDoctorCanaryPartsAndScratchCleanup(t *testing.T) {
	for _, mode := range []string{"pass", "process", "hook", "report", "stream error"} {
		t.Run(mode, func(t *testing.T) {
			started := setupDoctorCanary(t)
			t.Setenv("MAESTRO_TEST_CANARY_MODE", mode)
			out, commandErr := doctorOutput(t, "--canary", "codex")
			result, err := dispatch.LoadCanary("codex")
			if err != nil || result == nil {
				t.Fatalf("stored Canary = %+v, error = %v; output = %s", result, err, out)
			}
			if result.Version != "9.0.0" || result.CheckedAt.IsZero() || result.Family != "canary-cheap" {
				t.Fatalf("stored Canary = %+v", result)
			}
			if result.Process.Passed != (mode != "process" && mode != "stream error") || result.Hook.Passed || !result.Hook.NotApplicable || result.Report.Passed != (mode != "report") {
				t.Fatalf("Canary parts = %+v", result)
			}
			want := "canary passed for 9.0.0"
			if mode != "pass" && mode != "hook" {
				part := mode
				if mode == "stream error" {
					part = "process"
				}
				want = "canary failed for 9.0.0 (" + part + ":"
				if code, ok := proc.Code(commandErr); !ok || code != proc.CodeFailed {
					t.Fatalf("failed Canary error = %v", commandErr)
				}
			}
			if !strings.Contains(out, want) {
				t.Fatalf("doctor output = %s, want %s", out, want)
			}
			out, _ = doctorOutput(t)
			if !strings.Contains(out, want) {
				t.Fatalf("stored doctor output = %s, want %s", out, want)
			}
			b, err := os.ReadFile(started)
			if err != nil {
				t.Fatal(err)
			}
			folder := string(b)
			scratch := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(folder)))))
			if !strings.HasPrefix(filepath.Base(scratch), "maestro-canary-") {
				t.Fatalf("unexpected scratch path: %s", scratch)
			}
			if result.Passed() {
				if _, err := os.Stat(scratch); !os.IsNotExist(err) || result.ScratchFolder != "" {
					t.Fatalf("scratch folder remains: %s, %v", scratch, err)
				}
			} else {
				if result.ScratchFolder != scratch || !strings.Contains(out, scratch) {
					t.Fatalf("doctor did not name the scratch folder: %+v, %s", result, out)
				}
				for _, name := range []string{dispatch.FileBrief, dispatch.FileReport, dispatch.FileOutput, dispatch.FileStderr, dispatch.FileRecord} {
					if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
						t.Fatalf("failed Canary lost %s: %v", name, err)
					}
				}
			}
			control, err := config.ControlDirForFolder(folder)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(control); result.Passed() && !os.IsNotExist(err) || !result.Passed() && err != nil {
				t.Fatalf("control folder: %s, %v", control, err)
			}
			rows := 0
			err = ledger.Each(func(raw json.RawMessage) error {
				var rec dispatch.Record
				if err := json.Unmarshal(raw, &rec); err != nil {
					return err
				}
				rows++
				if rec.Kind != "dispatch" || !rec.Canary || rec.TaskInfo.Access != "read" || rec.Attempt != 1 || rec.Path != folder {
					t.Fatalf("Canary Ledger row = %+v", rec)
				}
				return nil
			})
			if err != nil || rows != 1 {
				t.Fatalf("Ledger rows = %d, error = %v", rows, err)
			}
		})
	}
}

func TestDoctorDoesNotDispatchAndRejectsOldCanaryVersion(t *testing.T) {
	started := setupDoctorCanary(t)
	out, _ := doctorOutput(t)
	if !strings.Contains(out, "no canary result") || !strings.Contains(out, "maestro doctor --canary codex") {
		t.Fatalf("doctor output = %s", out)
	}
	if _, err := os.Stat(started); !os.IsNotExist(err) {
		t.Fatalf("doctor started a Dispatch: %v", err)
	}
	if _, err := os.Stat(config.LedgerFile()); !os.IsNotExist(err) {
		t.Fatalf("doctor wrote a Ledger row: %v", err)
	}
	doctorOutput(t, "--canary")
	t.Setenv("MAESTRO_TEST_CANARY_VERSION", "9.0.1")
	out, _ = doctorOutput(t)
	if !strings.Contains(out, "no canary result") || strings.Contains(out, "canary passed") {
		t.Fatalf("old version passed for installed version: %s", out)
	}
}

func TestDoctorCanaryRemovesEarlierFailureBeforeNextDispatch(t *testing.T) {
	started := setupDoctorCanary(t)
	t.Setenv("MAESTRO_TEST_CANARY_MODE", "report")
	doctorOutput(t, "--canary", "codex")
	first, err := dispatch.LoadCanary("codex")
	if err != nil || first == nil || first.ScratchFolder == "" {
		t.Fatalf("failed Canary = %+v, error = %v", first, err)
	}
	b, err := os.ReadFile(started)
	if err != nil {
		t.Fatal(err)
	}
	control := controlFolder(t, string(b))
	markers := []string{filepath.Join(first.ScratchFolder, "old-evidence"), filepath.Join(control, "old-evidence")}
	for _, marker := range markers {
		if err := os.WriteFile(marker, []byte("earlier failure"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MAESTRO_TEST_CANARY_MODE", "process")
	doctorOutput(t, "--canary", "codex")
	for _, marker := range markers {
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("earlier evidence remains: %s, %v", marker, err)
		}
	}
	t.Setenv("MAESTRO_TEST_CANARY_MODE", "pass")
	out, _ := doctorOutput(t, "--canary", "codex")
	if !strings.Contains(out, "canary passed") {
		t.Fatalf("Canary did not pass: %s", out)
	}
	for _, folder := range []string{first.ScratchFolder, control} {
		if _, err := os.Stat(folder); !os.IsNotExist(err) {
			t.Fatalf("scratch files remain after a pass: %s, %v", folder, err)
		}
	}
}

func TestDoctorCanaryHelpAndArguments(t *testing.T) {
	setupDoctorCanary(t)
	out, err := doctorOutput(t, "--help")
	if err != nil || !strings.Contains(out, "--canary") || !strings.Contains(out, "Account") {
		t.Fatalf("doctor help = %s, error = %v", out, err)
	}
	for _, args := range [][]string{{"codex"}, {"--canary", "unknown"}, {"--canary", "claude"}} {
		if _, err := doctorOutput(t, args...); err == nil {
			t.Fatalf("doctor accepted %v", args)
		}
	}
}

func TestDoctorSoloRowDescribesView(t *testing.T) {
	for _, process := range []string{"", "42"} {
		t.Setenv("SOLO_PROCESS_ID", process)
		row := soloRow(&dispatch.Env{})
		if !strings.Contains(row.State, "View") || strings.Contains(row.State, "one-shot lanes only") {
			t.Fatalf("Solo row = %+v", row)
		}
	}
}

func TestCanaryWithEachHarness(t *testing.T) {
	for _, name := range []string{"claude", "codex", "grok", "agy"} {
		for _, mode := range []string{"pass", "hook", "run record"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				setupDoctorCanary(t)
				t.Setenv("MAESTRO_TEST_CANARY_HARNESS", name)
				t.Setenv("MAESTRO_TEST_CANARY_MODE", mode)
				r, err := roster.Load()
				if err != nil {
					t.Fatal(err)
				}
				bin := r.Harness["codex"].Bin
				r.Families = map[string]*roster.Family{"canary-cheap": {
					ID: "canary-cheap", Harness: name, Models: []string{"canary-cheap"}, Enabled: true,
					Levels: map[roster.Level]string{roster.LevelNone: "low"},
				}}
				env := &dispatch.Env{
					Roster: r, Reg: harness.New(map[string]string{name: bin}),
					Solo: &dispatch.SoloLink{ProjectID: 1},
					Disc: &discovery.Cache{Harness: map[string]discovery.Record{name: {
						Path: bin, Version: "9.0.0", Models: []harness.Model{{ID: "canary-cheap"}},
					}}},
				}
				result, err := dispatch.RunCanary(env, name)
				wantPass := mode != "hook" || name == "codex"
				if err != nil || result.Passed() != wantPass {
					t.Fatalf("Canary for %s = %+v, error = %v", name, result, err)
				}
				stored, err := dispatch.LoadCanary(name)
				if err != nil || stored.Hook.NotApplicable != (name == "codex") || (mode == "hook" && name != "codex" && !strings.Contains(stored.FailedParts(), "hook:")) {
					t.Fatalf("stored hook part = %+v, error = %v", stored, err)
				}
				if name != "codex" && mode != "hook" {
					place := "control folder"
					if mode == "run record" {
						place = "Run folder; the Lane can write this record"
					}
					if !strings.Contains(stored.Hook.Detail, place) {
						t.Fatalf("hook record place = %s", stored.Hook.Detail)
					}
				}
			})
		}
	}
}
