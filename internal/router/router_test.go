package router

import (
	"encoding/json"
	"fmt"
	"github.com/jalendport/maestro/internal/ledger"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
)

func testRouter(t *testing.T) *Router {
	t.Helper()
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "")
	d, _, _ := config.ParseFile(roster.Defaults)
	r := roster.Merge(d, &config.File{})
	r.Router.Backend = "rules"
	disc := &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{
		"claude": {Path: "/bin/claude", Version: "2.1.296", Models: []harness.Model{{ID: "haiku"}, {ID: "sonnet"}, {ID: "opus"}, {ID: "fable"}}},
		"codex":  {Path: "/bin/codex", Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}, {ID: "gpt-6-luna"}, {ID: "gpt-6-astra"}}},
		"grok":   {Path: "/bin/grok", Version: "1.0.50", Models: []harness.Model{{ID: "grok-4.7"}}},
		"agy":    {Path: "/bin/agy", Version: "1.2.9", Models: []harness.Model{{ID: "gemini-3.8-flash"}, {ID: "gemini-3.1-pro"}}},
	}}
	return &Router{Roster: r, Disc: disc, Reg: harness.New(map[string]string{"claude": "/bin/claude", "codex": "/bin/codex", "grok": "/bin/grok", "agy": "/bin/agy"})}
}

func mustBrief(t *testing.T, src string) *brief.Brief {
	b, err := brief.ParseText(src)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRulesPickHeavyImplementGoesToCodex(t *testing.T) {
	rt := testRouter(t)
	b := mustBrief(t, "---\ntitle: x\ncategory: implement\nlevel: significant\n---\n## Goal\ng\n## Done\nd\n")
	r, err := rt.Route(Request{Brief: b, Access: "edit"})
	if err != nil {
		t.Fatal(err)
	}
	if r.FamilyID != "codex-sol" || r.Tier != roster.TierHeavy || r.Effort != "high" || r.Model != "gpt-6.1-sol" {
		t.Fatalf("%+v", r)
	}
}

func TestDefaultsLandOnDaily(t *testing.T) {
	rt := testRouter(t)
	b := mustBrief(t, "---\ntitle: x\n---\n## Goal\ng\n## Done\nd\n")
	r, err := rt.Route(Request{Brief: b, Access: "edit"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Tier != roster.TierDaily || r.FamilyID != "codex-sol" || r.Effort != "medium" || len(r.DefaultsUsed) != 2 {
		t.Fatalf("%+v", r)
	}
}

func TestLightNeverEdits(t *testing.T) {
	rt := testRouter(t)
	b := mustBrief(t, "---\ntitle: x\nlevel: none\ncategory: refactor\n---\n## Goal\ng\n## Done\nd\n")
	r, err := rt.Route(Request{Brief: b, Access: "edit"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Tier == roster.TierLight {
		t.Fatalf("light took an edit: %+v", r)
	}
}

func TestReadRoutesToGrok(t *testing.T) {
	rt := testRouter(t)
	b := mustBrief(t, "---\ntitle: x\ncategory: research\nlevel: some\n---\n## Goal\ng\n## Done\nd\n")
	r, err := rt.Route(Request{Brief: b, Access: "read"})
	if err != nil {
		t.Fatal(err)
	}
	if r.FamilyID != "grok" {
		t.Fatalf("read went to %s", r.FamilyID)
	}

}

func TestRequestedLaneWarns(t *testing.T) {
	rt := testRouter(t)
	rt.Roster.Families["claude-fable"].RequestOnly = true
	b := mustBrief(t, "---\ntitle: x\n---\n## Goal\ng\n## Done\nd\n")
	r, err := rt.Route(Request{Brief: b, Access: "edit", Lane: "claude-fable:high"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Requested || r.OverrideWarning == "" || r.Effort != "high" || r.Model != "fable" {
		t.Fatalf("%+v", r)
	}
}

func TestAgyRefusesEdit(t *testing.T) {
	rt := testRouter(t)
	b := mustBrief(t, "---\ntitle: x\n---\n## Goal\ng\n## Done\nd\n")
	if _, err := rt.Route(Request{Brief: b, Access: "edit", Lane: "agy-flash"}); err == nil {
		t.Fatal("agy must refuse an edit task")
	}
}

func TestFloorShrinksTheList(t *testing.T) {
	rt := testRouter(t)
	b := mustBrief(t, "---\ntitle: x\ncategory: review\nlevel: significant\n---\n## Goal\ng\n## Done\nd\n")
	r, err := rt.Route(Request{Brief: b, Access: "read", Floor: roster.TierFrontier, Excluded: []string{"claude-opus"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Tier != roster.TierFrontier {
		t.Fatalf("floor frontier picked %s at %s", r.FamilyID, r.Tier)
	}
}

func TestRouteRedactsAllOutboundStrings(t *testing.T) {
	rt := testRouter(t)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	requests := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- body
		_, _ = io.WriteString(w, `{"answers":{}}`)
	}))
	defer server.Close()
	rt.Roster.Router.BaseURL = server.URL
	f := rt.Roster.Families["codex-sol"]
	f.GoodFor = "MARKED"
	b := &brief.Brief{Goal: "MARKED", Done: "MARKED", Facts: map[string]string{"fact": "MARKED"}}
	b.Title = "MARKED"
	_, jr, _ := rt.jevRoute(Request{Brief: b, Project: &config.Project{Redact: []string{"MARKED"}}}, []*roster.Family{f}, roster.LevelSome, "implement", nil)
	if jr.Error != "" {
		t.Fatal(jr.Error)
	}
	body := <-requests
	if strings.Contains(string(body), "MARKED") {
		t.Fatalf("unredacted request: %s", body)
	}
	var payload struct {
		State struct {
			Task struct {
				Title, Goal, Done string
				Facts             map[string]string
			}
		}
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	task := payload.State.Task
	if task.Title != "[redacted]" || task.Goal != "[redacted]" || task.Done != "[redacted]" || len(task.Facts) != 1 || task.Facts["fact"] != "[redacted]" {
		t.Fatalf("redaction lost: %+v", task)
	}
}

func TestMeteredCapBindsRouterViewAndRequestedRoute(t *testing.T) {
	for _, tc := range []struct {
		spent float64
		state string
	}{{0, "ok"}, {16, "ok"}, {17, "warning"}, {19, "warning"}, {20, "blocked"}, {21, "blocked"}} {
		t.Run(tc.state+fmt.Sprint(tc.spent), func(t *testing.T) {
			rt := testRouter(t)
			rt.Roster.Budget.WarnAt = .8
			account := rt.Roster.Accounts["agy"]
			account.Billing = "metered"
			account.Cap = &config.Cap{USD: 20, Period: "30d"}
			rt.Roster.Accounts["agy"] = account
			for _, row := range []map[string]any{
				{"kind": "dispatch", "ended_at": time.Now(), "route": map[string]any{"account": "agy"}, "budget": map[string]any{"cost_usd": tc.spent}},
				{"kind": "dispatch", "ended_at": time.Now().Add(-31 * 24 * time.Hour), "route": map[string]any{"account": "agy"}, "budget": map[string]any{"cost_usd": 100}},
				{"kind": "dispatch", "ended_at": time.Now(), "route": map[string]any{"account": "other"}, "budget": map[string]any{"cost_usd": 100}},
				{"kind": "run", "ended_at": time.Now(), "route": map[string]any{"account": "agy"}, "budget": map[string]any{"cost_usd": 100}},
			} {
				if err := ledger.Append(row); err != nil {
					t.Fatal(err)
				}
			}
			v := rt.View("agy")
			if v.State != tc.state || v.Tight == nil || v.Tight.Used != tc.spent/20 {
				t.Fatalf("view %+v for spend %v", v, tc.spent)
			}
			b := &brief.Brief{}
			_, err := rt.Route(Request{Brief: b, Access: "read", Lane: "agy-flash"})
			if (err != nil) != (tc.state == "blocked") {
				t.Fatalf("route error %v for spend %v", err, tc.spent)
			}
		})
	}
}

func TestPreflightRejectsMultipleAccountsPerHarness(t *testing.T) {
	rt := testRouter(t)
	rt.Roster.Accounts["other-codex"] = config.Account{Harness: "codex"}
	why := rt.Preflight(rt.Roster.Families["codex-sol"], "edit")
	if !strings.Contains(why, "multiple accounts") || !strings.Contains(why, "codex") || !strings.Contains(why, "other-codex") {
		t.Fatal(why)
	}
}

func TestOrchestratorExcludedFromAutomaticRoutes(t *testing.T) {
	rt := testRouter(t)
	d, _, _ := config.ParseFile(roster.Defaults)
	u, _, err := config.ParseFile(`[router]
backend = "rules"
orchestrator = "codex-sol"
`)
	if err != nil {
		t.Fatal(err)
	}
	rt.Roster = roster.Merge(d, u)
	b := mustBrief(t, "---\ntitle: x\ncategory: implement\nlevel: significant\n---\n## Goal\ng\n## Done\nd\n")
	r, err := rt.Route(Request{Brief: b, Access: "edit"})
	if err != nil {
		t.Fatal(err)
	}
	if r.FamilyID == "codex-sol" {
		t.Fatal("automatic route picked the Orchestrator")
	}
	found := false
	for _, e := range r.Excluded {
		if e.Family == "codex-sol" && e.Rule == "request only" {
			found = true
		}
	}
	if !found {
		t.Fatal("Orchestrator exclusion missing", r.Excluded)
	}
	r, err = rt.Route(Request{Brief: b, Access: "edit", Lane: "codex-sol"})
	if err != nil || r.FamilyID != "codex-sol" || !r.Requested {
		t.Fatalf("explicit request: %+v, %v", r, err)
	}
}

func TestNoKeyUsesRulesWithoutWarning(t *testing.T) {
	rt := testRouter(t)
	rt.Roster.Router.Backend = "jev"
	b := mustBrief(t, "---\ntitle: x\ncategory: implement\nlevel: some\n---\n## Goal\ng\n## Done\nd\n")
	r, err := rt.Route(Request{Brief: b, Access: "edit"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "rules" || r.Jev != nil || r.Band == "fallback" || r.OverrideWarning != "" || strings.Contains(r.ReasonLine, "key") || strings.Contains(r.ReasonLine, "fallback") || strings.Contains(r.ReasonLine, "warning") {
		t.Fatalf("no-key route: %+v", r)
	}
	rt.Roster.Router.Backend = "rules"
	explicit, err := rt.Route(Request{Brief: b, Access: "edit"})
	if err != nil || explicit.ReasonLine != r.ReasonLine {
		t.Fatalf("rules differ: %+v, %v", explicit, err)
	}
}

func TestCodexReserveAndAllocationWeight(t *testing.T) {
	rt := testRouter(t)
	account := rt.Roster.Accounts["codex"]
	account.Reserve = .35
	rt.Roster.Accounts["codex"] = account
	rt.Budget = &budget.File{Accounts: map[string]*budget.Account{}}
	f := rt.Roster.Families["codex-sol"]
	rt.Budget.Record("codex", []harness.Window{{Kind: "weekly", Used: .4}}, budget.SourceHarvest)
	v := rt.View("codex")
	if v.State != "ok" || v.Headroom < .249 || v.Headroom > .251 {
		t.Fatalf("reserve not applied: %+v", v)
	}
	if weight := rt.Weight(f); weight < .249 || weight > .251 {
		t.Fatalf("weight without price: %v", weight)
	}
	account.PlanPrice = 20
	rt.Roster.Accounts["codex"] = account
	if weight := rt.Weight(f); weight < 4.99 || weight > 5.01 {
		t.Fatalf("weight with price: %v", weight)
	}
	rt.Budget.Record("codex", []harness.Window{{Kind: "weekly", Used: .66}}, budget.SourceHarvest)
	if v = rt.View("codex"); v.State != "blocked" || v.Headroom != 0 {
		t.Fatalf("reserve not blocking: %+v", v)
	}
	r, err := rt.Route(Request{Brief: &brief.Brief{}, Access: "edit"})
	if err != nil || r.FamilyID == f.ID {
		t.Fatalf("blocked Codex route: %+v, %v", r, err)
	}
}

func TestPreflightClaudeFullModelIDs(t *testing.T) {
	rt := testRouter(t)
	f := *rt.Roster.Families["claude-opus"]
	for _, id := range []string{"opus", "claude-opus-5-5", "claude-opus-5-5-20261001"} {
		f.Models = []string{id}
		if why := rt.Preflight(&f, "read"); why != "" {
			t.Fatalf("%s: %s", id, why)
		}
		route := rt.finish(&f, roster.LevelSome, roster.LevelSome, "research", "read", "requested", nil, nil)
		if route.Model != id {
			t.Fatalf("route model = %q, want %q", route.Model, id)
		}
	}
	for _, id := range []string{"opuss", "cluade-opus-5-5", "claude-opus-5", "claude-opus-5-5-bad"} {
		f.Models = []string{id}
		if why := rt.Preflight(&f, "read"); !strings.Contains(why, "not in the claude catalog") {
			t.Fatalf("%s: %s", id, why)
		}
	}
}

func TestPreflightChecksDiscoveredEfforts(t *testing.T) {
	rt := testRouter(t)
	f := *rt.Roster.Families["codex-sol"]
	f.Levels = map[roster.Level]string{roster.LevelSome: "ultra"}
	rec := rt.Disc.Harness["codex"]
	rec.Models[0].Efforts = []string{"low", "medium", "high"}
	rt.Disc.Harness["codex"] = rec
	why := rt.Preflight(&f, "edit")
	for _, want := range []string{"codex-sol", "ultra", "low, medium, high"} {
		if !strings.Contains(why, want) {
			t.Fatalf("missing %q: %s", want, why)
		}
	}
	f.Levels[roster.LevelSome] = "high"
	if why := rt.Preflight(&f, "edit"); why != "" {
		t.Fatal(why)
	}
	rec.Models[0].Efforts = nil
	rt.Disc.Harness["codex"] = rec
	f.Levels[roster.LevelSome] = "ultra"
	if why := rt.Preflight(&f, "edit"); why != "" {
		t.Fatal(why)
	}
}
