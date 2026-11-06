package roster

import (
	"testing"

	"github.com/jalendport/maestro/internal/config"
)

func TestCommitTemplateAndIdentities(t *testing.T) {
	defaults, _, err := config.ParseFile(Defaults)
	if err != nil {
		t.Fatal(err)
	}
	user, unknown, err := config.ParseFile("[commit]\nmessage = \"{run}: {title}\\n\\n{summary}\\n{lane}\"\n[commit.identities]\ncodex = \"Custom <custom@example.test>\"\n[run]\nworktrees = false\n")
	if err != nil || len(unknown) > 0 {
		t.Fatalf("config: %v, %v", unknown, err)
	}
	r := Merge(defaults, &config.File{})
	if r.CommitTemplate() != "{title}\n\nCo-Authored-By: {lane}" || !r.Worktrees() {
		t.Fatalf("defaults: %+v", r.Commit)
	}
	r = Merge(defaults, user)
	if r.CommitTemplate() != "{run}: {title}\n\n{summary}\n{lane}" || r.Identity("codex-sol", "gpt-6.1-sol") != "Custom <custom@example.test>" || r.Worktrees() {
		t.Fatalf("overrides: %+v", r.Commit)
	}
	if r.Identity("claude-opus", "claude-opus-4-8") != "Claude Opus 4.8 <noreply@anthropic.com>" {
		t.Fatal("identity override changed another Harness")
	}
	empty := ""
	r = Merge(defaults, &config.File{Commit: config.Commit{Message: &empty}})
	if r.CommitTemplate() != "" {
		t.Fatal("an explicit empty message was lost")
	}
}
