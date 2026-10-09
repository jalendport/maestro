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
	if f.Account != "chatgpt-pro" {
		t.Fatalf("codex-sol bound to %q", f.Account)
	}
	if got := f.Tiers(); len(got) != 2 || got[0] != TierDaily || got[1] != TierHeavy {
		t.Fatalf("codex-sol tiers %v", got)
	}
	if !r.Families["claude-fable"].RequestOnly {
		t.Fatal("fable must be request only")
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
