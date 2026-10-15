package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/hook"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/spf13/cobra"
)

func (a *app) hookCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "hook [claude|codex|grok|agy]",
		Short:  "Hook Carrier entry point for PreToolUse and Reminder events (called by the Adapters)",
		Long:   "Read the Harness's PreToolUse or Reminder event payload on stdin. Apply the scoped spawn rule and Antigravity Lane permissions to tool calls. The hook Carrier writes the Reminder for an Orchestrator in scope on UserPromptSubmit (Claude Code and Codex) or PreInvocation (Antigravity). Grok uses a rules file Carrier.",
		Args:   cobra.MaximumNArgs(1),
		Hidden: false,
		RunE: func(_ *cobra.Command, args []string) error {
			harness := ""
			if len(args) == 1 {
				harness = args[0]
			}
			payload, err := io.ReadAll(os.Stdin)
			if err != nil {
				if harness == "agy" {
					fmt.Fprintln(os.Stdout, `{"decision":"allow"}`)
				}
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
			if call.Event == "ignore" {
				return nil
			}
			if call.Event == "reminder" {
				scope := ""
				if r, err := roster.Load(); err == nil {
					scope = r.Hook.Scope
				}
				if out := hook.RenderReminder(call, scope); out != nil {
					fmt.Fprintln(os.Stdout, string(out))
				}
				return nil
			}
			// Antigravity and Grok Lanes are held by this hook: their own
			// permission systems cannot hold the Access on their own.
			if call.Harness == "agy" || call.Harness == "grok" {
				taskFolder, found, err := hook.FindLaneFolder(call)
				if err != nil {
					fmt.Fprintln(os.Stdout, string(hook.Render(call.Harness, hook.Verdict{Kind: "deny", Reason: err.Error()})))
					return nil
				}
				if found {
					folder, err := config.ControlDirForFolder(taskFolder)
					if err != nil {
						fmt.Fprintln(os.Stdout, string(hook.Render(call.Harness, hook.Verdict{Kind: "deny", Reason: err.Error()})))
						return nil
					}
					if lane, ok := hook.LoadLane(folder); ok {
						verdict := hook.DecideLane(call, lane)
						if verdict.Kind == "relay" {
							input, err := json.Marshal(call.Input)
							answer := ""
							if err == nil {
								answer, err = dispatch.RelayWait(folder, dispatch.RelayRequest{ToolName: call.Tool, ToolInput: input})
							}
							verdict = hook.Verdict{Kind: "deny", Reason: answer}
							if err != nil {
								verdict.Reason = err.Error()
							} else if answer == "allow" {
								verdict = hook.Verdict{Kind: "auto_approve"}
							}
						}
						fmt.Fprintln(os.Stdout, string(hook.Render(call.Harness, verdict)))
						return nil
					}
					fmt.Fprintln(os.Stdout, string(hook.Render(call.Harness, hook.Verdict{Kind: "deny", Reason: "no readable Lane policy"})))
					return nil
				}
			}
			decision := hook.Decision{Allow: true}
			if r, err := roster.Load(); err == nil {
				decision = hook.Decide(call, r.Hook.Scope)
			}
			out := hook.Render(call.Harness, decision)
			if out != nil {
				fmt.Fprintln(os.Stdout, string(out))
			}
			return nil
		},
	}
}

// SkillMD is the embedded skill file, set by main.
var SkillMD string

func adapterNames(args []string) ([]string, error) {
	if len(args) == 0 {
		return adapter.Harnesses, nil
	}
	if _, ok := adapter.Spec(args[0]); !ok {
		return nil, &proc.ExitError{Code: proc.CodeFailed, Message: "unknown harness " + args[0]}
	}
	return args, nil
}

func (a *app) adapterCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "adapter [claude|codex|grok|agy]",
		Short: "Print the Adapter install lines (skill file, PreToolUse hook, Reminder Carrier)",
		Long:  "Print the shared skill link, hook file, PreToolUse entry, Reminder Carrier, and next step. The Carrier uses UserPromptSubmit (Claude Code and Codex), PreInvocation (Antigravity), or a rules file (Grok). Use maestro adapter install to do the steps.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			names, err := adapterNames(args)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			r, err := roster.Load()
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, hookScopeLine(r.Hook.Scope))
			fmt.Fprintln(os.Stdout, "maestro adapter prints the steps and writes nothing; maestro adapter install [harness] does them.")
			for _, name := range names {
				spec, _ := adapter.Paths(name, home)
				b, err := adapter.HookJSON(name)
				if err != nil {
					return err
				}
				link := spec.Link
				quote := func(path string) string { return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'" }
				fmt.Fprintf(os.Stdout, "\n%s:\n  mkdir -p %s\n  ln -s %s %s\n  %s\n  %s\n", name, quote(filepath.Dir(link)), quote(adapter.SkillDir()), quote(link), spec.HookFile, b)
				if name == "grok" {
					fmt.Fprintf(os.Stdout, "  %s\n%s", adapter.ReminderFile(home), adapter.ReminderRule())
				}
				fmt.Fprintf(os.Stdout, "  Next: %s\n", spec.Next)
			}
			return nil
		},
	}
	c.AddCommand(a.adapterInstallCmd())
	return c
}

func (a *app) adapterInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install [claude|codex|grok|agy]",
		Short: "Install the Adapter skill, link, PreToolUse hook, and Reminder Carrier",
		Long:  "Install the shared skill, link, PreToolUse hook, and Reminder Carrier per Harness. The Carrier uses UserPromptSubmit (Claude Code and Codex), PreInvocation (Antigravity), or a rules file (Grok).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			names, err := adapterNames(args)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				env, err := a.env(false)
				if err != nil {
					return err
				}
				found := []string{}
				for _, name := range names {
					if env.Disc.Harness[name].Path != "" {
						found = append(found, name)
					}
				}
				names = found
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			r, err := roster.Load()
			if err != nil {
				return err
			}
			if a.json {
				fmt.Fprintln(os.Stderr, hookScopeLine(r.Hook.Scope))
			} else {
				fmt.Fprintln(os.Stdout, hookScopeLine(r.Hook.Scope))
			}
			results := make([]adapter.Result, 0, len(names))
			failed := false
			for _, name := range names {
				result := adapter.Install(name, home, SkillMD)
				results = append(results, result)
				failed = failed || result.Err != nil
				if !a.json {
					for _, action := range result.Actions {
						detail := ""
						if action.State == "error" && action.Detail != "" {
							detail = " " + action.Detail
						}
						ui.Eventf(action.Kind, "%s %s%s", action.State, action.Path, detail)
					}
					ui.Outf("%s Next: %s", name, result.Next)
				}
			}
			if a.json {
				if err := json.NewEncoder(os.Stdout).Encode(struct {
					Results []adapter.Result `json:"results"`
				}{results}); err != nil {
					return err
				}
			}
			if failed {
				return &proc.ExitError{Code: proc.CodeFailed}
			}
			return nil
		},
	}
}

func hookScopeLine(scope string) string {
	if scope == "all" {
		return "Hook scope: all (every directory)."
	}
	return "Hook scope: projects (only inside a project with .agents/maestro.toml)."
}
