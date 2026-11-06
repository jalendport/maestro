package cli

import (
	"os"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/roster"
)

func TestLoadEnvPrunesBudgetToRosterAccounts(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	t.Setenv("PATH", "")
	t.Setenv("SOLO_PROCESS_ID", "")
	t.Chdir(t.TempDir())
	r, err := roster.Load()
	if err != nil {
		t.Fatal(err)
	}
	var kept string
	for id := range r.Accounts {
		kept = id
		break
	}
	if kept == "" {
		t.Fatal("the Roster has no Account")
	}
	f, err := budget.Load()
	if err != nil {
		t.Fatal(err)
	}
	f.RecordManual(kept, .2)
	f.RecordManual("renamed-account", .4)
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(config.BudgetFile())
	if err != nil {
		t.Fatal(err)
	}
	env, err := (&app{}).loadEnv(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if env.Budget.Snapshot("renamed-account") != nil || env.Budget.Snapshot(kept) == nil {
		t.Fatalf("loaded Budget = %+v", env.Budget.Accounts)
	}
	after, err := os.ReadFile(config.BudgetFile())
	if err != nil || string(after) != string(before) {
		t.Fatalf("load wrote the Budget: %v", err)
	}
	if err := env.Budget.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := budget.Load()
	if err != nil || len(loaded.Accounts) != 1 || loaded.Snapshot(kept) == nil {
		t.Fatalf("saved Budget = %+v, error = %v", loaded, err)
	}
}

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
