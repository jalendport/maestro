package cli

import (
	"encoding/json"
	"testing"

	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/router"
)

func TestStatisticsExcludeCanaryRows(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	for _, canary := range []bool{false, true} {
		row := &dispatch.Record{Kind: "dispatch", Canary: canary, Attempt: 1,
			Route:   &router.Route{Source: "jev", Band: "medium", Agreed: true, RulesPick: "codex-sol"},
			Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeVerified}}
		if err := ledger.Append(row); err != nil {
			t.Fatal(err)
		}
		row.Outcome.Outcome = dispatch.OutcomeEscalated
		if err := ledger.Append(row); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"router", "stats", "--json"}, {"stats", "--json"}} {
		root := (&app{}).build()
		root.SetArgs(args)
		out := captureStdout(t, root.Execute)
		var stats routerStats
		if err := json.Unmarshal([]byte(out), &stats); err != nil {
			t.Fatal(err)
		}
		if stats.Routes != 2 || stats.Compared != 2 || stats.Agreed != 2 || stats.Medium != 2 || stats.BySource["jev"] != 2 || stats.Attempted["jev"] != 2 || stats.Verified["jev"] != 1 || stats.Escalated["jev"] != 1 {
			t.Fatalf("Canary counted by %v: %+v", args, stats)
		}
	}
}
