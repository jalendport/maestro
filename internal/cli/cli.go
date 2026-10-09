// Package cli assembles the maestro command tree. Human output goes to
// stdout, progress to stderr; exit 0 ok, 1 failed, 2 needs the Orchestrator.
package cli

import (
	"fmt"
	"os"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/roster"
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
		Short:         "routes, dispatches, checks, and reviews agent work",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			ui.Quiet = a.quiet || a.json
		},
	}
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
			fmt.Fprintln(os.Stdout, "maestro "+a.version)
			return nil
		},
	})
	root.AddCommand(a.dispatchCmd())
	root.AddCommand(a.waitCmd())
	root.AddCommand(a.rosterCmd())
	root.AddCommand(a.doctorCmd())
	root.AddCommand(a.helpTopicCmd())
	root.AddCommand(a.keyCmd())
	root.AddCommand(a.routerCmd())
	root.AddCommand(a.superviseCmd())
	return root
}

// env loads everything a Dispatch reads. refresh forces a discovery refresh;
// otherwise a stale cache is refreshed on the way.
func (a *app) env(refresh bool) (*dispatch.Env, error) {
	r, err := roster.Load()
	if err != nil {
		return nil, err
	}
	for _, w := range r.Warnings {
		ui.Warnf("%s", w)
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
		ui.Eventf("roster", "refreshing the discovery cache")
		disc, err = discovery.Refresh(reg)
		if err != nil {
			return nil, err
		}
	}
	cwd, _ := os.Getwd()
	root := config.FindProjectRoot(cwd)
	project, err := config.LoadProject(root)
	if err != nil {
		return nil, err
	}
	return &dispatch.Env{Roster: r, Disc: disc, Reg: reg, Project: project, Root: root}, nil
}

func (a *app) renderHelp() {
	out := os.Stdout
	ui.Fprintln(out, ui.Header.Render("maestro")+" "+ui.Dim.Render("— routes, dispatches, checks, and reviews agent work"))
	ui.Fprintln(out, "")
	ui.Fprintln(out, ui.Section.Render("Work"))
	ui.Fprintln(out, "  "+ui.Name.Render("dispatch <brief.md>")+"      Route and dispatch one Task (returns at once; --wait to block)")
	ui.Fprintln(out, "  "+ui.Name.Render("wait <task>")+"              Block until a Task ends; print its report")
	ui.Fprintln(out, "")
	ui.Fprintln(out, ui.Section.Render("Setup and facts"))
	ui.Fprintln(out, "  "+ui.Name.Render("doctor")+"                   Check Harnesses, versions, logins, the Jev key, and Solo")
	ui.Fprintln(out, "  "+ui.Name.Render("roster")+"                   Show the effective Roster (families, models, levels, state)")
	ui.Fprintln(out, "    "+ui.Name.Render("roster refresh")+"         Re-read the Harness catalogs")
	ui.Fprintln(out, "    "+ui.Name.Render("roster prices")+"          Show the embedded price table")
	ui.Fprintln(out, "  "+ui.Name.Render("router stats")+"             Jev vs rules agreement, verified rates per Route source")
	ui.Fprintln(out, "  "+ui.Name.Render("key set")+"                  Store the Jev key in ~/.maestro/env (no echo)")
	ui.Fprintln(out, "  "+ui.Name.Render("help brief|report")+"        Print the Brief file format or the Report shapes")
	ui.Fprintln(out, "")
	ui.Fprintln(out, ui.Section.Render("Route flags")+ui.Dim.Render("  (dispatch)"))
	ui.Fprintln(out, "  --lane <family[:effort]>   Request a Lane (warns when it breaks a rule)")
	ui.Fprintln(out, "  --floor <tier>             At least this Tier        --ceiling <tier>  At most this Tier")
	ui.Fprintln(out, "  --level <L>                Judgment level estimate: none | some | significant | expert")
	ui.Fprintln(out, "")
	ui.Fprintln(out, ui.Section.Render("Output flags"))
	ui.Fprintln(out, "  --json                     Machine output (one object)    --quiet  progress only, on stderr")
	ui.Fprintln(out, "")
	ui.Fprintln(out, ui.Dim.Render("Not in this build yet: run, council, status, budget, hook, roster stats|solo."))
}
