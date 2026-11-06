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
