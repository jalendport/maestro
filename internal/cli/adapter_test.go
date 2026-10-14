package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/proc"
)

func TestAdapterPrintsExactHookSet(t *testing.T) {
	for _, tc := range []struct{ harness, want string }{
		{"claude", `{"hooks":{"PreToolUse":[{"hooks":[{"command":"maestro hook","timeout":10,"type":"command"}],"matcher":"Agent|Task|mcp__solo__spawn_agent|mcp__solo__spawn_process"}],"UserPromptSubmit":[{"hooks":[{"command":"maestro hook","timeout":10,"type":"command"}]}]}}`},
		{"codex", `{"hooks":{"PreToolUse":[{"hooks":[{"command":"maestro hook","timeout":10,"type":"command"}],"matcher":"Agent|mcp__solo__spawn_agent|mcp__solo__spawn_process"}],"UserPromptSubmit":[{"hooks":[{"command":"maestro hook","timeout":10,"type":"command"}]}]}}`},
		{"grok", `{"hooks":{"PreToolUse":[{"hooks":[{"command":"maestro hook","timeout":10,"type":"command"}],"matcher":"spawn_subagent|Task|solo__spawn_agent|solo__spawn_process"}]}}`},
		{"agy", `{"maestro":{"PreInvocation":[{"command":"maestro hook agy","timeout":30,"type":"command"}],"PreToolUse":[{"hooks":[{"command":"maestro hook","timeout":660,"type":"command"}],"matcher":"run_command|write_to_file|replace_file_content|multi_replace_file_content|invoke_subagent|define_subagent|manage_subagents"}]}}`},
	} {
		t.Run(tc.harness, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			t.Setenv("CODEX_HOME", "")
			command := (&app{}).adapterCmd()
			command.SetArgs([]string{tc.harness})
			output := captureStdout(t, command.Execute)
			spec, _ := adapter.Paths(tc.harness, home)
			want := "Hook scope: projects (only inside a project with .agents/maestro.toml).\nmaestro adapter prints the steps and writes nothing; maestro adapter install [harness] does them.\n"
			want += fmt.Sprintf("\n%s:\n  mkdir -p '%s'\n  ln -s '%s' '%s'\n  %s\n  %s\n", tc.harness, filepath.Dir(spec.Link), adapter.SkillDir(), spec.Link, spec.HookFile, tc.want)
			if tc.harness == "grok" {
				want += "  " + adapter.ReminderFile(home) + "\n" + adapter.ReminderRule()
			}
			want += "  Next: " + spec.Next + "\n"
			if output != want {
				t.Fatalf("output = %q, want %q", output, want)
			}
			if result := adapter.Install(tc.harness, home, "skill"); result.Err != nil {
				t.Fatal(result.Err)
			}
			data, err := os.ReadFile(spec.HookFile)
			if err != nil {
				t.Fatal(err)
			}
			var installed any
			if err := json.Unmarshal(data, &installed); err != nil {
				t.Fatal(err)
			}
			data, err = json.Marshal(installed)
			if err != nil || string(data) != tc.want {
				t.Fatalf("installed hook set = %s: %v", data, err)
			}
		})
	}
}

func TestAdapterInstallPrintsParseErrorOnce(t *testing.T) {
	for _, name := range adapter.Harnesses {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			t.Setenv("CODEX_HOME", "")
			spec, _ := adapter.Paths(name, home)
			if err := os.MkdirAll(filepath.Dir(spec.HookFile), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(spec.HookFile, []byte(`{"hooks":`), 0o644); err != nil {
				t.Fatal(err)
			}
			root := (&app{}).build()
			root.SetArgs([]string{"adapter", "install", name})
			stderr, err := os.CreateTemp(t.TempDir(), "install-stderr")
			if err != nil {
				t.Fatal(err)
			}
			defer stderr.Close()
			previous := os.Stderr
			os.Stderr = stderr
			defer func() { os.Stderr = previous }()
			var installErr error
			captureStdout(t, func() error { installErr = root.Execute(); return nil })
			if _, err := stderr.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			output, err := io.ReadAll(stderr)
			if err != nil {
				t.Fatal(err)
			}
			if code, ok := proc.Code(installErr); !ok || code != 1 || strings.Count(string(output), "parse JSON") != 1 {
				t.Fatalf("install = %v, output = %q", installErr, output)
			}
		})
	}
}

func TestAdapterCommandHelp(t *testing.T) {
	for _, args := range [][]string{{"hook", "--help"}, {"adapter", "--help"}, {"adapter", "install", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := (&app{}).build()
			root.SetArgs(args)
			output := captureStdout(t, root.Execute)
			for _, want := range []string{"PreToolUse", "Reminder", "Carrier", "UserPromptSubmit", "PreInvocation"} {
				if !strings.Contains(output, want) {
					t.Fatalf("help is missing %q: %s", want, output)
				}
			}
		})
	}
}

func TestAdapterGrokPrintsConfigPath(t *testing.T) {
	for _, maestroHome := range []string{"", filepath.Join(t.TempDir(), "home with spaces")} {
		t.Run(maestroHome, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("MAESTRO_HOME", maestroHome)
			command := (&app{}).adapterCmd()
			command.SetArgs([]string{"grok"})
			output := captureStdout(t, command.Execute)
			want := "~/.maestro/config.toml"
			if maestroHome != "" {
				want = filepath.Join(maestroHome, "config.toml")
			}
			if !strings.Contains(output, "`"+want+"`") {
				t.Fatal(output)
			}
		})
	}
}

func TestAdapterPrintsCarrier(t *testing.T) {
	for _, tc := range []struct{ harness, key, event, want string }{
		{"claude", "hooks", "UserPromptSubmit", `[{"hooks":[{"type":"command","command":"maestro hook","timeout":10}]}]`},
		{"codex", "hooks", "UserPromptSubmit", `[{"hooks":[{"type":"command","command":"maestro hook","timeout":10}]}]`},
		{"agy", "maestro", "PreInvocation", `[{"type":"command","command":"maestro hook agy","timeout":30}]`},
		{"grok", "", "", ""},
	} {
		t.Run(tc.harness, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			t.Setenv("CODEX_HOME", "")
			command := (&app{}).adapterCmd()
			command.SetArgs([]string{tc.harness})
			output := captureStdout(t, command.Execute)
			spec, _ := adapter.Paths(tc.harness, home)
			if !strings.Contains(output, spec.HookFile) {
				t.Fatal(output)
			}
			if tc.harness == "grok" {
				if !strings.Contains(output, adapter.ReminderFile(home)+"\n"+adapter.ReminderRule()) {
					t.Fatal(output)
				}
			} else {
				found := false
				for _, line := range strings.Split(output, "\n") {
					if !strings.HasPrefix(strings.TrimSpace(line), "{") {
						continue
					}
					var doc map[string]map[string]json.RawMessage
					if err := json.Unmarshal([]byte(line), &doc); err != nil {
						t.Fatal(err)
					}
					var got, want any
					if err := json.Unmarshal(doc[tc.key][tc.event], &got); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("Carrier = %s, want %s", doc[tc.key][tc.event], tc.want)
					}
					found = true
				}
				if !found {
					t.Fatal("Carrier missing:", output)
				}
			}
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 {
				t.Fatalf("adapter wrote to home: %v, %v", entries, err)
			}
		})
	}
}

func TestDoctorRequiresReminderForInstalledHarness(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("SOLO_PROCESS_ID", "")
	t.Setenv("PATH", "")
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'Claude Code 9.0.0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserFile(), []byte("[harness.claude]\nbin = \""+bin+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := adapter.Install("claude", home, "skill"); result.Err != nil {
		t.Fatal(result.Err)
	}
	spec, _ := adapter.Paths("claude", home)
	data, err := os.ReadFile(spec.HookFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	delete(doc["hooks"], "UserPromptSubmit")
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.HookFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	command := (&app{}).doctorCmd()
	var doctorErr error
	output := captureStdout(t, func() error { doctorErr = command.Execute(); return nil })
	if code, ok := proc.Code(doctorErr); !ok || code != 1 || !strings.Contains(output, "skill ok, link ok, hook ok, reminder missing") {
		t.Fatalf("doctor = %v, %q", doctorErr, output)
	}
	if result := adapter.Install("claude", home, "skill"); result.Err != nil {
		t.Fatal(result.Err)
	}
	command = (&app{}).doctorCmd()
	output = captureStdout(t, command.Execute)
	if !strings.Contains(output, "skill ok, link ok, hook ok, reminder ok") {
		t.Fatal(output)
	}
}

func TestReminderRootHelp(t *testing.T) {
	root := (&app{}).build()
	root.SetArgs([]string{"--help"})
	output := captureStdout(t, root.Execute)
	for _, want := range []string{
		"Print the Adapter install lines (skill file, hook file, reminder)",
		"Hook entry point for PreToolUse and reminder events (called by the Adapters)",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("help is missing %q: %s", want, output)
		}
	}
}
