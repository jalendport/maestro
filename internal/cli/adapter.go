package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jalendport/maestro/internal/adapter"
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
		Short:  "PreToolUse hook entry point (called by the Adapters)",
		Long:   "Read the Harness's PreToolUse payload on stdin and write its answer on stdout after the scoped spawn rule and Antigravity Lane permissions are applied.",
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
			if folder := os.Getenv("MAESTRO_TASK_FOLDER"); folder != "" && call.Harness == "agy" {
				if lane, ok := hook.LoadLane(folder); ok {
					verdict := hook.DecideLane(call, lane)
					if verdict.Kind == "relay" {
						input, err := json.Marshal(call.Input)
						answer := ""
						if err == nil {
							answer, err = dispatch.RelayWait(folder, dispatch.RelayRequest{ToolName: "run_command", ToolInput: input})
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
		Short: "Print the Adapter install steps per Harness",
		Long:  "Print the shared skill link, hook file, hook entry, and next step. Use maestro adapter install to do the steps.",
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
				entry := map[string]any{
					"matcher": spec.Matcher,
					"hooks":   []any{map[string]any{"type": "command", "command": adapter.Command, "timeout": spec.Timeout}},
				}
				hooks := map[string]any{"PreToolUse": []any{entry}}
				key := "hooks"
				if name == "agy" {
					key = "maestro"
				}
				b, err := json.Marshal(map[string]any{key: hooks})
				if err != nil {
					return err
				}
				link := spec.Link
				quote := func(path string) string { return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'" }
				fmt.Fprintf(os.Stdout, "\n%s:\n  mkdir -p %s\n  ln -s %s %s\n  %s\n  %s\n  Next: %s\n", name, quote(filepath.Dir(link)), quote(adapter.SkillDir()), quote(link), spec.HookFile, b, spec.Next)
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
		Short: "Install the Adapter skill link and hook per Harness",
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
						if action.State == "error" {
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
