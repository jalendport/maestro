// Package cli assembles the maestro command tree. Human output goes to
// stdout, progress to stderr; exit 0 ok, 1 failed, 2 needs the Orchestrator.
package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/solo"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/spf13/cobra"
)

type app struct {
	version string
	json    bool
	quiet   bool
}

// Execute is the binary entry point.
func Execute(version string) int {
	a := &app{version: version}
	root := a.build()
	if err := root.Execute(); err != nil {
		if code, ok := proc.Code(err); ok {
			if err.Error() != "" {
				ui.Errorf("%v", err)
			}
			return code
		}
		ui.Errorf("%v", err)
		return proc.CodeFailed
	}
	return proc.CodeOK
}

func (a *app) build() *cobra.Command {
	root := &cobra.Command{
		Use:           "maestro",
		Version:       a.version,
		Short:         "routes, dispatches, checks, and reviews agent work",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			ui.Quiet = a.quiet
			return nil
		},
	}
	root.SetVersionTemplate("maestro {{.Version}}\n")
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().BoolVar(&a.json, "json", false, "machine output: exactly one JSON object")
	root.PersistentFlags().BoolVar(&a.quiet, "quiet", false, "reason lines and progress only, on stderr")
	root.SetHelpFunc(func(c *cobra.Command, _ []string) {
		if c == root {
			a.renderHelp()
			return
		}
		fmt.Fprintln(os.Stdout, c.Long)
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, c.UsageString())
	})

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.result(map[string]any{"version": a.version}, "maestro %s", a.version)
		},
	})
	root.AddCommand(a.runCmd())
	root.AddCommand(a.dispatchCmd())
	root.AddCommand(a.waitCmd())
	root.AddCommand(a.statusCmd())
	root.AddCommand(a.addCmd())
	root.AddCommand(a.cancelCmd())
	root.AddCommand(a.approveCmd())
	root.AddCommand(a.resumeCmd())
	root.AddCommand(a.cleanCmd())
	root.AddCommand(a.runSupervisorCmd())
	root.AddCommand(a.councilCmd())
	root.AddCommand(a.answerCmd())
	root.AddCommand(a.relayCmd())
	root.AddCommand(a.hookCmd())
	root.AddCommand(a.initCmd())
	root.AddCommand(a.adapterCmd())
	root.AddCommand(a.tellCmd())
	root.AddCommand(a.councilSupervisorCmd())
	root.AddCommand(a.rosterCmd())
	root.AddCommand(a.doctorCmd())
	root.AddCommand(a.helpTopicCmd())
	root.AddCommand(a.keyCmd())
	root.AddCommand(a.budgetCmd())
	root.AddCommand(a.routerCmd())
	root.AddCommand(a.superviseCmd())
	return root
}

// env loads everything a Dispatch reads. refresh forces a discovery refresh;
// otherwise a stale cache is refreshed on the way.
func (a *app) env(refresh bool) (*dispatch.Env, error) {
	return a.loadEnv(refresh, true)
}

func refreshSkill(_ *cobra.Command, _ []string) error {
	_, err := adapter.EnsureSkill(SkillMD)
	return err
}

func (a *app) loadEnv(refresh, persist bool) (*dispatch.Env, error) {
	r, err := roster.Load()
	if err != nil {
		return nil, err
	}
	for _, w := range r.Warnings {
		ui.Warnf("%s", w)
	}
	if err := r.ValidateAccounts(); err != nil {
		return nil, err
	}
	bins := map[string]string{}
	for name, h := range r.Harness {
		bins[name] = h.Bin
	}
	reg := harness.New(bins)
	disc, err := discovery.Load()
	if err != nil {
		return nil, err
	}
	if refresh || disc.Stale() {
		if persist {
			ui.Eventf("roster", "refreshing the discovery cache")
		}
		if persist {
			disc, err = discovery.Refresh(reg)
		} else {
			disc = discovery.Scan(reg)
		}
		if err != nil {
			return nil, err
		}
	}
	cwd, _ := os.Getwd()
	root := config.FindProjectRoot(cwd)
	if root == "" {
		return nil, fmt.Errorf("the home directory cannot be a project root; change to a project folder")
	}
	project, err := config.LoadProject(root)
	if err != nil {
		return nil, err
	}
	bf, err := budget.Load()
	if err != nil {
		return nil, err
	}
	env := &dispatch.Env{Roster: r, Disc: disc, Reg: reg, Project: project, Root: root, Budget: bf}
	if solo.InSolo() {
		env.Solo = dispatch.ConnectSolo()
		if env.Solo != nil && (refresh || soloToolsMissing(disc)) {
			recordSoloTools(env, persist)
		}
	}
	return env, nil
}

func (a *app) renderHelp() {
	out := os.Stdout
	ui.Fprintln(out, ui.Header.Render("maestro")+" "+ui.Dim.Render("— routes, dispatches, checks, and reviews agent work"))
	ui.Fprintln(out, "")
	ui.Fprintln(out, ui.Section.Render("Work"))
	ui.Fprintln(out, "  "+ui.Name.Render("run <plan.md>")+"            Run every Task of a Plan file (one live Run per project)")
	ui.Fprintln(out, "  "+ui.Name.Render("dispatch <brief.md>")+"      Route and dispatch one Task (returns at once; --wait to block)")
	ui.Fprintln(out, "  "+ui.Name.Render("council <question>")+"       Ask several Lanes the same question in parallel")
	ui.Fprintln(out, "  "+ui.Name.Render("wait <run|task|council>")+"  Block until a Run, Task, or Council ends; print its report")
	ui.Fprintln(out, "  "+ui.Name.Render("status [run|task|council]")+" Show Runs, Dispatches, and Councils")
	ui.Fprintln(out, "")
	ui.Fprintln(out, ui.Section.Render("Mid-run"))
	ui.Fprintln(out, "  "+ui.Name.Render("add <brief.md>")+"           Add a Task to the live Run")
	ui.Fprintln(out, "  "+ui.Name.Render("cancel <task|run>")+"        Cancel a Task or a Run (running Dispatches are stopped)")
	ui.Fprintln(out, "  "+ui.Name.Render("tell <task> <text>")+"       Send a note into a running Lane (Solo tab only)")
	ui.Fprintln(out, "  "+ui.Name.Render("answer <task> allow|deny")+" Answer a relayed permission prompt")
	ui.Fprintln(out, "  "+ui.Name.Render("approve <run>")+"            Approve a breakdown that was held for approval")
	ui.Fprintln(out, "  "+ui.Name.Render("resume <run> --task <n>")+"  (--lane F[:E] | --brief <file> | --skip)  Continue a paused Run")
	ui.Fprintln(out, "  "+ui.Name.Render("clean [--older-than 30d]")+" Remove old Run and Council folders (never the ledger)")
	ui.Fprintln(out, "")
	ui.Fprintln(out, ui.Section.Render("Setup and facts"))
	ui.Fprintln(out, "  "+ui.Name.Render("doctor")+"                   Check Harnesses, versions, logins, the Jev key, and Solo")
	ui.Fprintln(out, "  "+ui.Name.Render("roster")+"                   Show the effective Roster (families, models, levels, state)")
	ui.Fprintln(out, "    "+ui.Name.Render("roster refresh")+"         Re-read the Harness catalogs")
	ui.Fprintln(out, "    "+ui.Name.Render("roster prices")+"          Show the embedded price table")
	ui.Fprintln(out, "    "+ui.Name.Render("roster solo")+"            Print Solo preset command lines to type in by hand")
	ui.Fprintln(out, "  "+ui.Name.Render("budget")+"                   Show Budget readings per Account")
	ui.Fprintln(out, "    "+ui.Name.Render("budget set <account> <percent>")+"   Record a manual reading")
	ui.Fprintln(out, "    "+ui.Name.Render("budget record claude")+"   Read a Claude statusline JSON on stdin (statusline tee)")
	ui.Fprintln(out, "    "+ui.Name.Render("budget reset codex")+"     Spend one Codex reset credit (manual only)")
	ui.Fprintln(out, "  "+ui.Name.Render("router stats")+"             Jev vs rules agreement, verified rates per Route source")
	ui.Fprintln(out, "  "+ui.Name.Render("key set")+"                  Store the Jev key in ~/.maestro/env (no echo)")
	ui.Fprintln(out, "  "+ui.Name.Render("init")+"                     Write the project file .agents/maestro.toml, which puts the project in hook scope")
	ui.Fprintln(out, "  "+ui.Name.Render("adapter [harness]")+"        Print the Adapter install lines (skill file, hook file, reminder)")
	ui.Fprintln(out, "    "+ui.Name.Render("adapter install [harness]")+" Install the Adapter skill, link, and hook")
	ui.Fprintln(out, "  "+ui.Name.Render("hook [harness]")+"           Hook entry point for PreToolUse and reminder events (called by the Adapters)")
	ui.Fprintln(out, "  "+ui.Name.Render("help brief|plan|report")+"   Print the Brief file, Plan file, or Report formats")
	ui.Fprintln(out, "")
	ui.Fprintln(out, ui.Section.Render("Route flags")+ui.Dim.Render("  (run, dispatch, council)"))
	ui.Fprintln(out, "  --lane <family[:effort]>   Request a Lane (warns when it breaks a rule)")
	ui.Fprintln(out, "  --floor <tier>             At least this Tier        --ceiling <tier>  At most this Tier (run, dispatch)")
	ui.Fprintln(out, "  --level <L>                Judgment level estimate: none | some | significant | expert")
	ui.Fprintln(out, "")
	ui.Fprintln(out, ui.Section.Render("Output flags"))
	ui.Fprintln(out, "  --json                     Machine output (one object)    --quiet  progress only, on stderr")
	ui.Fprintln(out, "")
}

// result prints the verb's one result object or its human line.
func (a *app) result(value any, format string, args ...any) error {
	if a.json {
		return json.NewEncoder(os.Stdout).Encode(value)
	}
	ui.Outf(format, args...)
	return nil
}

// soloToolsMissing also refreshes old caches that did not record preset choices.
func soloToolsMissing(disc *discovery.Cache) bool {
	for _, rec := range disc.Harness {
		if len(rec.SoloPresets) > 0 {
			return false
		}
	}
	return true
}

// recordSoloTools maps Solo's agent tools onto Harness names in the cache.
func recordSoloTools(env *dispatch.Env, persist bool) {
	res, err := env.Solo.Client.Call("list_agent_tools", nil)
	if err != nil {
		return
	}
	var tools []discovery.SoloAgentTool
	if json.Unmarshal(res, &tools) != nil {
		return
	}
	if env.Disc.RecordSoloTools(tools) && persist {
		_ = env.Disc.Save()
	}
}
