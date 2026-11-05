package cli

import (
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/budget"
)

func TestPaceUsesReportedDuration(t *testing.T) {
	now := time.Now()
	window := budget.Window{Kind: "session", Duration: 2 * time.Hour, Used: .7, ResetsAt: now.Add(time.Hour)}
	if got := budget.WindowName(window); got != "2h" {
		t.Fatal(got)
	}
	if got := pace(window, now); got != "on pace to run out" {
		t.Fatal(got)
	}
	window.Duration = 5 * time.Hour
	if got := pace(window, now); got != "ok" {
		t.Fatal(got)
	}
}
