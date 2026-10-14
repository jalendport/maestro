// Package router makes Routes: it applies the hard rules in code, then asks
// Jev, with the rules table as fallback, to pick a family, and prints
// one reason line per Route. Jev never sees the hard rules as words.
package router

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/jev"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/ui"
)

// Request is everything a Route needs.
type Request struct {
	Brief    *brief.Brief
	Access   string
	Lane     string // --lane family[:effort]
	Floor    roster.Tier
	Ceiling  roster.Tier
	Level    roster.Level // --level, beats the Brief's estimate
	Project  *config.Project
	Excluded []string // families removed for this Task (failed attempts)
	InSolo   bool
	// Seat pins a Council Seat: family id and effort, Route source "council".
	Seat       string
	SeatEffort string
}

// Exclusion records why a family left the list.
type Exclusion struct {
	Family string `json:"family"`
	Rule   string `json:"rule"`
}

// Route is one decision.
type Route struct {
	Family          *roster.Family `json:"-"`
	FamilyID        string         `json:"family"`
	Harness         string         `json:"harness"`
	Tier            roster.Tier    `json:"tier"`
	LevelEstimate   roster.Level   `json:"level_estimate"`
	Level           roster.Level   `json:"level_score"`
	Effort          string         `json:"effort"`
	Model           string         `json:"model_requested"`
	Account         string         `json:"account"`
	Source          string         `json:"route_source"` // jev | rules | requested
	Confidence      float64        `json:"confidence,omitempty"`
	Band            string         `json:"confidence_band,omitempty"`
	RulesPick       string         `json:"rules_pick"`
	Agreed          bool           `json:"agreed"`
	DefaultsUsed    []string       `json:"defaults_used,omitempty"`
	TieBreak        string         `json:"tie_break,omitempty"`
	ReasonLine      string         `json:"reason_line"`
	Requested       bool           `json:"requested"`
	OverrideWarning string         `json:"override_warning,omitempty"`
	GapFlag         bool           `json:"gap_flag"`
	Category        string         `json:"category"`
	Enforcement     string         `json:"enforcement"`
	Excluded        []Exclusion    `json:"excluded,omitempty"`
	Jev             *JevRoute      `json:"jev,omitempty"`
	Budget          *BudgetNote    `json:"budget,omitempty"`
	BudgetWarning   string         `json:"budget_warning,omitempty"`
}

// AskOrchestrator is returned when no Lane can take the Task.
type AskOrchestrator struct {
	Reason   string
	Excluded []Exclusion
}

func (e *AskOrchestrator) Error() string { return e.Reason }

// Router holds what every Route reads.
type Router struct {
	Roster *roster.Roster
	Disc   *discovery.Cache
	Reg    *harness.Registry
	Budget *budget.File // nil = every Account unknown
}

// BudgetNote is the Budget reading in force at Route time (ledger group Budget).
type BudgetNote struct {
	Account  string  `json:"account"`
	State    string  `json:"state"`
	Headroom float64 `json:"headroom"`
	Used     float64 `json:"used"`
	ReadAt   string  `json:"read_at,omitempty"`
	Source   string  `json:"source"`
	Reason   string  `json:"reason,omitempty"`
}

// View evaluates one Account's Budget with the roster's rules.
func (rt *Router) View(account string) budget.View {
	acct := rt.Roster.Accounts[account]
	if acct.Billing == "metered" {
		return budget.EvaluateMetered(account, acct.Cap, rt.Roster.Budget.WarnAt)
	}
	if rt.Budget == nil {
		return budget.View{Headroom: rt.Roster.Budget.AssumedHeadroom, State: "unknown", Reason: "budget unknown"}
	}
	r := budget.Rules{BlockAt: rt.Roster.Budget.BlockAt, WarnAt: rt.Roster.Budget.WarnAt, StaleAfter: rt.Roster.Budget.StaleAfter.Duration, AssumedHeadroom: rt.Roster.Budget.AssumedHeadroom}
	if acct.BlockAt > 0 {
		r.BlockAt = acct.BlockAt
	}
	r.Reserve = acct.Reserve
	return rt.Budget.Evaluate(account, r, time.Now())
}

// Note turns a View into the ledger's Budget note.
func (rt *Router) Note(account string) *BudgetNote {
	v := rt.View(account)
	n := &BudgetNote{Account: account, State: v.State, Headroom: v.Headroom, Source: "unknown", Reason: v.Reason}
	if v.Tight != nil {
		n.Used = v.Tight.Used
		n.ReadAt = v.Tight.ReadAt.Format(time.RFC3339)
		n.Source = v.Tight.Source
	}
	return n
}

// Weight is plan price times Headroom, or Headroom alone without a plan price.
func (rt *Router) Weight(f *roster.Family) float64 {
	headroom := rt.View(f.Account).Headroom
	if price := rt.Roster.Accounts[f.Account].PlanPrice; price > 0 {
		return price * headroom
	}
	return headroom
}

// Route applies the hard rules, then asks Jev to pick a Family.
// The rules table uses the same candidates when Jev is absent.
func (rt *Router) Route(req Request) (*Route, error) {
	if req.Lane == "" && req.Seat == "" {
		req.Lane = req.Brief.Lane
	}
	category := req.Brief.Category
	var defaults []string
	if category == "" {
		category = "implement"
		defaults = append(defaults, "category=implement")
	}
	estimate, _ := roster.ParseLevel(req.Brief.Level)
	level := estimate
	if req.Level != "" {
		level = req.Level
	}
	if level == "" {
		level = roster.LevelSome
		defaults = append(defaults, "level=some")
	}
	// Hard rule 1: the Light Tier never edits files. A none-level edit goes Daily.
	if level == roster.LevelNone && req.Access == brief.AccessEdit {
		level = roster.LevelSome
		defaults = append(defaults, "edit raises light to daily")
	}
	level = constrainedLevel(req, level)

	if req.Seat != "" {
		f, ok := rt.Roster.Family(req.Seat)
		if !ok {
			return nil, fmt.Errorf("unknown family %q", req.Seat)
		}
		if err := rt.CheckAccess(f, req.Access); err != nil {
			return nil, err
		}
		if why := rt.Preflight(f, req.Access); why != "" {
			return nil, fmt.Errorf("family %s fails pre-flight: %s", f.ID, why)
		}
		if v := rt.View(f.Account); v.State == "blocked" {
			return nil, fmt.Errorf("family %s is blocked by budget: %s", f.ID, v.Reason)
		}
		useLevel := level
		if !f.Serves(useLevel) {
			useLevel = f.NearestLevel(level)
		}
		if roster.FanOut(f.Levels[useLevel]) || roster.FanOut(req.SeatEffort) {
			return nil, fmt.Errorf("fan-out effort requires an explicit Lane and effort")
		}
		r := rt.finish(f, useLevel, estimate, category, req.Access, "council", defaults, nil)
		if req.SeatEffort != "" {
			r.Effort = req.SeatEffort
		}
		r.ReasonLine = reason(r, "council seat")
		return r, nil
	}
	if req.Lane != "" {
		return rt.requested(req, level, estimate, category, defaults)
	}

	cands, excluded := rt.candidates(req, level)
	rulesFamily, tie := rt.rulesPick(cands, level, category, req.Floor)
	useRules := rt.Roster.Router.Backend == "rules" || (req.Project != nil && req.Project.Router == "rules")
	if !useRules {
		key, err := jev.Key()
		useRules = err != nil || key == ""
	}
	if !useRules {
		r, jr, err := rt.jevRoute(req, cands, level, category, defaults)
		if err != nil {
			if ask, ok := err.(*AskOrchestrator); ok {
				ask.Excluded = excluded
			}
			return nil, err
		}
		if r != nil {
			r.Jev = jr
			r.Excluded = excluded
			if rulesFamily != nil {
				r.RulesPick = rulesFamily.ID
			}
			r.Agreed = r.RulesPick == r.FamilyID
			tail := fmt.Sprintf("jev %.2f", r.Confidence)
			if r.Band == "medium" {
				tail += " medium confidence"
			}
			r.ReasonLine = reason(r, tail)
			if !r.Agreed && r.RulesPick != "" {
				r.ReasonLine += "  rules table: " + r.RulesPick
			}
			return r, nil
		}
		// Jev absent or below the floor: the rules table decides.
		if rulesFamily == nil {
			return nil, &AskOrchestrator{Reason: fmt.Sprintf("no lane passes the hard rules for a %s %s task at level %s", req.Access, category, level), Excluded: excluded}
		}
		why := "low confidence"
		if jr != nil && jr.Error != "" {
			why = jr.Error
		}
		r = rt.finish(rulesFamily, level, estimate, category, req.Access, "rules", defaults, excluded)
		r.Jev = jr
		r.RulesPick = rulesFamily.ID
		r.Band = "fallback"
		r.TieBreak = tie
		r.ReasonLine = reason(r, "rules fallback ("+ui.FirstLine(why, 60)+")")
		return r, nil
	}
	if rulesFamily == nil {
		return nil, &AskOrchestrator{Reason: fmt.Sprintf("no lane passes the hard rules for a %s %s task at level %s", req.Access, category, level), Excluded: excluded}
	}
	r := rt.finish(rulesFamily, level, estimate, category, req.Access, "rules", defaults, excluded)
	r.RulesPick = rulesFamily.ID
	r.Agreed = true
	r.TieBreak = tie
	r.ReasonLine = reason(r, "rules table")
	return r, nil
}

// candidates removes disabled, request-only, forbidden, excluded, and
// blocked Families, and those that fail preflight or read Enforcement.
// Metered Families join only for read work when no subscription serves the level.
func (rt *Router) candidates(req Request, level roster.Level) ([]*roster.Family, []Exclusion) {
	var out []*roster.Family
	var excluded []Exclusion
	forbidden := map[string]bool{}
	if req.Project != nil {
		for _, f := range req.Project.Forbid {
			forbidden[f] = true
		}
	}
	removed := map[string]bool{}
	for _, f := range req.Excluded {
		removed[f] = true
	}
	var subscription, metered []*roster.Family
	for _, f := range rt.Roster.SortedFamilies() {
		if !f.Enabled {
			continue
		}
		if removed[f.ID] {
			excluded = append(excluded, Exclusion{f.ID, "failed earlier attempt"})
			continue
		}
		if f.RequestOnly {
			excluded = append(excluded, Exclusion{f.ID, "request only"})
			continue
		}
		if forbidden[f.ID] {
			excluded = append(excluded, Exclusion{f.ID, "forbidden by the project file"})
			continue
		}
		if why := rt.Preflight(f, req.Access); why != "" {
			excluded = append(excluded, Exclusion{f.ID, "pre-flight: " + why})
			continue
		}
		if req.Access == brief.AccessRead {
			h, _ := rt.Reg.Get(f.Harness)
			if h.Enforcement("read") != harness.Enforced {
				excluded = append(excluded, Exclusion{f.ID, "read needs an enforced lane"})
				continue
			}
		}
		// Hard rule 3: an Account at or past its block point leaves the list.
		if v := rt.View(f.Account); v.State == "blocked" {
			excluded = append(excluded, Exclusion{f.ID, "budget: " + v.Reason})
			continue
		}
		// Keep only levels allowed for an automatic Route. Do not change the Roster.
		allowed := *f
		allowed.Levels = map[roster.Level]string{}
		for _, l := range roster.Levels {
			effort, serves := f.Levels[l]
			if !serves {
				continue
			}
			if roster.FanOut(effort) {
				excluded = append(excluded, Exclusion{f.ID, "fan-out effort " + effort + " at level " + string(l) + " requires an explicit Lane and effort"})
				continue
			}
			if !allowedTier(req, f.TierFor(l)) {
				continue
			}
			allowed.Levels[l] = effort
		}
		if len(allowed.Levels) == 0 {
			excluded = append(excluded, Exclusion{f.ID, "no level in the allowed Tier range"})
			continue
		}
		f = &allowed
		if rt.Roster.Accounts[f.Account].Billing == "metered" {
			metered = append(metered, f)
			continue
		}
		subscription = append(subscription, f)
	}
	out = subscription
	// Rule 8: a token-billed Lane joins an automatic Route only when the Task
	// is read-only and no subscription Lane serves the level. For edit work,
	// request it by lane; the Route then carries a warning.
	servesLevel := false
	for _, f := range subscription {
		if f.Serves(level) {
			servesLevel = true
			break
		}
	}
	for _, f := range metered {
		if req.Access == brief.AccessRead && !servesLevel {
			out = append(out, f)
		} else {
			excluded = append(excluded, Exclusion{f.ID, "token-billed: subscriptions first; request it by lane"})
		}
	}
	return out, excluded
}

// Preflight is hard rule 4: installed, version gate, model in the catalog,
// and a launch shape for this Access. Empty means the family passes.
func (rt *Router) Preflight(f *roster.Family, access string) string {
	if len(f.Models) == 0 {
		return "family " + f.ID + " has no model"
	}
	if err := rt.Roster.ValidateAccounts(); err != nil {
		return err.Error()
	}
	h, ok := rt.Reg.Get(f.Harness)
	if !ok {
		return "unknown harness " + f.Harness
	}
	rec, ok := rt.Disc.Harness[f.Harness]
	if !ok || rec.Path == "" {
		return f.Harness + " is not installed"
	}
	if f.Harness == "agy" {
		home, err := os.UserHomeDir()
		if err != nil || adapter.State("agy", home).Hook != "ok" {
			return "agy needs its Maestro hook: run maestro adapter install agy"
		}
	}
	if min := rt.Roster.Harness[f.Harness].MinVersion; min != "" && rec.Version != "" && harness.VersionLess(rec.Version, min) {
		return fmt.Sprintf("needs %s ≥ %s (have %s)", f.Harness, min, rec.Version)
	}
	model, ok := rt.Disc.Resolve(f.Harness, rt.Roster.LaunchModels(f))
	if !ok {
		return fmt.Sprintf("%s not in the %s catalog", strings.Join(f.Models, "/"), f.Harness)
	}
	if entry, found := rt.Disc.Model(f.Harness, model); found && len(entry.Efforts) > 0 {
		for _, level := range roster.Levels {
			if effort, set := f.Levels[level]; set && !slices.Contains(entry.Efforts, effort) {
				return fmt.Sprintf("family %s effort %q is not supported by %s (supported: %s)", f.ID, effort, model, strings.Join(entry.Efforts, ", "))
			}
		}
	}
	if h.Enforcement(access) == harness.NoShape {
		return fmt.Sprintf("%s has no %s shape", f.Harness, access)
	}
	if _, err := h.Build(harness.Launch{Model: model, Access: access, Root: "."}); err != nil {
		return err.Error()
	}
	return ""
}

// rulesPick walks the Tier's chain for the category and takes the first
// candidate that serves the level; then any candidate in the Tier.
func (rt *Router) rulesPick(cands []*roster.Family, level roster.Level, category string, floor roster.Tier) (*roster.Family, string) {
	byID := map[string]*roster.Family{}
	for _, f := range cands {
		byID[f.ID] = f
	}
	tier := roster.TierOf(level)
	if floor.Rank() > tier.Rank() {
		tier = floor
	}
	for _, id := range rt.Roster.Chain(tier, category) {
		if f, ok := byID[id]; ok && f.Serves(level) {
			return f, "chain " + category
		}
	}
	var rest []*roster.Family
	for _, f := range cands {
		if f.Serves(level) {
			rest = append(rest, f)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].ID < rest[j].ID })
	if len(rest) > 0 {
		return rest[0], "first candidate off the chain"
	}
	return nil, ""
}

// CheckAccess refuses a requested Lane whose shape cannot hold the Access.
func (rt *Router) CheckAccess(f *roster.Family, access string) error {
	h, ok := rt.Reg.Get(f.Harness)
	if !ok {
		return fmt.Errorf("unknown harness %s", f.Harness)
	}
	switch h.Enforcement(access) {
	case harness.NoShape:
		return fmt.Errorf("lane %s cannot take %s work: has no %s shape (see maestro roster)", f.ID, access, access)
	case harness.BestEffort:
		return fmt.Errorf("lane %s cannot take %s work: its %s shape is best-effort (see maestro roster)", f.ID, access, access)
	}
	return nil
}

// requested honours --lane, except for Access, Budget, and dead or disabled
// Lanes, with a one-line warning when it breaks another rule.
func (rt *Router) requested(req Request, level, estimate roster.Level, category string, defaults []string) (*Route, error) {
	id, effort := req.Lane, ""
	if i := strings.IndexByte(id, ':'); i >= 0 {
		id, effort = id[:i], id[i+1:]
	}
	f, ok := rt.Roster.Family(id)
	if !ok {
		return nil, fmt.Errorf("unknown family %q (see maestro roster)", id)
	}
	if !f.Enabled {
		return nil, fmt.Errorf("family %s is disabled", id)
	}
	if err := rt.CheckAccess(f, req.Access); err != nil {
		return nil, err
	}
	if why := rt.Preflight(f, req.Access); why != "" {
		return nil, fmt.Errorf("family %s fails pre-flight: %s", id, why)
	}
	if v := rt.View(f.Account); v.State == "blocked" {
		return nil, fmt.Errorf("family %s is blocked by budget: %s", id, v.Reason)
	}
	var warnings []string
	if f.RequestOnly {
		warnings = append(warnings, "request-only family")
	}
	// A token-billed Account takes edit work only on request: the Route goes
	// ahead with a warning, so the orchestrator spends tokens on purpose.
	if rt.Roster.Accounts[f.Account].Billing == "metered" && req.Access == brief.AccessEdit {
		warnings = append(warnings, "token-billed account on edit work; subscriptions are preferred")
	}
	if req.Project != nil {
		for _, x := range req.Project.Forbid {
			if x == id {
				warnings = append(warnings, "forbidden by the project file")
			}
		}
	}
	useLevel := level
	if !f.Serves(useLevel) {
		useLevel = f.NearestLevel(level)
		if useLevel != level {
			warnings = append(warnings, fmt.Sprintf("family serves level %s, not %s", useLevel, level))
		}
	}
	if useLevel == "" {
		return nil, fmt.Errorf("family %s serves no level", id)
	}
	if effort == "" && roster.FanOut(f.Levels[useLevel]) {
		return nil, fmt.Errorf("family %s fan-out effort %s requires an explicit Lane and effort", id, f.Levels[useLevel])
	}
	r := rt.finish(f, useLevel, estimate, category, req.Access, "requested", defaults, nil)
	if effort != "" {
		switch effort {
		case "max", "ultra", "ultracode":
			warnings = append(warnings, "fan-out effort "+effort)
		}
		r.Effort = effort
	}
	r.Requested = true
	r.OverrideWarning = strings.Join(warnings, "; ")
	r.ReasonLine = reason(r, "requested")
	return r, nil
}

func (rt *Router) finish(f *roster.Family, level, estimate roster.Level, category, access, source string, defaults []string, excluded []Exclusion) *Route {
	model, _ := rt.Disc.Resolve(f.Harness, rt.Roster.LaunchModels(f))
	h, _ := rt.Reg.Get(f.Harness)
	r := &Route{
		Family: f, FamilyID: f.ID, Harness: f.Harness, Tier: f.TierFor(level), LevelEstimate: estimate, Level: level,
		Effort: f.Levels[level], Model: model, Account: f.Account, Source: source,
		DefaultsUsed: defaults, Category: category, Excluded: excluded, Enforcement: h.Enforcement(access),
	}
	if estimate != "" && abs(estimate.Rank()-level.Rank()) >= 2 {
		r.GapFlag = true
	}
	r.Budget = rt.Note(f.Account)
	switch r.Budget.State {
	case "unknown":
		r.BudgetWarning = "budget unknown"
	case "warning":
		r.BudgetWarning = "budget " + r.Budget.Reason
	case "stale":
		r.BudgetWarning = "budget reading stale"
	}
	return r
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// reason renders the one line: family, Tier, level, source, tail.
func reason(r *Route, source string) string {
	var tail []string
	if r.TieBreak != "" {
		tail = append(tail, r.TieBreak)
	}
	if len(r.DefaultsUsed) > 0 {
		tail = append(tail, "defaulted "+strings.Join(r.DefaultsUsed, ", "))
	}
	if r.OverrideWarning != "" {
		tail = append(tail, "warning: "+r.OverrideWarning)
	}
	if r.BudgetWarning != "" {
		tail = append(tail, r.BudgetWarning)
	}
	line := fmt.Sprintf("%-13s %-9s %-12s %s", r.FamilyID, titleCase(string(r.Tier)), r.Level, source)
	if len(tail) > 0 {
		line += "  (" + strings.Join(tail, "; ") + ")"
	}
	return line
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// constrainedLevel applies the same level bounds to the estimate and Jev score.
func constrainedLevel(req Request, level roster.Level) roster.Level {
	if req.Floor != "" && roster.TierOf(level).Rank() < req.Floor.Rank() {
		level = roster.LevelForTier(req.Floor)
	}
	if req.Ceiling != "" && roster.TierOf(level).Rank() > req.Ceiling.Rank() {
		level = roster.LevelForTier(req.Ceiling)
	}
	if req.Access == brief.AccessEdit && level == roster.LevelNone {
		level = roster.LevelSome
	}
	return level
}

func allowedTier(req Request, tier roster.Tier) bool {
	return (req.Access != brief.AccessEdit || tier != roster.TierLight) &&
		(req.Floor == "" || tier.Rank() >= req.Floor.Rank()) &&
		(req.Ceiling == "" || tier.Rank() <= req.Ceiling.Rank())
}
