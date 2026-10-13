package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/hook"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/spf13/cobra"
)

func (a *app) hookCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "hook [claude|codex|grok|agy]",
		Short:  "PreToolUse hook entry point (called by the Adapters)",
		Long:   "Read the Harness's PreToolUse payload on stdin and write its answer on stdout: read-only sub-agent spawns pass; code-writing sub-agent and Solo spawns are denied with \"use maestro dispatch\".",
		Args:   cobra.MaximumNArgs(1),
		Hidden: false,
		RunE: func(_ *cobra.Command, args []string) error {
			harness := ""
			if len(args) == 1 {
				harness = args[0]
			}
			payload, err := io.ReadAll(os.Stdin)
			if err != nil {
				return nil
			}
			call, err := hook.Parse(harness, payload)
			if err != nil {
				// Never block a Harness on a payload Maestro cannot read.
				if harness == "grok" || harness == "agy" {
					fmt.Fprintln(os.Stdout, `{"decision":"allow"}`)
				}
				return nil
			}
			out := hook.Render(call.Harness, hook.Decide(call))
			if out != nil {
				fmt.Fprintln(os.Stdout, string(out))
			}
			return nil
		},
	}
}

// adapterText is the install recipe per Harness: a skill symlink and a hook file.
func adapterText(name, repo string) string {
	skill := filepath.Join(repo, "skills", "maestro")
	switch name {
	case "claude":
		return fmt.Sprintf(`claude — plugin (skill + hook; no binary inside):
  claude plugin marketplace add jalendport/maestro
  claude plugin install maestro@jalendport
  or, without the plugin: ln -s %s ~/.claude/skills/maestro and add to ~/.claude/settings.json:
  %s
`, skill, hookJSON("maestro hook claude", "Agent|Task|mcp__solo__spawn_agent|mcp__solo__spawn_process"))
	case "codex":
		return fmt.Sprintf(`codex — skill symlink + ~/.codex/hooks.json (needs a one-time trust review in /hooks):
  mkdir -p ~/.agents/skills && ln -s %s ~/.agents/skills/maestro
  ~/.codex/hooks.json:
  %s
`, skill, hookJSON("maestro hook codex", "Agent|mcp__solo__spawn_agent|mcp__solo__spawn_process"))
	case "grok":
		return fmt.Sprintf(`grok — reads ~/.claude/skills already; hook file ~/.grok/hooks/maestro.json (set timeout; hooks fail open):
  {"hooks":{"PreToolUse":[{"matcher":"spawn_subagent|Task|solo__spawn_agent|solo__spawn_process","hooks":[{"type":"command","command":"maestro hook grok","timeout":10}]}]}}
  and in ~/.grok/config.toml: [compat.claude] hooks = false
`)
	case "agy":
		return fmt.Sprintf(`agy — skill symlink + a named entry in ~/.gemini/config/hooks.json:
  ln -s %s ~/.gemini/antigravity-cli/skills/maestro
  {"maestro":{"PreToolUse":[{"matcher":"invoke_subagent","hooks":[{"type":"command","command":"maestro hook agy","timeout":30}]}]}}
`, skill)
	}
	return ""
}

func hookJSON(command, matcher string) string {
	return fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":%q,"hooks":[{"type":"command","command":%q,"timeout":10}]}]}}`, matcher, command)
}

// SkillMD is the embedded skill file, set by main.
var SkillMD string

func (a *app) adapterCmd() *cobra.Command {
	var writeSkill bool
	c := &cobra.Command{
		Use:   "adapter [claude|codex|grok|agy]",
		Short: "Print the Adapter install lines per Harness (skill file + hook file)",
		Long:  "Maestro never writes a Harness's settings files. This prints what to add: the shared skill file and one PreToolUse hook that calls maestro hook <harness>. --write-skill installs the skill file under ~/.maestro/skills/maestro/ so the symlinks have a target.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			repo := repoRoot()
			if writeSkill {
				dir := filepath.Join(config.Home(), "skills", "maestro")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(SkillMD), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "skill written to %s\n\n", filepath.Join(dir, "SKILL.md"))
				repo = config.Home()
			} else if _, err := os.Stat(filepath.Join(config.Home(), "skills", "maestro", "SKILL.md")); err == nil {
				repo = config.Home()
			}
			names := []string{"claude", "codex", "grok", "agy"}
			if len(args) == 1 {
				names = []string{args[0]}
			}
			for _, n := range names {
				t := adapterText(n, repo)
				if t == "" {
					return &proc.ExitError{Code: proc.CodeFailed, Message: "unknown harness " + n}
				}
				fmt.Fprintln(os.Stdout, t)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&writeSkill, "write-skill", false, "install the embedded skill file under ~/.maestro/skills/maestro/")
	return c
}

// repoRoot guesses where the skill file lives: beside the binary's source
// checkout when run from it, else the Homebrew share folder.
func repoRoot() string {
	cwd, _ := os.Getwd()
	for d := cwd; d != "/" && d != ""; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "skills", "maestro", "SKILL.md")); err == nil {
			return d
		}
	}
	if exe, err := os.Executable(); err == nil {
		share := filepath.Join(filepath.Dir(filepath.Dir(exe)), "share", "maestro")
		if _, err := os.Stat(filepath.Join(share, "skills", "maestro", "SKILL.md")); err == nil {
			return share
		}
	}
	return "<run: maestro adapter --write-skill>"
}

// adapterState reports which Adapters are in place on this machine (doctor).
func adapterState() string {
	home, _ := os.UserHomeDir()
	var parts []string
	check := func(name, path, needle string) {
		b, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(b), needle) {
			parts = append(parts, name+" ok")
			return
		}
		parts = append(parts, name+" missing")
	}
	claudeHook := filepath.Join(home, ".claude", "settings.json")
	if claudePluginInstalled(home) {
		parts = append(parts, "claude ok (plugin)")
	} else {
		check("claude", claudeHook, "maestro hook")
	}
	check("codex", filepath.Join(home, ".codex", "hooks.json"), "maestro hook")
	check("grok", filepath.Join(home, ".grok", "hooks", "maestro.json"), "maestro hook")
	check("agy", filepath.Join(home, ".gemini", "config", "hooks.json"), "maestro hook")
	return strings.Join(parts, ", ")
}

// claudePluginInstalled reports whether the maestro plugin is in Claude Code's
// installed-plugins registry, or sits under ~/.claude/plugins in any layout.
func claudePluginInstalled(home string) bool {
	registry := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	if b, err := os.ReadFile(registry); err == nil && strings.Contains(string(b), `"maestro@`) {
		return true
	}
	for _, pattern := range []string{
		filepath.Join(home, ".claude", "plugins", "*", "maestro*"),
		filepath.Join(home, ".claude", "plugins", "cache", "*", "maestro*"),
	} {
		if entries, err := filepath.Glob(pattern); err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}
