package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/roster"
)

func TestHookQueuesAwakeAfterReminderOnlyInScope(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	t.Setenv("MAESTRO_LANE", "")
	project, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProjectFile(project), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"claude", "codex", "agy"} {
		for _, tc := range []struct {
			name, cwd, lane string
			queued          bool
		}{
			{"in scope", project, "", true},
			{"outside scope", outside, "", false},
			{"Lane", project, "1", false},
		} {
			t.Run(harness+"/"+tc.name, func(t *testing.T) {
				t.Setenv("MAESTRO_TASK_FOLDER", tc.lane)
				payload := map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": tc.cwd}
				if harness == "agy" {
					payload = map[string]any{"invocationNum": 1, "workspacePaths": []string{tc.cwd}}
				}
				data, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				queued := false
				out := runHookWithAwake(t, string(data), harness, func(_ *roster.Roster, event string) {
					if event != "reminder" {
						t.Errorf("event = %s", event)
					}
					queued = true
					fmt.Fprintln(os.Stdout, "awake queued")
				})
				if queued != tc.queued {
					t.Fatalf("queued = %v, want %v", queued, tc.queued)
				}
				if tc.queued && (!strings.HasSuffix(out, "awake queued\n") || !strings.Contains(out, adapter.Reminder)) {
					t.Fatalf("Reminder was not written before sleep control: %s", out)
				}
			})
		}
	}
}

func TestHookQueuesStop(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	t.Setenv("MAESTRO_LANE", "")
	project, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProjectFile(project), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ harness, payload, output string }{
		{"claude", `{"hook_event_name":"Stop"}`, ""},
		{"codex", `{"hook_event_name":"Stop"}`, "{}"},
		{"agy", `{"executionNum":1,"terminationReason":"model_stop","fullyIdle":true}`, `{"decision":"allow"}`},
	} {
		t.Run(tc.harness, func(t *testing.T) {
			for _, scope := range []struct {
				name, cwd, lane, task string
				on, queued            bool
			}{
				{"in scope", project, "", "", true, true},
				{"off", project, "", "", false, false},
				{"outside scope", outside, "", "", true, false},
				{"Lane", project, "1", "", true, false},
				{"Task", project, "", "1", true, false},
			} {
				t.Run(scope.name, func(t *testing.T) {
					t.Setenv("MAESTRO_LANE", scope.lane)
					t.Setenv("MAESTRO_TASK_FOLDER", scope.task)
					if err := os.WriteFile(config.UserFile(), []byte(fmt.Sprintf("[failure]\nkeep_awake = %t\n", scope.on)), 0o600); err != nil {
						t.Fatal(err)
					}
					var payload map[string]any
					if err := json.Unmarshal([]byte(tc.payload), &payload); err != nil {
						t.Fatal(err)
					}
					payload["cwd"] = scope.cwd
					if tc.harness == "agy" {
						payload["workspacePaths"] = []string{scope.cwd}
					}
					data, err := json.Marshal(payload)
					if err != nil {
						t.Fatal(err)
					}
					queued := false
					out := runHookWithAwake(t, string(data), tc.harness, func(_ *roster.Roster, event string) { queued = event == "stop" })
					if queued != scope.queued || strings.TrimSpace(out) != tc.output {
						t.Fatalf("Stop queued=%v, want %v, output=%s", queued, scope.queued, out)
					}
				})
			}
		})
	}
}
