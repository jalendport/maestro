package council

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/roster"
)

func TestListCouncilsAndSeatState(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	for _, tt := range []struct {
		slug, state string
		pid         int
		ended       time.Time
		pidFile     bool
	}{
		{"live", "running", os.Getpid(), time.Time{}, false},
		{"starting", "running", 0, time.Time{}, true},
		{"dead", "running", 0, time.Time{}, false},
		{"recent", "finished", 0, now.Add(-time.Minute), false},
		{"old", "finished", 0, now.Add(-2 * time.Hour), false},
	} {
		folder := filepath.Join(config.CouncilsDir(root), tt.slug)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		c := &Council{Slug: tt.slug, Path: folder, State: tt.state, Pid: tt.pid, StartedAt: now.Add(-time.Hour), EndedAt: tt.ended, Tier: roster.TierHeavy, RoundsRun: 2,
			Seats: []*Seat{{Family: "codex-sol", Effort: "high"}, {Family: "dropped", Rounds: []SeatRound{{Round: 1, Status: "dropped"}}}}}
		if err := c.Save(); err != nil {
			t.Fatal(err)
		}
		if tt.pidFile {
			if err := os.WriteFile(filepath.Join(folder, "supervisor.pid"), []byte(fmt.Sprint(os.Getpid())), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		seatFolder := filepath.Join(folder, "round-2", "codex-sol")
		if err := os.MkdirAll(seatFolder, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := (&dispatch.Record{Path: seatFolder, State: "running"}).Save(); err != nil {
			t.Fatal(err)
		}
	}
	got := List(root)
	if len(got) != 3 || got[0].State != "running" || got[1].State != "running" || got[2].Slug != "recent" {
		t.Fatalf("Councils: %+v", got)
	}
	for _, value := range []string{"Heavy", "round 2", "codex-sol high (running)", "dropped  (dropped)"} {
		if !strings.Contains(Status(got[0]), value) {
			t.Fatalf("missing %q: %s", value, Status(got[0]))
		}
	}
	if !strings.Contains(Status(got[2]), "merge ready") {
		t.Fatal("recent Council has no merge ready label")
	}
}
