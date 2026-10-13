package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/roster"
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

func TestSoloPresetNoteNamesChoicesAndUserSetting(t *testing.T) {
	env := &dispatch.Env{Roster: &roster.Roster{Harness: map[string]config.Harness{}}, Disc: &discovery.Cache{Harness: map[string]discovery.Record{"claude": {SoloPresets: []discovery.SoloPreset{{ID: 1, Name: "Claude A"}, {ID: 2, Name: "Claude B"}}}}}}
	note := soloPresetNote(env, "claude")
	for _, want := range []string{"Claude A (1)", "Claude B (2)", "[harness.claude] solo_tool", config.UserFile()} {
		if !strings.Contains(note, want) {
			t.Fatalf("missing %s: %s", want, note)
		}
	}
	env.Roster.Harness["claude"] = config.Harness{SoloTool: 2}
	if got := soloPresetNote(env, "claude"); got != "agent tool id 2 (user file)" {
		t.Fatal(got)
	}
}

func TestSoloDiscoveryRefreshesOldCache(t *testing.T) {
	cache := &discovery.Cache{Harness: map[string]discovery.Record{"claude": {SoloTool: 99}}}
	if !soloToolsMissing(cache) {
		t.Fatal("old preset selection trusted without discovery")
	}
	cache.Harness["claude"] = discovery.Record{SoloPresets: []discovery.SoloPreset{{ID: 1}, {ID: 2}}}
	if soloToolsMissing(cache) {
		t.Fatal("recorded ambiguity requires discovery again")
	}
}
