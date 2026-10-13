package council

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
)

// SupervisorAlive checks the pid saved at detach, or the inline supervisor.
func SupervisorAlive(c *Council) bool {
	pid := c.Pid
	if b, err := os.ReadFile(filepath.Join(c.Path, "supervisor.pid")); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n > 0 {
			pid = n
		}
	}
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

// List lists live Councils, followed by Councils finished in the last hour.
func List(root string) []*Council {
	out := []*Council{}
	entries, _ := os.ReadDir(config.CouncilsDir(root))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		c, err := Load(filepath.Join(config.CouncilsDir(root), e.Name()))
		if err != nil {
			continue
		}
		if (c.State == "running" && SupervisorAlive(c)) || (c.State == "finished" && !c.EndedAt.IsZero() && time.Since(c.EndedAt) <= time.Hour) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].State != out[j].State {
			return out[i].State == "running"
		}
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

// Status renders a Council line with the state of each Seat in this Round.
func Status(c *Council) string {
	round := max(1, c.RoundsRun)
	folder := c.Path
	if round > 1 {
		folder = filepath.Join(folder, fmt.Sprintf("round-%d", round))
	}
	var seats []string
	for _, s := range c.Seats {
		state := "waiting"
		if r := seatRound(s, round); r != nil {
			state = r.Status
		} else if rec, err := dispatch.LoadRecord(filepath.Join(folder, s.Family)); err == nil {
			state = rec.State
			if state == "ended" && rec.Outcome.Outcome != "" {
				state = rec.Outcome.Outcome
			}
		} else if round > 1 {
			if r := last(s); r != nil && r.Status != "ok" {
				state = r.Status
			}
		}
		seats = append(seats, s.Family+" "+s.Effort+" ("+state+")")
	}
	ready := ""
	if c.State == "finished" {
		ready = "  merge ready"
	}
	return fmt.Sprintf("  council %s  %s  round %d  %s%s\n", c.Slug, titleCase(string(c.Tier)), round, strings.Join(seats, ", "), ready)
}
