package roster

import (
	"testing"

	"github.com/jalendport/maestro/internal/config"
)

func TestDefaultsParseAndBind(t *testing.T) {
	d, unknown, err := config.ParseFile(Defaults)
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown keys in defaults: %v", unknown)
	}
	r := Merge(d, &config.File{})
	if len(r.Warnings) != 0 {
		t.Fatalf("warnings on a clean merge: %v", r.Warnings)
	}
	f := r.Families["codex-sol"]
	if f.Account != "codex" {
		t.Fatalf("codex-sol bound to %q", f.Account)
	}
	if got := f.Tiers(); len(got) != 2 || got[0] != TierDaily || got[1] != TierHeavy {
		t.Fatalf("codex-sol tiers %v", got)
	}
	for _, family := range r.Families {
		if family.RequestOnly {
			t.Fatalf("default family %s is request only", family.ID)
		}
	}
	if len(r.Accounts) != 4 {
		t.Fatal(r.Accounts)
	}
	for _, name := range []string{"claude", "codex", "grok", "agy"} {
		account := r.Accounts[name]
		billing := "subscription"
		if name == "agy" {
			billing = "metered"
		}
		if account.Harness != name || account.Billing != billing || account.PlanPrice != 0 || account.Reserve != 0 || account.Cap != nil {
			t.Fatalf("account %s is not neutral: %+v", name, account)
		}
	}
	if r.Hook.Scope != "projects" || r.Router.Orchestrator != "" {
		t.Fatalf("unexpected hook or orchestrator defaults: %+v %+v", r.Hook, r.Router)
	}
	if r.Failure.WallLimit["heavy"].Duration.Minutes() != 60 {
		t.Fatalf("heavy wall limit %v", r.Failure.WallLimit["heavy"])
	}
}

func TestUserDeltaChangesOneKey(t *testing.T) {
	d, _, _ := config.ParseFile(Defaults)
	u, _, err := config.ParseFile(`
[families.grok]
enabled = false

[rules.heavy]
implement = ["claude-opus"]
`)
	if err != nil {
		t.Fatal(err)
	}
	r := Merge(d, u)
	g := r.Families["grok"]
	if g.Enabled {
		t.Fatal("grok still enabled")
	}
	if g.Harness != "grok" || len(g.Models) != 1 {
		t.Fatalf("delta wiped the embedded stanza: %+v", g)
	}
	if c := r.Chain(TierHeavy, "implement"); len(c) != 1 || c[0] != "claude-opus" {
		t.Fatalf("chain override lost: %v", c)
	}
	if c := r.Chain(TierHeavy, "review"); c[0] != "claude-opus" || len(c) != 4 {
		t.Fatalf("untouched chain changed: %v", c)
	}
}

func TestAccountRenameReplacesHarnessDefault(t *testing.T) {
	d, _, _ := config.ParseFile(Defaults)
	u, _, err := config.ParseFile(`[accounts.claude-pro]
harness = "claude"
plan_price = 20
`)
	if err != nil {
		t.Fatal(err)
	}
	r := Merge(d, u)
	if _, exists := r.Accounts["claude"]; exists {
		t.Fatal("embedded account retained")
	}
	if len(r.Accounts) != len(d.Accounts) {
		t.Fatal(r.Accounts)
	}
	for _, family := range r.Families {
		if family.Harness == "claude" && family.Account != "claude-pro" {
			t.Fatalf("family %s bound to %s", family.ID, family.Account)
		}
	}
	if err := r.ValidateAccounts(); err != nil {
		t.Fatal(err)
	}
}

func TestAccountPriceOverrideKeepsOtherFields(t *testing.T) {
	d, _, _ := config.ParseFile(Defaults)
	u, _, err := config.ParseFile(`[accounts.claude]
plan_price = 20
[accounts.agy]
plan_price = 10
`)
	if err != nil {
		t.Fatal(err)
	}
	r := Merge(d, u)
	claude := r.Accounts["claude"]
	if claude.PlanPrice != 20 || claude.Reserve != d.Accounts["claude"].Reserve || claude.Harness != "claude" || claude.BlockAt != .9 || claude.BudgetSource != "harvest+probe" {
		t.Fatalf("lost defaults: %+v", claude)
	}
	agy := r.Accounts["agy"]
	if agy.PlanPrice != 10 || agy.Billing != "metered" || agy.Cap != nil {
		t.Fatalf("lost metered defaults: %+v", agy)
	}
}

func TestAccountExplicitZeroAndFalseOverrideDefaults(t *testing.T) {
	d, _, _ := config.ParseFile(`[accounts.claude]
harness = "claude"
billing = "subscription"
reserve = 0.35
plan_price = 200
block_at = 0.9
paid_overflow = true
base_url = "https://example.test"
budget = { usd = 20, period = "30d" }
`)
	u, _, err := config.ParseFile(`[accounts.claude]
reserve = 0
plan_price = 0
block_at = 0
paid_overflow = false
base_url = ""
budget = { usd = 0 }
`)
	if err != nil {
		t.Fatal(err)
	}
	account := Merge(d, u).Accounts["claude"]
	if account.Reserve != 0 || account.PlanPrice != 0 || account.BlockAt != 0 || account.PaidOverflow || account.BaseURL != "" || account.Cap.USD != 0 || account.Cap.Period != "30d" || account.Harness != "claude" {
		t.Fatalf("explicit values lost: %+v", account)
	}
	if d.Accounts["claude"].Cap.USD != 20 {
		t.Fatal("merge changed defaults")
	}
}

func TestOrchestratorAndHookMerge(t *testing.T) {
	d, _, err := config.ParseFile(Defaults)
	if err != nil {
		t.Fatal(err)
	}
	u, unknown, err := config.ParseFile(`[hook]
scope = "all"
[router]
orchestrator = "codex-sol"
[families.codex-sol]
request_only = false
`)
	if err != nil || len(unknown) != 0 {
		t.Fatalf("parse: %v, unknown: %v", err, unknown)
	}
	r := Merge(d, u)
	if r.Hook.Scope != "all" || r.Router.Orchestrator != "codex-sol" || !r.Families["codex-sol"].RequestOnly {
		t.Fatalf("settings lost: %+v %+v %+v", r.Hook, r.Router, r.Families["codex-sol"])
	}
}
