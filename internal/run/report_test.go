package run

import (
	"strings"
	"testing"
	"time"
)

func TestStatusTaskElapsed(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		name, state, want string
		started           time.Time
	}{
		{"cancelled before start", TaskCancelled, "0    —", time.Time{}},
		{"skipped before start", TaskSkipped, "0    —", time.Time{}},
		{"running without start", TaskRunning, "1    —   idle", time.Time{}},
		{"finished after start", TaskDone, "0    1m00s", now.Add(-time.Minute)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &State{Folder: t.TempDir(), StartedAt: now, Tasks: []*TaskState{{
				Number: 1, ID: "task", Title: "Task elapsed", State: tt.state,
				StartedAt: tt.started, EndedAt: now, LastOutput: now,
			}}}
			got := Status(s)
			if !strings.Contains(got, tt.want+"\n") && !strings.Contains(got, tt.want+" ") {
				t.Fatalf("missing %q in status: %s", tt.want, got)
			}
		})
	}
}
