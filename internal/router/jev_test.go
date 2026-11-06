package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/roster"
)

func TestJevLevelSetsTier(t *testing.T) {
	for _, tc := range []struct {
		name, estimate, access, pick string
		override                     roster.Level
		score                        int
		confidence                   float64
		floor, ceiling, tier         roster.Tier
		level                        roster.Level
		clamped                      bool
	}{
		{name: "estimate does not raise score", estimate: "expert", access: "edit", pick: "codex-sol", score: 1, level: roster.LevelSome, tier: roster.TierDaily},
		{name: "level flag overrides Brief", estimate: "some", override: roster.LevelExpert, access: "edit", pick: "codex-sol", score: 1, level: roster.LevelSome, tier: roster.TierDaily},
		{name: "medium confidence", estimate: "expert", access: "edit", pick: "codex-sol", score: 1, confidence: 0.63, level: roster.LevelSome, tier: roster.TierDaily},
		{name: "estimate does not lower score", estimate: "some", access: "edit", pick: "codex-sol", score: 3, level: roster.LevelExpert, tier: roster.TierHeavy},
		{name: "light read", estimate: "expert", access: "read", pick: "codex-luna", score: 0, level: roster.LevelNone, tier: roster.TierLight},
		{name: "light edit", estimate: "expert", access: "edit", pick: "codex-luna", score: 0, level: roster.LevelSome, tier: roster.TierDaily, clamped: true},
		{name: "heavy floor", estimate: "expert", access: "edit", pick: "codex-sol", score: 1, floor: roster.TierHeavy, level: roster.LevelSignificant, tier: roster.TierHeavy, clamped: true},
		{name: "daily ceiling", estimate: "expert", access: "edit", pick: "codex-sol", score: 3, ceiling: roster.TierDaily, level: roster.LevelSome, tier: roster.TierDaily, clamped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := testRouter(t)
			t.Setenv("TYPESAFE_API_KEY", "test-key")
			confidence := tc.confidence
			if confidence == 0 {
				confidence = 0.95
			}
			states := make(chan map[string]any, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					State struct {
						Task map[string]any `json:"task"`
					} `json:"state"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("route state: %v", err)
				}
				states <- payload.State.Task
				fmt.Fprintf(w, `{"answers":{"lane":{"choice":%q,"confidence":%g},"judgment_level":{"score":%d},"spec_is_concrete":{"noul":1}}}`, tc.pick, confidence, tc.score)
			}))
			defer server.Close()
			rt.Roster.Router.Backend = "jev"
			rt.Roster.Router.BaseURL = server.URL
			b := &brief.Brief{Fields: brief.Fields{Level: tc.estimate, Category: "implement"}}
			route, err := rt.Route(Request{Brief: b, Access: tc.access, Floor: tc.floor, Ceiling: tc.ceiling, Level: tc.override})
			if err != nil {
				t.Fatal(err)
			}
			estimate := tc.estimate
			if tc.override != "" {
				estimate = string(tc.override)
			}
			if route.Source != "jev" || route.Level != tc.level || route.Tier != tc.tier || route.LevelEstimate != roster.Level(estimate) {
				t.Fatalf("Route: %+v", route)
			}
			if !tc.clamped && route.FamilyID != tc.pick {
				t.Fatalf("Jev picked %s; Route uses %s", tc.pick, route.FamilyID)
			}
			if got := strings.Contains(strings.Join(route.DefaultsUsed, "; "), "clamped"); got != tc.clamped {
				t.Fatalf("clamped=%t; defaults: %v", got, route.DefaultsUsed)
			}
			score := levelFromScore(float64(tc.score))
			if score.Rank() < roster.Level(estimate).Rank() {
				want := fmt.Sprintf("jev %s, orchestrator estimated %s", score, estimate)
				if !strings.Contains(route.ReasonLine, want) {
					t.Fatalf("Reason line lacks %q: %s", want, route.ReasonLine)
				}
			} else if strings.Contains(route.ReasonLine, "orchestrator estimated") {
				t.Fatalf("Reason line has no lower Jev score: %s", route.ReasonLine)
			}
			if state := <-states; state["orchestrator_estimate"] != estimate {
				t.Fatalf("route state: %+v", state)
			}
			if tc.name == "estimate does not raise score" && !route.GapFlag {
				t.Fatalf("estimate and score gap lost: %+v", route)
			}
		})
	}
}

func TestRulesFallbackUsesBriefLevel(t *testing.T) {
	for _, mode := range []string{"rules", "no key", "low confidence", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			rt := testRouter(t)
			calls := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls <- struct{}{}
				if mode == "unavailable" {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				fmt.Fprint(w, `{"answers":{"lane":{"choice":"codex-sol","confidence":0.20},"judgment_level":{"score":1},"spec_is_concrete":{"noul":1}}}`)
			}))
			defer server.Close()
			rt.Roster.Router.BaseURL = server.URL
			if mode != "rules" {
				rt.Roster.Router.Backend = "jev"
			}
			if mode != "no key" {
				t.Setenv("TYPESAFE_API_KEY", "test-key")
			}
			b := &brief.Brief{Fields: brief.Fields{Level: "expert", Category: "implement"}}
			route, err := rt.Route(Request{Brief: b, Access: brief.AccessEdit})
			if err != nil {
				t.Fatal(err)
			}
			if route.Source != "rules" || route.Level != roster.LevelExpert || route.LevelEstimate != roster.LevelExpert || route.Tier != roster.TierHeavy || route.FamilyID != "codex-sol" || route.Effort != "xhigh" {
				t.Fatalf("Route: %+v", route)
			}
			wantCalls := 1
			if mode == "rules" || mode == "no key" {
				wantCalls = 0
			}
			if len(calls) != wantCalls {
				t.Fatalf("Jev calls: %d, want %d", len(calls), wantCalls)
			}
		})
	}
}
