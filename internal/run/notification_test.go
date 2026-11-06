package run

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/dispatch"
)

func TestEndCallsNotifierWithProjectAndSummary(t *testing.T) {
	for _, tc := range []struct {
		state string
		tasks []*TaskState
		want  string
	}{
		{StateFinished, []*TaskState{{State: TaskDone}, {State: TaskDone}}, "maestro: Run r-0077 finished: 2 done, 0 blocked, 0 failed"},
		{StatePaused, []*TaskState{{State: TaskDone}, {State: TaskBlocked}, {State: TaskFailed}}, "maestro: Run r-0077 paused: 1 done, 1 blocked, 1 failed"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			previous := notify
			t.Cleanup(func() { notify = previous })
			var messages []string
			notify = func(text string) { messages = append(messages, text) }
			sup := &Supervisor{
				Env: &dispatch.Env{},
				State: &State{Run: "r-0077", Project: filepath.Join(t.TempDir(), "maestro"),
					Folder: t.TempDir(), StartedAt: time.Now(), Tasks: tc.tasks},
			}
			if err := sup.end(tc.state); err != nil {
				t.Fatal(err)
			}
			if len(messages) != 1 || messages[0] != tc.want {
				t.Fatalf("notifications = %q, want [%q]", messages, tc.want)
			}
		})
	}
}

func TestDefaultNotifyRunsNothingDuringTests(t *testing.T) {
	if !testing.Testing() {
		t.Fatal("test mode is off")
	}
	folder := t.TempDir()
	marker := filepath.Join(folder, "notification")
	t.Setenv("PATH", folder)
	t.Setenv("MAESTRO_NOTIFICATION_MARKER", marker)
	script := "#!/bin/sh\nprintf '%s\\n' called > \"$MAESTRO_NOTIFICATION_MARKER\"\n"
	if err := os.WriteFile(filepath.Join(folder, "osascript"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Check the fixture before testing that the notifier leaves it unused.
	if err := exec.Command("osascript").Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	defaultNotify("maestro: Run r-0077 finished: 1 done, 0 blocked, 0 failed")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("notifier ran osascript or marker check failed: %v", err)
	}
}
