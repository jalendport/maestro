// Package router makes Routes: it applies the hard rules in code, then asks
// the backend (the rules table today, Jev next) to pick a family, and prints
// one reason line per Route. Jev never sees the hard rules as words.
package router

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
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
}

// Route decides. The rules table decides today; the Jev backend lands in
// milestone 2 and will use the same candidate list.
func (rt *Router) Route(req Request) (*Route, error) {
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
	if req.Floor != "" && roster.TierOf(level).Rank() < req.Floor.Rank() {
		level = levelForTier(req.Floor)
	}

	if req.Lane != "" {
		return rt.requested(req, level, estimate, category, defaults)
	}

	cands, excluded := rt.candidates(req, level)
	if req.Ceiling != "" || req.Floor != "" {
		// Floors and ceilings only shrink the list before the Route.
		var kept []*roster.Family
		for _, f := range cands {
			top := f.TierFor(f.HighestLevel())
			switch {
			case req.Ceiling != "" && f.TierFor(level).Rank() > req.Ceiling.Rank():
				excluded = append(excluded, Exclusion{f.ID, "above ceiling"})
			case req.Floor != "" && top.Rank() < req.Floor.Rank():
				excluded = append(excluded, Exclusion{f.ID, "below floor"})
			default:
				kept = append(kept, f)
			}
		}
		cands = kept
	}
	rulesFamily, tie := rt.rulesPick(cands, level, category)
	useRules := rt.Roster.Router.Backend == "rules" || (req.Project != nil && req.Project.Router == "rules")
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
		r.ReasonLine = reason(r, "rules fallback ("+firstLine(why)+")")
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

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 60 {
		s = s[:60] + "…"
	}
	return s
}

func levelForTier(t roster.Tier) roster.Level {
	switch t {
	case roster.TierLight:
		return roster.LevelNone
	case roster.TierDaily:
		return roster.LevelSome
	case roster.TierFrontier:
		return roster.LevelExpert
	default:
		return roster.LevelSignificant
	}
}

// candidates applies hard rules 2, 4, 6, 7, 8 and the enabled flag.
// Rule 3 (Budget block) joins in the budget milestone; rule 5 (reviewer
// family) in the council milestone.
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
		if rt.Roster.Accounts[f.Account].Billing == "metered" {
			metered = append(metered, f)
			continue
		}
		subscription = append(subscription, f)
	}
	out = subscription
	// Rule 8: a token-billed Lane joins only when the Task is read-only and
	// no subscription Lane serves the level.
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
			excluded = append(excluded, Exclusion{f.ID, "token-billed: tasks go to subscriptions"})
		}
	}
	return out, excluded
}

// Preflight is hard rule 4: installed, version gate, model in the catalog,
// and a launch shape for this Access. Empty means the family passes.
func (rt *Router) Preflight(f *roster.Family, access string) string {
	h, ok := rt.Reg.Get(f.Harness)
	if !ok {
		return "unknown harness " + f.Harness
	}
	rec, ok := rt.Disc.Harness[f.Harness]
	if !ok || rec.Path == "" {
		return f.Harness + " is not installed"
	}
	if min := rt.Roster.Harness[f.Harness].MinVersion; min != "" && rec.Version != "" && harness.VersionLess(rec.Version, min) {
		return fmt.Sprintf("needs %s ≥ %s (have %s)", f.Harness, min, rec.Version)
	}
	if _, ok := rt.Disc.Resolve(f.Harness, f.Models); !ok {
		return fmt.Sprintf("%s not in the %s catalog", strings.Join(f.Models, "/"), f.Harness)
	}
	if h.Enforcement(access) == harness.NoShape {
		return fmt.Sprintf("%s has no %s shape", f.Harness, access)
	}
	if _, err := h.Build(harness.Launch{Model: f.Models[0], Access: access, Root: "."}); err != nil {
		return err.Error()
	}
	return ""
}

// rulesPick walks the Tier's chain for the category and takes the first
// candidate that serves the level; then any candidate in the Tier.
func (rt *Router) rulesPick(cands []*roster.Family, level roster.Level, category string) (*roster.Family, string) {
	byID := map[string]*roster.Family{}
	for _, f := range cands {
		byID[f.ID] = f
	}
	tier := roster.TierOf(level)
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

// requested honours --lane: it beats every rule except Budget block and a
// dead or disabled Lane, with a one-line warning when it breaks one.
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
	if why := rt.Preflight(f, req.Access); why != "" {
		return nil, fmt.Errorf("family %s fails pre-flight: %s", id, why)
	}
	var warnings []string
	if f.RequestOnly {
		warnings = append(warnings, "request-only family")
	}
	if req.Access == brief.AccessRead {
		if h, _ := rt.Reg.Get(f.Harness); h.Enforcement("read") != harness.Enforced {
			warnings = append(warnings, "read task on a best-effort lane")
		}
	}
	if rt.Roster.Accounts[f.Account].Billing == "metered" && req.Access == brief.AccessEdit {
		return nil, fmt.Errorf("family %s is read-only (token-billed harness without an edit shape)", id)
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
		useLevel = nearestLevel(f, level)
		if useLevel != level {
			warnings = append(warnings, fmt.Sprintf("family serves level %s, not %s", useLevel, level))
		}
	}
	if useLevel == "" {
		return nil, fmt.Errorf("family %s serves no level", id)
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

func nearestLevel(f *roster.Family, want roster.Level) roster.Level {
	var best roster.Level
	for _, l := range roster.Levels {
		if !f.Serves(l) {
			continue
		}
		if best == "" || l.Rank() <= want.Rank() {
			best = l
		}
	}
	return best
}

func (rt *Router) finish(f *roster.Family, level, estimate roster.Level, category, access, source string, defaults []string, excluded []Exclusion) *Route {
	model, _ := rt.Disc.Resolve(f.Harness, f.Models)
	h, _ := rt.Reg.Get(f.Harness)
	r := &Route{
		Family: f, FamilyID: f.ID, Harness: f.Harness, Tier: f.TierFor(level), LevelEstimate: estimate, Level: level,
		Effort: f.Levels[level], Model: model, Account: f.Account, Source: source,
		DefaultsUsed: defaults, Category: category, Excluded: excluded, Enforcement: h.Enforcement(access),
	}
	if estimate != "" && abs(estimate.Rank()-level.Rank()) >= 2 {
		r.GapFlag = true
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
