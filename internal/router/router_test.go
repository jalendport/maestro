package router

import (
	"encoding/json"
	"fmt"
	"github.com/jalendport/maestro/internal/ledger"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
)

func testRouter(t *testing.T) *Router {
	t.Helper()
	previousOS := sandboxOS
	sandboxOS = "darwin"
	t.Cleanup(func() { sandboxOS = previousOS })
	t.Setenv("MAESTRO_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GROK_HOME", filepath.Join(home, ".grok"))
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
	if result := adapter.Install("grok", os.Getenv("HOME"), "skill"); result.Err != nil {
		t.Fatal(result.Err)
	}
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
	r, err := rt.Route(Request{Brief: b, Access: "read", Lane: "claude-fable:high"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Requested || r.OverrideWarning == "" || r.Effort != "high" || r.Model != "fable" {
		t.Fatalf("%+v", r)
	}
}

func TestRouteEnforcementMatchesLaunch(t *testing.T) {
	for _, tc := range []struct {
		name, family, access, root, want, warning string
		projectDirs, briefDirs                    []string
		auto                                      bool
	}{
		{name: "claude", family: "claude-opus", access: "edit", want: harness.Enforced},
		{name: "claude_auto", family: "claude-opus", access: "edit", auto: true, want: harness.BestEffort, warning: "claude_auto: edit shape is best-effort"},
		{name: "claude_auto_read", family: "claude-opus", access: "read", auto: true, want: harness.Enforced},
		{name: "grok_inside", family: "grok", access: "edit", root: "/project", projectDirs: []string{"/project/cache"}, briefDirs: []string{"/project/vendor"}, want: harness.Enforced},
		{name: "grok_project_outside", family: "grok", access: "edit", root: "/project", projectDirs: []string{"/cache"}, want: harness.BestEffort, warning: "grok: an extra dir outside the project turns the sandbox off; edit shape is best-effort"},
		{name: "grok_brief_outside", family: "grok", access: "edit", root: "/project", briefDirs: []string{"/project-other"}, want: harness.BestEffort, warning: "grok: an extra dir outside the project turns the sandbox off; edit shape is best-effort"},
		{name: "grok_tilde_outside", family: "grok", access: "edit", root: "/project", projectDirs: []string{"~/.cache"}, want: harness.BestEffort, warning: "grok: an extra dir outside the project turns the sandbox off; edit shape is best-effort"},
		{name: "grok_tilde_inside", family: "grok", access: "edit", root: "~", briefDirs: []string{"~/cache"}, want: harness.Enforced},
		{name: "grok_unknown_root", family: "grok", access: "edit", briefDirs: []string{"/outside"}, want: harness.Enforced},
		{name: "grok_read", family: "grok", access: "read", root: "/project", projectDirs: []string{"/outside"}, want: harness.Enforced},
	} {
		for _, source := range []string{"requested", "rules", "council"} {
			t.Run(tc.name+"/"+source, func(t *testing.T) {
				rt := testRouter(t)
				f := rt.Roster.Families[tc.family]
				for _, family := range rt.Roster.Families {
					family.Enabled = family.ID == f.ID
				}
				f.RequestOnly = false
				if f.Harness == "grok" {
					if result := adapter.Install("grok", os.Getenv("HOME"), "skill"); result.Err != nil {
						t.Fatal(result.Err)
					}
				}
				b := &brief.Brief{Fields: brief.Fields{Level: "significant", Dirs: tc.briefDirs}}
				req := Request{Brief: b, Access: tc.access, Root: config.ExpandHome(tc.root), Project: &config.Project{ClaudeAuto: tc.auto, Dirs: tc.projectDirs}}
				switch source {
				case "requested":
					req.Lane = f.ID
				case "council":
					req.Seat = f.ID
				}
				route, err := rt.Route(req)
				if err != nil {
					t.Fatal(err)
				}
				if route.FamilyID != f.ID || route.Source != source || route.Enforcement != tc.want {
					t.Fatalf("Route: %+v", route)
				}
				wantWarning := ""
				if source == "requested" {
					wantWarning = tc.warning
				}
				if route.OverrideWarning != wantWarning || (wantWarning != "" && !strings.Contains(route.ReasonLine, wantWarning)) {
					t.Fatalf("warning=%q reason=%q", route.OverrideWarning, route.ReasonLine)
				}
			})
		}
	}
}

func TestClaudePreflightNeedsLinuxSandboxDependencies(t *testing.T) {
	for _, tc := range []struct {
		os           string
		bwrap, socat bool
	}{
		{os: "linux"}, {os: "linux", bwrap: true}, {os: "linux", socat: true},
		{os: "linux", bwrap: true, socat: true}, {os: "darwin"},
	} {
		t.Run(fmt.Sprintf("%s/bwrap=%t/socat=%t", tc.os, tc.bwrap, tc.socat), func(t *testing.T) {
			rt := testRouter(t)
			sandboxOS = tc.os
			binDir := t.TempDir()
			t.Setenv("PATH", binDir)
			for name, installed := range map[string]bool{"bwrap": tc.bwrap, "socat": tc.socat} {
				if installed {
					if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
						t.Fatal(err)
					}
				}
			}
			want := ""
			if tc.os == "linux" && (!tc.bwrap || !tc.socat) {
				want = "claude sandbox needs bubblewrap and socat on PATH"
			}
			for _, access := range []string{"read", "edit"} {
				if got := rt.Preflight(rt.Roster.Families["claude-opus"], access); got != want {
					t.Fatalf("%s: %q, want %q", access, got, want)
				}
				if got := rt.Preflight(rt.Roster.Families["codex-sol"], access); got != "" {
					t.Fatalf("Codex: %s", got)
				}
			}
		})
	}
}

func TestAgyAcceptsReadAndEdit(t *testing.T) {
	rt := testRouter(t)
	if result := adapter.Install("agy", os.Getenv("HOME"), "skill"); result.Err != nil {
		t.Fatal(result.Err)
	}
	b := mustBrief(t, "---\ntitle: x\n---\n## Goal\ng\n## Done\nd\n")
	for _, access := range []string{"read", "edit"} {
		for _, source := range []string{"flag", "seat"} {
			req := Request{Brief: b, Access: access, Lane: "agy-flash"}
			if source == "seat" {
				req.Lane, req.Seat = "", "agy-flash"
			}
			route, err := rt.Route(req)
			if err != nil || route.FamilyID != "agy-flash" || route.Enforcement != harness.Enforced {
				t.Fatalf("%s/%s: route=%+v err=%v", access, source, route, err)
			}
		}
	}
}

func TestTokenBilledLaneTakesEditOnRequestOnly(t *testing.T) {
	rt := testRouter(t)
	if result := adapter.Install("agy", os.Getenv("HOME"), "skill"); result.Err != nil {
		t.Fatal(result.Err)
	}
	account := rt.Roster.Accounts["agy"]
	account.Billing = "metered"
	rt.Roster.Accounts["agy"] = account
	b := mustBrief(t, "---\ntitle: x\n---\n## Goal\ng\n## Done\nd\n")
	route, err := rt.Route(Request{Brief: b, Access: "edit", Lane: "agy-flash"})
	if err != nil || route.FamilyID != "agy-flash" || !route.Requested {
		t.Fatalf("route=%+v err=%v", route, err)
	}
	if !strings.Contains(route.OverrideWarning, "token-billed") || !strings.Contains(route.ReasonLine, "warning: token-billed") {
		t.Fatalf("warning missing: %q / %q", route.OverrideWarning, route.ReasonLine)
	}
	route, err = rt.Route(Request{Brief: b, Access: "edit"})
	if err != nil || route.FamilyID == "agy-flash" {
		t.Fatalf("automatic route=%+v err=%v", route, err)
	}
	found := false
	for _, x := range route.Excluded {
		if x.Family == "agy-flash" && strings.HasPrefix(x.Rule, "token-billed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("agy-flash not excluded as token-billed: %+v", route.Excluded)
	}
}

func TestRequestedLaneRefusesAccessOrMissingHook(t *testing.T) {
	for _, tc := range []struct {
		lane, access, reason string
	}{
		{"agy-flash", "read", "agy needs its Maestro hook: run maestro adapter install agy"},
		{"agy-flash:high", "edit", "agy needs its Maestro hook: run maestro adapter install agy"},
		{"grok", "read", "grok needs its Maestro hook: run maestro adapter install grok"},
		{"grok:high", "edit", "grok needs its Maestro hook: run maestro adapter install grok"},
	} {
		for _, source := range []string{"brief", "flag", "seat"} {
			t.Run(tc.lane+"/"+tc.access+"/"+source, func(t *testing.T) {
				rt := testRouter(t)
				req := Request{Brief: &brief.Brief{}, Access: tc.access}
				id := strings.Split(tc.lane, ":")[0]
				switch source {
				case "brief":
					req.Brief.Lane = tc.lane
				case "flag":
					req.Lane = tc.lane
				case "seat":
					req.Seat = id
				}
				route, err := rt.Route(req)
				want := fmt.Sprintf("lane %s cannot take %s work: %s (see maestro roster)", id, tc.access, tc.reason)
				if strings.Contains(tc.reason, "needs its Maestro hook") {
					want = fmt.Sprintf("family %s fails pre-flight: %s", id, tc.reason)
				}
				if route != nil || err == nil || err.Error() != want {
					t.Fatalf("route=%+v err=%v, want %s", route, err, want)
				}
			})
		}
	}
}

func TestHookHeldHarnessPreflightRequiresHook(t *testing.T) {
	wrong := map[string]string{
		"agy":  `{"maestro":{"PreToolUse":[{"matcher":"invoke_subagent","hooks":[{"type":"command","command":"maestro hook","timeout":10}]}]}}`,
		"grok": `{"hooks":{"PreToolUse":[{"matcher":"spawn_subagent","hooks":[{"type":"command","command":"maestro hook","timeout":10}]}]}}`,
	}
	for _, tc := range []struct{ harness, family string }{{"agy", "agy-flash"}, {"grok", "grok"}} {
		for _, state := range []string{"missing", "wrong", "ok"} {
			t.Run(tc.harness+"/"+state, func(t *testing.T) {
				rt := testRouter(t)
				home := os.Getenv("HOME")
				if state != "missing" {
					if result := adapter.Install(tc.harness, home, "skill"); result.Err != nil {
						t.Fatal(result.Err)
					}
					if state == "wrong" {
						spec, _ := adapter.Paths(tc.harness, home)
						if err := os.WriteFile(spec.HookFile, []byte(wrong[tc.harness]), 0o644); err != nil {
							t.Fatal(err)
						}
					}
				}
				for _, access := range []string{"read", "edit"} {
					want := fmt.Sprintf("%s needs its Maestro hook: run maestro adapter install %s", tc.harness, tc.harness)
					if state == "ok" {
						want = ""
					}
					if got := rt.Preflight(rt.Roster.Families[tc.family], access); got != want {
						t.Fatalf("%s: %q, want %q", access, got, want)
					}
				}
			})
		}
	}
}

func TestPreflightReportsDisabledHookReason(t *testing.T) {
	for _, tc := range []struct{ harness, family string }{{"agy", "agy-flash"}, {"grok", "grok"}} {
		t.Run(tc.harness, func(t *testing.T) {
			rt := testRouter(t)
			home := os.Getenv("HOME")
			if result := adapter.Install(tc.harness, home, "skill"); result.Err != nil {
				t.Fatal(result.Err)
			}
			spec, _ := adapter.Paths(tc.harness, home)
			path := filepath.Join(os.Getenv("GROK_HOME"), "disabled-hooks")
			content := []byte("maestro.json\n")
			if tc.harness == "agy" {
				path = spec.HookFile
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var root map[string]map[string]any
				if err := json.Unmarshal(data, &root); err != nil {
					t.Fatal(err)
				}
				root["maestro"]["enabled"] = false
				content, err = json.Marshal(root)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, content, 0o644); err != nil {
				t.Fatal(err)
			}
			state := adapter.State(tc.harness, home)
			if state.Hook != "disabled" || !strings.Contains(state.HookReason, path) {
				t.Fatal(state)
			}
			for _, access := range []string{"read", "edit"} {
				want := tc.harness + " hook is disabled: " + state.HookReason
				if got := rt.Preflight(rt.Roster.Families[tc.family], access); got != want {
					t.Fatalf("%s: %q, want %q", access, got, want)
				}
			}
			if got := strings.Contains(state.HookReason, "maestro adapter install"); got != (tc.harness == "agy") {
				t.Fatal(state.HookReason)
			}
		})
	}
}

func TestBriefLaneIsRequestedAndFlagWins(t *testing.T) {
	rt := testRouter(t)
	b := &brief.Brief{Fields: brief.Fields{Lane: "codex-sol:high"}}
	route, err := rt.Route(Request{Brief: b, Access: "read"})
	if err != nil || !route.Requested || route.FamilyID != "codex-sol" || route.Effort != "high" {
		t.Fatalf("Brief Lane: %+v, %v", route, err)
	}
	b.Lane = "agy-flash"
	route, err = rt.Route(Request{Brief: b, Access: "read", Lane: "codex-sol"})
	if err != nil || !route.Requested || route.FamilyID != "codex-sol" {
		t.Fatalf("flag Lane: %+v, %v", route, err)
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
			if result := adapter.Install("grok", os.Getenv("HOME"), "skill"); result.Err != nil {
				t.Fatal(result.Err)
			}
			rt.Roster.Budget.WarnAt = .8
			account := rt.Roster.Accounts["grok"]
			account.Billing = "metered"
			account.Cap = &config.Cap{USD: 20, Period: "30d"}
			rt.Roster.Accounts["grok"] = account
			for _, row := range []map[string]any{
				{"kind": "dispatch", "ended_at": time.Now(), "route": map[string]any{"account": "grok"}, "budget": map[string]any{"cost_usd": tc.spent}},
				{"kind": "dispatch", "ended_at": time.Now().Add(-31 * 24 * time.Hour), "route": map[string]any{"account": "grok"}, "budget": map[string]any{"cost_usd": 100}},
				{"kind": "dispatch", "ended_at": time.Now(), "route": map[string]any{"account": "other"}, "budget": map[string]any{"cost_usd": 100}},
				{"kind": "run", "ended_at": time.Now(), "route": map[string]any{"account": "grok"}, "budget": map[string]any{"cost_usd": 100}},
			} {
				if err := ledger.Append(row); err != nil {
					t.Fatal(err)
				}
			}
			v := rt.View("grok")
			if v.State != tc.state || v.Tight == nil || v.Tight.Used != tc.spent/20 {
				t.Fatalf("view %+v for spend %v", v, tc.spent)
			}
			b := &brief.Brief{}
			_, err := rt.Route(Request{Brief: b, Access: "read", Lane: "grok"})
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
	r, err = rt.Route(Request{Brief: b, Access: "read", Lane: "codex-sol"})
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
