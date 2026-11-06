package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/config"
)

func TestHookReminderStaleAdapters(t *testing.T) {
	const skill = "current skill\n"
	previous := SkillMD
	SkillMD = skill
	t.Cleanup(func() { SkillMD = previous })
	for _, tc := range []struct {
		name        string
		installed   []string
		skill, grok bool
		want        string
	}{
		{name: "not installed"},
		{name: "matching", installed: []string{"claude", "grok"}},
		{name: "skill differs", installed: []string{"claude", "grok"}, skill: true, want: "claude, grok"},
		{name: "Codex skill differs", installed: []string{"codex"}, skill: true, want: "codex"},
		{name: "Grok skill differs", installed: []string{"grok"}, skill: true, want: "grok"},
		{name: "Grok rules differ", installed: []string{"claude", "grok"}, grok: true, want: "grok"},
		{name: "both differ", installed: []string{"claude", "grok"}, skill: true, grok: true, want: "claude, grok"},
		{name: "all skills differ", installed: adapter.Harnesses, skill: true, want: "claude, codex, grok, agy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
			t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
			t.Setenv("GROK_HOME", filepath.Join(home, "grok"))
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("MAESTRO_TASK_FOLDER", "")
			t.Setenv("MAESTRO_LANE", "")
			if err := os.WriteFile(config.UserFile(), []byte("[hook]\nscope = \"all\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, harness := range tc.installed {
				if result := adapter.Install(harness, home, skill); result.Err != nil {
					t.Fatal(result.Err)
				}
			}
			if tc.skill {
				if err := os.WriteFile(adapter.SkillFile(), []byte("old skill\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.grok {
				if err := os.WriteFile(adapter.ReminderFile(home), []byte("old rules\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			root := t.TempDir()
			check := func(names string) {
				t.Helper()
				want := adapter.Reminder
				if names != "" {
					want += "\nAdapter stale (" + names + "): run maestro adapter install"
				}
				for _, harness := range []string{"claude", "codex", "agy"} {
					output := runHookPayload(t, reminderPayload(t, harness, root), harness)
					if got := reminderText(t, harness, output); got != want {
						t.Fatalf("%s: Reminder = %q, want %q", harness, got, want)
					}
				}
			}
			check(tc.want)
			for _, harness := range tc.installed {
				if result := adapter.Install(harness, home, skill); result.Err != nil {
					t.Fatal(result.Err)
				}
			}
			check("")
		})
	}
}
