package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/proc"
)

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
