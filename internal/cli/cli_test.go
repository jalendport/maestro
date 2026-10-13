package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/proc"
)

func TestInformationCommandsDoNotWrite(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"status"}, {"adapter", "claude"}, {"doctor"}} {
		t.Run(args[0], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("MAESTRO_HOME", home)
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
