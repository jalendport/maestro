package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/proc"
)

func TestInformationCommandsDoNotWrite(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"status"}, {"adapter", "claude"}, {"doctor"}} {
		t.Run(args[0], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("MAESTRO_HOME", home)
			t.Setenv("GROK_HOME", t.TempDir())
			t.Setenv("PATH", "")
			t.Setenv("SOLO_PROCESS_ID", "")
			root := (&app{version: "test"}).build()
			root.SetArgs(args)
			err := root.Execute()
			if args[0] != "status" && err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 {
				t.Fatalf("home entries = %v, error = %v", entries, err)
			}
		})
	}
}

func TestDoctorFailsForInstalledHarnessError(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	t.Setenv("PATH", "")
	t.Setenv("SOLO_PROCESS_ID", "")
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserFile(), []byte("[harness.claude]\nbin = "+`"`+bin+`"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := (&app{}).build()
	root.SetArgs([]string{"doctor"})
	err := root.Execute()
	if code, ok := proc.Code(err); !ok || code != proc.CodeFailed {
		t.Fatalf("doctor error = %v", err)
	}
	if _, err := os.Stat(config.DiscoveryFile()); !os.IsNotExist(err) {
		t.Fatalf("discovery cache error = %v", err)
	}
}

func TestDoctorWarnsOnceForRemovedUserKeys(t *testing.T) {
	// Build retired names in parts so the source search lists only live keys.
	for _, tc := range []struct {
		section, key, value string
	}{
		{"accounts.claude", "paid_" + "overflow", "true"},
		{"accounts.claude", "budget_" + "source", `"harvest+probe"`},
		{"accounts.claude", "base_" + "url", `"https://example.test"`},
		{"failure", "cooling_" + "period", `"1h"`},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("GROK_HOME", t.TempDir())
			t.Setenv("PATH", "")
			t.Setenv("SOLO_PROCESS_ID", "")
			src := fmt.Sprintf("[%s]\n%s = %s\n", tc.section, tc.key, tc.value)
			if err := os.WriteFile(config.UserFile(), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			_, unknown, err := config.LoadUser()
			key := tc.section + "." + tc.key
			if err != nil || len(unknown) != 1 || unknown[0] != key {
				t.Fatalf("load error = %v, unknown = %v", err, unknown)
			}
			stderr, err := os.CreateTemp(t.TempDir(), "doctor-stderr")
			if err != nil {
				t.Fatal(err)
			}
			defer stderr.Close()
			previous := os.Stderr
			os.Stderr = stderr
			defer func() { os.Stderr = previous }()
			root := (&app{}).build()
			root.SetArgs([]string{"doctor"})
			captureStdout(t, root.Execute)
			if _, err := stderr.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			output, err := io.ReadAll(stderr)
			if err != nil {
				t.Fatal(err)
			}
			warning := fmt.Sprintf("unknown key %q", key)
			if strings.Count(string(output), warning) != 1 {
				t.Fatalf("doctor output = %q, want one %q", output, warning)
			}
		})
	}
}
