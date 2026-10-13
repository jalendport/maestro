package router

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
)

func TestJevClampsFinalPick(t *testing.T) {
	for _, tc := range []struct {
		name, pick, access   string
		score                int
		floor, ceiling, want roster.Tier
	}{
		{name: "below floor", pick: "codex-sol", score: 1, access: "read", floor: roster.TierHeavy, want: roster.TierHeavy},
		{name: "above ceiling", pick: "codex-sol", score: 3, access: "read", ceiling: roster.TierDaily, want: roster.TierDaily},
		{name: "light edit", pick: "codex-luna", score: 0, access: "edit", want: roster.TierDaily},
		{name: "frontier family above ceiling", pick: "claude-fable", score: 3, access: "read", ceiling: roster.TierDaily, want: roster.TierDaily},
		{name: "frontier escalation", pick: "codex-sol", score: 1, access: "read", floor: roster.TierFrontier, want: roster.TierFrontier},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := testRouter(t)
			t.Setenv("TYPESAFE_API_KEY", "test-key")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"answers":{"lane":{"choice":%q,"confidence":0.95},"judgment_level":{"score":%d},"spec_is_concrete":{"noul":1}}}`, tc.pick, tc.score)
			}))
			defer server.Close()
			rt.Roster.Router.Backend = "jev"
			rt.Roster.Router.BaseURL = server.URL
			b := &brief.Brief{Fields: brief.Fields{Level: "none"}}
			route, err := rt.Route(Request{Brief: b, Access: tc.access, Floor: tc.floor, Ceiling: tc.ceiling})
			if err != nil {
				t.Fatal(err)
			}
			if route.Source != "jev" || route.Tier != tc.want || !strings.Contains(route.ReasonLine, "clamped to allowed Tier") {
				t.Fatalf("%+v", route)
			}
		})
	}
}

func TestRulesEscalateHeavyThroughFrontierChain(t *testing.T) {
	rt := testRouter(t)
	req := Request{Brief: &brief.Brief{Fields: brief.Fields{Level: "significant", Category: "implement"}}, Access: "edit"}
	first, err := rt.Route(req)
	if err != nil || first.Tier != roster.TierHeavy {
		t.Fatalf("%+v %v", first, err)
	}
	req.Floor = roster.Tiers[first.Tier.Rank()+1]
	req.Excluded = []string{first.FamilyID}
	next, err := rt.Route(req)
	if err != nil || next.FamilyID != "codex-astra" || next.Tier != roster.TierFrontier || next.TieBreak != "chain implement" {
		t.Fatalf("%+v %v", next, err)
	}
}

func TestFanOutNeedsExplicitLaneAndEffort(t *testing.T) {
	for _, effort := range []string{"ultra", "max", "ultracode"} {
		for _, backend := range []string{"rules", "jev"} {
			t.Run(effort+"/"+backend, func(t *testing.T) {
				rt := testRouter(t)
				rt.Roster.Families["codex-sol"].Levels[roster.LevelSome] = effort
				if backend == "jev" {
					t.Setenv("TYPESAFE_API_KEY", "test-key")
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						io.WriteString(w, `{"answers":{"lane":{"choice":"codex-sol","confidence":0.95},"judgment_level":{"score":1},"spec_is_concrete":{"noul":1}}}`)
					}))
					defer server.Close()
					rt.Roster.Router.Backend = "jev"
					rt.Roster.Router.BaseURL = server.URL
				}
				req := Request{Brief: &brief.Brief{Fields: brief.Fields{Level: "some"}}, Access: "edit"}
				route, err := rt.Route(req)
				if err != nil || roster.FanOut(route.Effort) {
					t.Fatalf("%+v %v", route, err)
				}
				found := false
				for _, e := range route.Excluded {
					if e.Family == "codex-sol" && strings.Contains(e.Rule, "fan-out effort "+effort) {
						found = true
					}
				}
				if !found {
					t.Fatal("missing exclusion", route.Excluded)
				}
				req.Lane = "codex-sol"
				req.Access = "read"
				if _, err = rt.Route(req); err == nil {
					t.Fatal("implicit fan-out effort accepted")
				}
				req.Lane = "codex-sol:" + effort
				route, err = rt.Route(req)
				if err != nil || route.Effort != effort {
					t.Fatalf("%+v %v", route, err)
				}
			})
		}
	}
}

func TestUserAliasChoosesLaunchModel(t *testing.T) {
	rt := testRouter(t)
	defaults, _, _ := config.ParseFile(roster.Defaults)
	user, _, _ := config.ParseFile("[aliases]\nsonnet = 'claude-opus-5-5'\n")
	rt.Roster = roster.Merge(defaults, user)
	req := Request{Brief: &brief.Brief{}, Access: "read", Lane: "claude-sonnet"}
	route, err := rt.Route(req)
	if err != nil {
		t.Fatal(err)
	}
	if route.Model != "claude-opus-5-5" || rt.Roster.Aliases["sonnet"] != route.Model {
		t.Fatalf("%+v", route)
	}
	h, _ := rt.Reg.Get("claude")
	command, err := h.Build(harness.Launch{Access: "read", Model: route.Model, Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(command.Cmd.Args, " "), "--model claude-opus-5-5") {
		t.Fatal(command.Cmd.Args)
	}
	rt.Roster = roster.Merge(defaults, &config.File{})
	route, err = rt.Route(req)
	if err != nil || route.Model != "sonnet" {
		t.Fatalf("%+v %v", route, err)
	}
}

func TestPreflightNoModel(t *testing.T) {
	rt := testRouter(t)
	f := &roster.Family{ID: "empty", Harness: "codex"}
	if why := rt.Preflight(f, "read"); !strings.Contains(why, "no model") {
		t.Fatal(why)
	}
}
