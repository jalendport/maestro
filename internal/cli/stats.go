package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jalendport/maestro/internal/ledger"
	"github.com/spf13/cobra"
)

type routerStats struct {
	Routes    int            `json:"routes"`
	BySource  map[string]int `json:"by_source"`
	Agreed    int            `json:"agreed"`
	Compared  int            `json:"compared"`
	Fallback  int            `json:"fallback"`
	Medium    int            `json:"medium_confidence"`
	Verified  map[string]int `json:"first_try_verified"`
	Attempted map[string]int `json:"first_try_attempted"`
	Escalated map[string]int `json:"escalated"`
	VerAgreed [2]int         `json:"-"`
	VerDisagr [2]int         `json:"-"`
}

func (a *app) routerCmd() *cobra.Command {
	c := &cobra.Command{Use: "router", Short: "Router facts"}
	c.AddCommand(&cobra.Command{
		Use:   "stats",
		Short: "Jev vs rules agreement, verified rates per Route source",
		RunE: func(_ *cobra.Command, _ []string) error {
			st := routerStats{BySource: map[string]int{}, Verified: map[string]int{}, Attempted: map[string]int{}, Escalated: map[string]int{}}
			err := ledger.Each(func(raw json.RawMessage) error {
				var row struct {
					Kind    string `json:"kind"`
					Attempt int    `json:"attempt"`
					Route   struct {
						Source    string `json:"route_source"`
						Band      string `json:"confidence_band"`
						Agreed    bool   `json:"agreed"`
						RulesPick string `json:"rules_pick"`
					} `json:"route"`
					Outcome struct {
						Outcome string `json:"outcome"`
					} `json:"outcome"`
				}
				if json.Unmarshal(raw, &row) != nil || row.Kind != "dispatch" {
					return nil
				}
				st.Routes++
				st.BySource[row.Route.Source]++
				if row.Route.Band == "fallback" {
					st.Fallback++
				}
				if row.Route.Band == "medium" {
					st.Medium++
				}
				if row.Route.Source == "jev" && row.Route.RulesPick != "" {
					st.Compared++
					if row.Route.Agreed {
						st.Agreed++
					}
				}
				if row.Attempt == 1 {
					st.Attempted[row.Route.Source]++
					ok := row.Outcome.Outcome == "verified" || row.Outcome.Outcome == "answered"
					if ok {
						st.Verified[row.Route.Source]++
					}
					if row.Route.Source == "jev" {
						idx := &st.VerDisagr
						if row.Route.Agreed {
							idx = &st.VerAgreed
						}
						idx[1]++
						if ok {
							idx[0]++
						}
					}
				}
				if row.Outcome.Outcome == "escalated" {
					st.Escalated[row.Route.Source]++
				}
				return nil
			})
			if err != nil {
				return err
			}
			if a.json {
				b, _ := json.MarshalIndent(st, "", "  ")
				fmt.Fprintln(os.Stdout, string(b))
				return nil
			}
			fmt.Fprintf(os.Stdout, "routes %d   jev %d  rules %d  requested %d\n", st.Routes, st.BySource["jev"], st.BySource["rules"], st.BySource["requested"])
			fmt.Fprintf(os.Stdout, "jev vs rules table agreement  %s\n", ratio(st.Agreed, st.Compared))
			fmt.Fprintf(os.Stdout, "first-try verified   jev %s (agreed %s, disagreed %s)   rules %s   requested %s\n",
				ratio(st.Verified["jev"], st.Attempted["jev"]), ratio(st.VerAgreed[0], st.VerAgreed[1]), ratio(st.VerDisagr[0], st.VerDisagr[1]),
				ratio(st.Verified["rules"], st.Attempted["rules"]), ratio(st.Verified["requested"], st.Attempted["requested"]))
			fmt.Fprintf(os.Stdout, "escalations          jev %s   rules %s   requested %s\n",
				ratio(st.Escalated["jev"], st.Attempted["jev"]), ratio(st.Escalated["rules"], st.Attempted["rules"]), ratio(st.Escalated["requested"], st.Attempted["requested"]))
			fmt.Fprintf(os.Stdout, "medium confidence    %s     fallback %s\n", ratio(st.Medium, st.Routes), ratio(st.Fallback, st.Routes))
			return nil
		},
	})
	return c
}

func ratio(n, d int) string {
	if d == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f", float64(n)/float64(d))
}
