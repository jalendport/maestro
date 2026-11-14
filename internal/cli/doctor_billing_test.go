package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorBillingWarnings(t *testing.T) {
	for _, tc := range []struct {
		name, harness, variable, billing, account, strip string
		unset, empty, absent, wantWarning                bool
	}{
		{name: "Claude API key", harness: "claude", variable: "ANTHROPIC_API_KEY", wantWarning: true},
		{name: "Claude auth token", harness: "claude", variable: "ANTHROPIC_AUTH_TOKEN", wantWarning: true},
		{name: "Codex API key", harness: "codex", variable: "OPENAI_API_KEY", wantWarning: true},
		{name: "renamed Account", harness: "claude", variable: "ANTHROPIC_API_KEY", account: "claude-work", wantWarning: true},
		{name: "metered Claude", harness: "claude", variable: "ANTHROPIC_API_KEY", billing: "metered"},
		{name: "metered Claude token", harness: "claude", variable: "ANTHROPIC_AUTH_TOKEN", billing: "metered"},
		{name: "metered Codex", harness: "codex", variable: "OPENAI_API_KEY", billing: "metered"},
		{name: "empty Claude key", harness: "claude", variable: "ANTHROPIC_API_KEY", empty: true},
		{name: "unset Claude key", harness: "claude", variable: "ANTHROPIC_API_KEY", unset: true},
		{name: "empty Codex key", harness: "codex", variable: "OPENAI_API_KEY", empty: true},
		{name: "unset Codex key", harness: "codex", variable: "OPENAI_API_KEY", unset: true},
		{name: "Jev key", harness: "codex", variable: "TYPESAFE_API_KEY", strip: `["TYPESAFE_API_KEY"]`},
		{name: "effort setting", harness: "claude", variable: "CLAUDE_CODE_EFFORT_LEVEL"},
		{name: "other Harness key", harness: "claude", variable: "OPENAI_API_KEY"},
		{name: "uninstalled Harness", harness: "codex", variable: "OPENAI_API_KEY", absent: true},
		{name: "credential kept by override", harness: "codex", variable: "OPENAI_API_KEY", strip: `[]`},
		{name: "custom API key", harness: "codex", variable: "CUSTOM_API_KEY_SUFFIX", strip: `["CUSTOM_API_KEY_SUFFIX"]`, wantWarning: true},
		{name: "custom auth token", harness: "claude", variable: "CUSTOM_AUTH_TOKEN_SUFFIX", strip: `["CUSTOM_AUTH_TOKEN_SUFFIX"]`, wantWarning: true},
	} {
		for _, machine := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/text", true: "/JSON"}[machine], func(t *testing.T) {
				home, project := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("MAESTRO_HOME", t.TempDir())
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
				t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
				t.Setenv("GROK_HOME", t.TempDir())
				t.Setenv("PATH", "")
				t.Setenv("SOLO_PROCESS_ID", "")
				t.Chdir(project)
				for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "OPENAI_API_KEY", "TYPESAFE_API_KEY", "CLAUDE_CODE_EFFORT_LEVEL"} {
					t.Setenv(name, "")
				}
				const secret = "doctor-test-secret-value"
				t.Setenv(tc.variable, secret)
				if tc.empty {
					t.Setenv(tc.variable, "")
				}
				if tc.unset {
					if err := os.Unsetenv(tc.variable); err != nil {
						t.Fatal(err)
					}
				}
				account, billing := tc.account, tc.billing
				if account == "" {
					account = tc.harness
				}
				if billing == "" {
					billing = "subscription"
				}
				source := fmt.Sprintf("[accounts.%s]\nharness = %q\nbilling = %q\n[harness.%s]\n", account, tc.harness, billing, tc.harness)
				if !tc.absent {
					bin := filepath.Join(t.TempDir(), tc.harness)
					script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo '9.0.0'\nelse\n  echo '{\"models\":[]}'\nfi\n"
					if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
						t.Fatal(err)
					}
					source += fmt.Sprintf("bin = %q\n", bin)
				}
				if tc.strip != "" {
					source += "strip_env = " + tc.strip + "\n"
				}
				initTestFile(t, filepath.Join(os.Getenv("MAESTRO_HOME"), "config.toml"), source)
				command := (&app{json: machine}).doctorCmd()
				output := captureStdout(t, command.Execute)
				if strings.Contains(output, secret) {
					t.Fatal("doctor printed the credential value")
				}
				message := fmt.Sprintf("%s is set, but Account %s is a subscription, so Lanes do not get it; if you pay by API key, set billing = \"metered\" under [accounts.%s] in ~/.maestro/config.toml", tc.variable, account, account)
				warnings := 0
				if machine {
					var rows []doctorRow
					if err := json.Unmarshal([]byte(output), &rows); err != nil {
						t.Fatal(err)
					}
					for _, row := range rows {
						if strings.HasPrefix(row.State, "warning:") {
							warnings++
							if row.Name != tc.harness || row.State != "warning: "+message || row.Detail != filepath.Join(os.Getenv("MAESTRO_HOME"), "config.toml") {
								t.Fatalf("unexpected warning row: %+v", row)
							}
						}
					}
				} else {
					warnings = strings.Count(output, "warning:")
					if tc.wantWarning && !strings.Contains(output, "warning: "+message) {
						t.Fatalf("warning missing from output: %s", output)
					}
				}
				want := 0
				if tc.wantWarning {
					want = 1
				}
				if warnings != want {
					t.Fatalf("got %d warnings, want %d: %s", warnings, want, output)
				}
			})
		}
	}
}
