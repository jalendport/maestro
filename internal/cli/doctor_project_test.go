package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/config"
)

func TestDoctorProjectWarnings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		warnings     []string
	}{
		{"no Project file", "", nil},
		{"commented checks", "# checks = ['go test ./...']\n", []string{"no active checks"}},
		{"empty checks", "checks = []\n", []string{"no active checks"}},
		{"Docker without cache", "checks = ['docker compose run --rm test', 'docker build .']\n", []string{"Docker check needs ~/.docker in dirs"}},
		{"Docker with cache", "checks = ['docker compose run --rm test']\ndirs = ['~/.docker']\n", nil},
		{"Docker with absolute cache", "checks = ['docker build .']\ndirs = [CACHE]\n", nil},
		{"other checks", "checks = ['go test ./...']\n", nil},
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
				if tc.source != "" {
					source := strings.ReplaceAll(tc.source, "CACHE", "'"+filepath.Join(home, ".docker")+"'")
					initTestFile(t, config.ProjectFile(project), source)
				}
				command := (&app{json: machine}).doctorCmd()
				output := captureStdout(t, command.Execute)
				if machine {
					var rows []doctorRow
					if err := json.Unmarshal([]byte(output), &rows); err != nil {
						t.Fatal(err)
					}
					output = ""
					for _, row := range rows {
						if row.Name == "project" {
							output += row.State + "\n"
						}
					}
				}
				if strings.Count(output, "warning:") != len(tc.warnings) {
					t.Fatalf("output = %s, want %v", output, tc.warnings)
				}
				for _, warning := range tc.warnings {
					if !strings.Contains(output, "warning: "+warning) {
						t.Fatalf("output = %s, want %s", output, warning)
					}
				}
			})
		}
	}
}

func TestDoctorDirtyTreeInformation(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "JSON"}[asJSON], func(t *testing.T) {
			root, _ := worktreeConfigFixture(t)
			writeWorktreeConfig(t, config.ProjectFile(root), "checks = ['go test ./...']\n")
			writeWorktreeConfig(t, filepath.Join(root, "base.txt"), "changed\n")
			command := (&app{json: asJSON}).doctorCmd()
			output := captureStdout(t, command.Execute)
			const detail = "tree has uncommitted changes: Tasks start from HEAD"
			if asJSON {
				var rows []doctorRow
				if err := json.Unmarshal([]byte(output), &rows); err != nil {
					t.Fatal(err)
				}
				found := false
				for _, row := range rows {
					if row.Name == "project" && row.Detail == detail && row.State == "info" {
						found = true
					}
				}
				if !found {
					t.Fatalf("project information row is missing: %s", output)
				}
			} else if !strings.Contains(strings.Join(strings.Fields(output), " "), "project "+detail+" info") {
				t.Fatalf("project information row is missing: %s", output)
			}
			if strings.Contains(output, "warning:") || strings.Contains(output, "Runs use the tree one Task at a time") {
				t.Fatalf("doctor warns about uncommitted changes: %s", output)
			}
		})
	}
}
