package router

import (
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
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

func TestReadExcludesGrokAndFable(t *testing.T) {
	rt := testRouter(t)
	b := mustBrief(t, "---\ntitle: x\ncategory: research\nlevel: some\n---\n## Goal\ng\n## Done\nd\n")
	r, err := rt.Route(Request{Brief: b, Access: "read"})
	if err != nil {
		t.Fatal(err)
	}
	if r.FamilyID == "grok" || r.FamilyID == "claude-fable" {
		t.Fatalf("read went to %s", r.FamilyID)
	}
	for _, e := range r.Excluded {
		if e.Family == "grok" && e.Rule != "read needs an enforced lane" && !strings.HasPrefix(e.Rule, "pre-flight") {
			t.Fatalf("grok excluded for %q", e.Rule)
		}
	}
}

func TestRequestedLaneWarns(t *testing.T) {
	rt := testRouter(t)
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
