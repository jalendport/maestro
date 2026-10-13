package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/spf13/cobra"
)

func (a *app) budgetCmd() *cobra.Command {
	var refresh bool
	c := &cobra.Command{
		Use:   "budget",
		Short: "Show Budget readings per Account",
		Long:  "One row per Account and window: used share, headroom, reserve, reset time, reading age and source, and the on-pace note (display only).",
		RunE: func(_ *cobra.Command, _ []string) error {
			env, err := a.env(false)
			if err != nil {
				return err
			}
			if refresh {
				env.RefreshBudget()
			}
			return a.printBudget(env)
		},
	}
	c.Flags().BoolVar(&refresh, "refresh", false, "probe stale Accounts first (Claude Haiku probe, Codex app-server)")
	c.AddCommand(&cobra.Command{
		Use:   "set <account> <percent>",
		Short: "Record a manual reading (lasts until the window resets)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			env, err := a.env(false)
			if err != nil {
				return err
			}
			if _, ok := env.Roster.Accounts[args[0]]; !ok {
				return fmt.Errorf("unknown account %q (see maestro roster)", args[0])
			}
			pct, err := strconv.ParseFloat(strings.TrimSuffix(args[1], "%"), 64)
			if err != nil || pct < 0 || pct > 100 {
				return fmt.Errorf("percent must be 0 to 100")
			}
			env.Budget.RecordManual(args[0], pct/100)
			if err := env.Budget.Save(); err != nil {
				return err
			}
			ui.Successf("%s recorded at %.0f%% used (manual)", args[0], pct)
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "record claude",
		Short: "Read a Claude statusline JSON on stdin and store its windows",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if args[0] != "claude" {
				return fmt.Errorf("only claude has a statusline tee")
			}
			env, err := a.env(false)
			if err != nil {
				return err
			}
			ws, err := budget.StatuslineWindows(bufio.NewReader(os.Stdin))
			if err != nil {
				return err
			}
			id := accountFor(env, "claude")
			env.Budget.Record(id, ws, budget.SourceHarvest)
			return env.Budget.Save()
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "reset codex",
		Short: "Spend one Codex reset credit (asks first)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if args[0] != "codex" {
				return fmt.Errorf("only codex has reset credits")
			}
			env, err := a.env(false)
			if err != nil {
				return err
			}
			h, _ := env.Reg.Get("codex")
			cl, err := budget.ReadCodex(h.Bin())
			if err != nil {
				return err
			}
			if cl.Credits == 0 {
				return fmt.Errorf("no reset credit available")
			}
			fmt.Fprintf(os.Stderr, "Spend one of %d Codex reset credits now? [y/N] ", cl.Credits)
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.TrimSpace(strings.ToLower(line)) != "y" {
				ui.Outf("kept")
				return nil
			}
			if err := budget.ConsumeCodexReset(h.Bin()); err != nil {
				return err
			}
			if cl, err = budget.ReadCodex(h.Bin()); err == nil {
				id := accountFor(env, "codex")
				env.Budget.Record(id, cl.Windows, budget.SourceProbe)
				env.Budget.SetCredits(id, cl.Credits, cl.Plan)
				env.Budget.Unblock(id)
				_ = env.Budget.Save()
			}
			ui.Successf("reset credit spent; %d left", cl.Credits)
			return nil
		},
	})
	return c
}

// accountFor returns the single Account of a Harness.
func accountFor(env *dispatch.Env, harnessName string) string {
	for id, acct := range env.Roster.Accounts {
		if acct.Harness == harnessName {
			return id
		}
	}
	return harnessName
}

type budgetRow struct {
	Account  string `json:"account"`
	Billing  string `json:"billing"`
	Plan     string `json:"plan"`
	Window   string `json:"window"`
	Used     string `json:"used"`
	Headroom string `json:"headroom"`
	Reserve  string `json:"reserve"`
	Resets   string `json:"resets"`
	Read     string `json:"read"`
	Source   string `json:"source"`
	State    string `json:"state"`
	Pace     string `json:"pace"`
}

func (a *app) printBudget(env *dispatch.Env) error {
	rt := env.NewRouter()
	ids := make([]string, 0, len(env.Roster.Accounts))
	for id := range env.Roster.Accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var rows []budgetRow
	now := time.Now()
	for _, id := range ids {
		acct := env.Roster.Accounts[id]
		v := rt.View(id)
		plan := "—"
		if acct.PlanPrice > 0 {
			plan = fmt.Sprintf("$%.0f", acct.PlanPrice)
		}
		if acct.Billing == "metered" {
			spent := budget.MeteredSpend(id, acct.Cap)
			period, headroom := "—", "—"
			if acct.Cap != nil {
				period = acct.Cap.Period
				headroom = fmt.Sprintf("$%.2f of $%.0f", acct.Cap.USD-spent, acct.Cap.USD)
			}
			rows = append(rows, budgetRow{Account: id, Billing: "metered", Plan: "—", Window: period, Used: fmt.Sprintf("$%.2f", spent), Headroom: headroom, Reserve: "—", Resets: "—", Read: "—", Source: "computed", State: v.State, Pace: "—"})
			continue
		}
		rec := env.Budget.Snapshot(id)
		if rec == nil || len(rec.Windows) == 0 {
			rows = append(rows, budgetRow{Account: id, Billing: acct.Billing, Plan: plan, Window: "—", Used: "?", Headroom: fmt.Sprintf("%.2f assumed", env.Roster.Budget.AssumedHeadroom), Reserve: "—", Resets: "—", Read: "—", Source: "unknown", State: v.State})
			continue
		}
		for i, w := range rec.Windows {
			row := budgetRow{Window: budget.WindowName(w), Used: fmt.Sprintf("%.2f", w.Used), Headroom: fmt.Sprintf("%.2f", 1-w.Used), Reserve: "—", Source: w.Source}
			if i == 0 {
				row.Account, row.Billing, row.Plan = id, acct.Billing, plan
				row.State = v.State
				if v.Reason != "" && v.State != "ok" {
					row.State += "  " + v.Reason
				}
				if rec.Credits > 0 {
					row.State += fmt.Sprintf("  %d reset credits", rec.Credits)
				}
			}
			if w.Kind == "weekly" && acct.Reserve > 0 {
				row.Reserve = fmt.Sprintf("%.2f", acct.Reserve)
				row.Headroom = fmt.Sprintf("%.2f", max(0, 1-w.Used-acct.Reserve))
			}
			if !w.ResetsAt.IsZero() {
				row.Resets = "in " + shortDur(w.ResetsAt.Sub(now))
				row.Pace = pace(w, now)
			}
			row.Read = ago(w.ReadAt)
			rows = append(rows, row)
		}
	}
	if a.json {
		b, _ := json.MarshalIndent(map[string]any{"rows": rows, "file": env.Budget}, "", "  ")
		fmt.Fprintln(os.Stdout, string(b))
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "account\tbilling\tplan\twindow\tused\theadroom\treserve\tresets\tread\tsource\tstate\tpace")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Account, r.Billing, r.Plan, r.Window, r.Used, r.Headroom, r.Reserve, r.Resets, r.Read, r.Source, r.State, r.Pace)
	}
	return w.Flush()
}

// pace compares the share used with the share of the window elapsed (display only).
func pace(w budget.Window, now time.Time) string {
	length := w.Length()
	elapsed := length - w.ResetsAt.Sub(now)
	if elapsed <= 0 || length <= 0 {
		return "—"
	}
	share := float64(elapsed) / float64(length)
	if w.Used > share+0.10 {
		return "on pace to run out"
	}
	return "ok"
}

func shortDur(d time.Duration) string {
	if d < 0 {
		return "0m"
	}
	d = d.Round(time.Minute)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
