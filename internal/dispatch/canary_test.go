package dispatch

import (
	"testing"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
)

func TestCanaryRouteUsesCheapestEnabledFamily(t *testing.T) {
	for _, name := range []string{"claude", "codex", "grok", "agy"} {
		t.Run(name, func(t *testing.T) {
			families := map[string]*roster.Family{}
			for _, id := range []string{"costly", "cheap", "disabled"} {
				families[id] = &roster.Family{
					ID: id, Harness: name, Models: []string{id}, Enabled: id != "disabled",
					Levels: map[roster.Level]string{roster.LevelNone: "low"},
				}
			}
			env := &Env{
				Reg: harness.New(nil),
				Roster: &roster.Roster{Families: families, Prices: map[string]config.Price{
					"costly": {Input: 10, Output: 20}, "cheap": {Input: 1, Output: 2}, "disabled": {},
				}},
				Disc: &discovery.Cache{Harness: map[string]discovery.Record{name: {Models: []harness.Model{{ID: "costly"}, {ID: "cheap"}, {ID: "disabled"}}}}},
			}
			route, err := canaryRoute(env, name)
			if err != nil || route.FamilyID != "cheap" || route.Effort != "low" {
				t.Fatalf("Canary Route = %+v, error = %v", route, err)
			}
			families["cheap"].Enabled = false
			route, err = canaryRoute(env, name)
			if err != nil || route.FamilyID != "costly" {
				t.Fatalf("Canary Route after disable = %+v, error = %v", route, err)
			}
			families["costly"].Enabled = false
			if _, err := canaryRoute(env, name); err == nil {
				t.Fatal("Canary selected a disabled Family")
			}
		})
	}
}
