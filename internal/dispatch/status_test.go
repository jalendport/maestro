package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/router"
)

func TestStatusWithoutStartTime(t *testing.T) {
	for _, state := range []string{"running", "ended"} {
		t.Run(state, func(t *testing.T) {
			rec := &Record{Task: "never-started", State: state, DurationS: 60, Outcome: Outcome{Outcome: OutcomeCancelled}}
			got := Status(rec)
			if !strings.Contains(got, "attempt 0  —  tab no") {
				t.Fatalf("elapsed time without a start: %s", got)
			}
		})
	}
}

func TestListStandaloneDispatches(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	for _, tt := range []struct {
		run, task, state string
		ended            time.Time
		inRun            bool
	}{
		{"r-0001", "live", "running", time.Time{}, false},
		{"r-0002", "recent", "ended", now.Add(-30 * time.Minute), false},
		{"r-0003", "old", "ended", now.Add(-2 * time.Hour), false},
		{"r-0004", "managed", "running", time.Time{}, true},
	} {
		folder := filepath.Join(config.RunsDir(root), tt.run, tt.task)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		rec := &Record{Task: tt.task, State: tt.state, StartedAt: now.Add(-time.Minute), EndedAt: tt.ended, Path: folder,
			TaskInfo: TaskInfo{Title: "Task title"}, Route: &router.Route{FamilyID: "codex-sol", Effort: "high"}, Attempt: 2, SoloProcess: 42,
			Outcome: Outcome{Outcome: OutcomeUnverified}}
		if err := rec.Save(); err != nil {
			t.Fatal(err)
		}
		if tt.inRun {
			if err := os.WriteFile(filepath.Join(filepath.Dir(folder), "run.json"), []byte(`{}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := List(root)
	if len(got) != 2 || got[0].Task != "live" || got[1].Task != "recent" {
		t.Fatalf("Dispatches: %+v", got)
	}
	for _, value := range []string{"live", "Task title", "codex-sol high", "attempt 2", "1m00s", "tab yes", "running"} {
		if !strings.Contains(Status(got[0]), value) {
			t.Fatalf("missing %q: %s", value, Status(got[0]))
		}
	}
	if !strings.Contains(Status(got[1]), "unverified") {
		t.Fatal("recent result has no outcome")
	}
}
