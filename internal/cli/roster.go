package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/spf13/cobra"
)

func (a *app) rosterCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "roster",
		Short: "Show the effective Roster",
		Long:  "Print the merge of the embedded defaults, the user file, and the discovery cache: one row per family with its resolved model and pre-flight state.",
		RunE: func(_ *cobra.Command, _ []string) error {
			env, err := a.env(false)
			if err != nil {
				return err
			}
			return printRoster(env, a.json)
		},
	}
	c.AddCommand(&cobra.Command{
		Use:   "refresh",
		Short: "Re-read the Harness catalogs",
		RunE: func(_ *cobra.Command, _ []string) error {
			env, err := a.env(true)
			if err != nil {
				return err
			}
			reportDiscovery(env)
			return printRoster(env, a.json)
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "solo",
		Short: "Print Solo preset command lines to type in by hand",
		RunE: func(_ *cobra.Command, _ []string) error {
			env, err := a.env(false)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, "Solo → Settings → Agents. One preset per Harness; Maestro passes model, effort, and permission flags per launch in extra_args.")
			fmt.Fprintln(os.Stdout)
			for _, name := range env.Reg.Names() {
				h, _ := env.Reg.Get(name)
				known := soloPresetNote(env, name)
				toolType := map[string]string{"claude": "claude", "codex": "codex", "agy": "antigravity", "grok": "generic"}[name]
				fmt.Fprintf(os.Stdout, "  %-6s  Name: %s   Command: %s   Tool type: %s   Default arguments: (none)   — %s\n", name, strings.ToUpper(name[:1])+name[1:], h.Bin(), toolType, known)
			}
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "prices",
		Short: "Show the embedded price table",
		RunE: func(_ *cobra.Command, _ []string) error {
			r, err := roster.Load()
			if err != nil {
				return err
			}
			if a.json {
				b, _ := json.MarshalIndent(r.Prices, "", "  ")
				fmt.Fprintln(os.Stdout, string(b))
				return nil
			}
			ids := make([]string, 0, len(r.Prices))
			for id := range r.Prices {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "model\tinput\tcached input\toutput\t(USD per 1M tokens)")
			for _, id := range ids {
				p := r.Prices[id]
				fmt.Fprintf(w, "%s\t%.2f\t%.3f\t%.2f\t\n", id, p.Input, p.CachedInput, p.Output)
			}
			return w.Flush()
		},
	})
	return c
}

type rosterRow struct {
	Family   string            `json:"family"`
	Harness  string            `json:"harness"`
	Models   []string          `json:"models"`
	Resolved string            `json:"resolved_model"`
	Levels   map[string]string `json:"levels"`
	Account  string            `json:"account"`
	Billing  string            `json:"billing"`
	Enabled  bool              `json:"enabled"`
	State    string            `json:"state"`
}

func rosterRows(env *dispatch.Env) []rosterRow {
	rt := env.NewRouter()
	var rows []rosterRow
	for _, f := range env.Roster.SortedFamilies() {
		row := rosterRow{Family: f.ID, Harness: f.Harness, Models: f.Models, Account: f.Account, Enabled: f.Enabled, Levels: map[string]string{}}
		for l, e := range f.Levels {
			row.Levels[string(l)] = e
		}
		row.Billing = env.Roster.Accounts[f.Account].Billing
		resolved, ok := env.Disc.Resolve(f.Harness, env.Roster.LaunchModels(f))
		if ok {
			row.Resolved = resolved
		} else if len(f.Models) == 0 {
			row.Resolved = "no model"
		} else {
			row.Resolved = f.Models[0] + " (missing)"
		}
		access := "edit"
		h, _ := env.Reg.Get(f.Harness)
		if h != nil && h.Enforcement("edit") == harness.NoShape {
			access = "read"
		}
		var state []string
		switch {
		case !f.Enabled:
			state = append(state, "disabled")
		case rt.Preflight(f, access) != "":
			state = append(state, "pre-flight: "+rt.Preflight(f, access))
		default:
			state = append(state, "ok")
		}
		if f.RequestOnly {
			state = append(state, "request only")
		}
		if row.Billing == "metered" {
			state = append(state, "metered")
		}
		if h != nil && h.Enforcement("edit") == harness.NoShape {
			state = append(state, "read only")
		}
		if h != nil && h.Enforcement("read") == harness.BestEffort {
			state = append(state, "read: best-effort")
		}
		row.State = strings.Join(state, "  ")
		rows = append(rows, row)
	}
	return rows
}

func printRoster(env *dispatch.Env, asJSON bool) error {
	rows := rosterRows(env)
	if asJSON {
		b, _ := json.MarshalIndent(map[string]any{"families": rows, "cache": env.Disc}, "", "  ")
		fmt.Fprintln(os.Stdout, string(b))
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "family\tharness\tmodel (resolved)\tlevels\taccount\tstate")
	for _, r := range rows {
		var levels []string
		for _, l := range roster.Levels {
			if e, ok := r.Levels[string(l)]; ok {
				levels = append(levels, fmt.Sprintf("%s=%s", l, e))
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Family, r.Harness, r.Resolved, strings.Join(levels, " "), r.Account, r.State)
	}
	w.Flush()
	fmt.Fprintf(os.Stdout, "cache  %s  refreshed %s\n", config.DiscoveryFile(), ago(env.Disc.RefreshedAt))
	return nil
}

// reportDiscovery prints what the catalogs showed: unmapped models and
// family models missing from the catalog.
func reportDiscovery(env *dispatch.Env) {
	mapped := map[string]bool{}
	for _, f := range env.Roster.Families {
		for _, m := range f.Models {
			mapped[f.Harness+"/"+m] = true
		}
	}
	for _, name := range env.Reg.Names() {
		rec := env.Disc.Harness[name]
		if rec.Error != "" {
			ui.Eventf("catalog", "%-7s %s", name, rec.Error)
			continue
		}
		var unmapped []string
		for _, m := range rec.Models {
			if !mapped[name+"/"+m.ID] {
				unmapped = append(unmapped, m.ID)
			}
		}
		line := fmt.Sprintf("%-7s %s  %d models", name, rec.Version, len(rec.Models))
		if len(unmapped) > 0 {
			line += "  no family: " + strings.Join(unmapped, ", ")
		}
		ui.Eventf("catalog", "%s", line)
	}
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t).Round(time.Minute)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func soloPresetNote(env *dispatch.Env, name string) string {
	if id := env.Roster.Harness[name].SoloTool; id != 0 {
		return fmt.Sprintf("agent tool id %d (user file)", id)
	}
	rec := env.Disc.Harness[name]
	if len(rec.SoloPresets) > 1 {
		var choices []string
		for _, preset := range rec.SoloPresets {
			choices = append(choices, fmt.Sprintf("%s (%d)", preset.Name, preset.ID))
		}
		return fmt.Sprintf("several presets: %s; set [harness.%s] solo_tool = <preset id> in %s", strings.Join(choices, ", "), name, config.UserFile())
	}
	if rec.SoloTool != 0 {
		return fmt.Sprintf("agent tool id %d", rec.SoloTool)
	}
	return "not found in Solo"
}
