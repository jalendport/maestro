package budget

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/ledger"
)

func EvaluateMetered(account string, cap *config.Cap, warnAt float64) View {
	spent := MeteredSpend(account, cap)
	if cap == nil {
		return View{State: "unknown", Headroom: 1, Reason: "no spend cap"}
	}
	share := 0.0
	if cap.USD > 0 {
		share = spent / cap.USD
	}
	v := View{State: "ok", Headroom: max(0, 1-share), Tight: &Window{Kind: "spend", Used: share, ReadAt: time.Now(), Source: SourceComputed}}
	switch {
	case spent >= cap.USD:
		v.State, v.Headroom = "blocked", 0
		v.Reason = fmt.Sprintf("spend $%.2f at cap $%.2f", spent, cap.USD)
	case share > warnAt:
		v.State = "warning"
		v.Reason = fmt.Sprintf("spend $%.2f above %.0f%% of cap", spent, warnAt*100)
	}
	return v
}

// MeteredSpend sums the priced cost of this Account's Dispatches in the period.
func MeteredSpend(account string, cap *config.Cap) float64 {
	period := 30 * 24 * time.Hour
	if cap != nil && cap.Period != "" {
		if d, err := time.ParseDuration(strings.Replace(cap.Period, "d", "h", 1)); err == nil {
			if strings.HasSuffix(cap.Period, "d") {
				d *= 24
			}
			period = d
		}
	}
	since := time.Now().Add(-period)
	total := 0.0
	_ = ledger.Each(func(raw json.RawMessage) error {
		var row struct {
			Kind  string    `json:"kind"`
			Ended time.Time `json:"ended_at"`
			Route struct {
				Account string `json:"account"`
			} `json:"route"`
			Budget struct {
				Cost *float64 `json:"cost_usd"`
			} `json:"budget"`
		}
		if json.Unmarshal(raw, &row) == nil && row.Kind == "dispatch" && row.Route.Account == account && row.Ended.After(since) && row.Budget.Cost != nil {
			total += *row.Budget.Cost
		}
		return nil
	})
	return total
}
