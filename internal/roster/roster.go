// Package roster merges the embedded defaults with the user file into the one
// effective Roster: families, accounts, the rules table, prices, and the
// global blocks. Nothing else describes a Lane.
package roster

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jalendport/maestro/internal/config"
)

// Level is a Judgment level: how much a Lane must decide on its own.
type Level string

const (
	LevelNone        Level = "none"
	LevelSome        Level = "some"
	LevelSignificant Level = "significant"
	LevelExpert      Level = "expert"
)

// Levels in ladder order, lowest first.
var Levels = []Level{LevelNone, LevelSome, LevelSignificant, LevelExpert}

// Tier is a rung on the strength ladder.
type Tier string

const (
	TierLight    Tier = "light"
	TierDaily    Tier = "daily"
	TierHeavy    Tier = "heavy"
	TierFrontier Tier = "frontier"
)

// Tiers in ladder order, lowest first.
var Tiers = []Tier{TierLight, TierDaily, TierHeavy, TierFrontier}

// TierOf maps a Judgment level to the Tier that serves it.
func TierOf(l Level) Tier {
	switch l {
	case LevelNone:
		return TierLight
	case LevelSome:
		return TierDaily
	default:
		return TierHeavy
	}
}

// Rank orders Tiers and Levels for "higher than" comparisons.
func (t Tier) Rank() int {
	for i, x := range Tiers {
		if x == t {
			return i
		}
	}
	return -1
}

func (l Level) Rank() int {
	for i, x := range Levels {
		if x == l {
			return i
		}
	}
	return -1
}

// ParseLevel accepts a level word; "" is the zero value.
func ParseLevel(s string) (Level, error) {
	switch Level(strings.ToLower(s)) {
	case "":
		return "", nil
	case LevelNone, LevelSome, LevelSignificant, LevelExpert:
		return Level(strings.ToLower(s)), nil
	}
	return "", fmt.Errorf("unknown judgment level %q (none, some, significant, expert)", s)
}

// ParseTier accepts a tier word; "" is the zero value.
func ParseTier(s string) (Tier, error) {
	switch Tier(strings.ToLower(s)) {
	case "":
		return "", nil
	case TierLight, TierDaily, TierHeavy, TierFrontier:
		return Tier(strings.ToLower(s)), nil
	}
	return "", fmt.Errorf("unknown tier %q (light, daily, heavy, frontier)", s)
}

// Family is one Harness × model pair with every embedded and user field resolved.
type Family struct {
	ID          string
	Harness     string
	Models      []string
	Levels      map[Level]string
	Account     string
	GoodFor     string
	NotFor      string
	RequestOnly bool
	Frontier    bool
	Enabled     bool
}

// Tiers returns the Tiers this family serves, derived from its level map.
func (f *Family) Tiers() []Tier {
	seen := map[Tier]bool{}
	var out []Tier
	if f.Frontier {
		seen[TierFrontier] = true
		out = append(out, TierFrontier)
	}
	for _, l := range Levels {
		if _, ok := f.Levels[l]; ok {
			t := TierOf(l)
			if f.Frontier {
				continue
			}
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// Serves reports whether the family lists an effort for the level.
func (f *Family) Serves(l Level) bool {
	_, ok := f.Levels[l]
	return ok
}

// TierFor is the Tier the family sits in for a level it serves.
func (f *Family) TierFor(l Level) Tier {
	if f.Frontier {
		return TierFrontier
	}
	return TierOf(l)
}

// HighestLevel is the top of the family's level map.
func (f *Family) HighestLevel() Level {
	var top Level
	for _, l := range Levels {
		if _, ok := f.Levels[l]; ok {
			top = l
		}
	}
	return top
}

// Roster is the effective merge.
type Roster struct {
	Accounts    map[string]config.Account
	Harness     map[string]config.Harness
	Families    map[string]*Family
	Rules       map[Tier]config.Chains
	Prices      map[string]config.Price
	Aliases     map[string]string
	Hook        config.Hook
	Router      config.Router
	Budget      config.Budget
	Failure     config.Failure
	Concurrency config.Concurrency
	Warnings    []string
}

// Load builds the Roster from the embedded defaults and the user file.
func Load() (*Roster, error) {
	defaults, _, err := config.ParseFile(Defaults)
	if err != nil {
		return nil, fmt.Errorf("embedded defaults: %w", err)
	}
	user, unknown, err := config.LoadUser()
	if err != nil {
		return nil, err
	}
	r := Merge(defaults, user)
	for _, k := range unknown {
		r.Warnings = append(r.Warnings, fmt.Sprintf("unknown key %q in %s (ignored)", k, config.UserFile()))
	}
	return r, nil
}

// Merge applies the user deltas on top of the defaults. The user file is
// never rewritten; the merge happens in memory on every start.
func Merge(d, u *config.File) *Roster {
	r := &Roster{
		Accounts:    map[string]config.Account{},
		Harness:     map[string]config.Harness{},
		Families:    map[string]*Family{},
		Rules:       map[Tier]config.Chains{},
		Prices:      map[string]config.Price{},
		Aliases:     map[string]string{},
		Hook:        d.Hook,
		Router:      d.Router,
		Budget:      d.Budget,
		Failure:     d.Failure,
		Concurrency: d.Concurrency,
	}
	userAccounts := map[string]config.Account{}
	replaced := map[string]bool{}
	for id, delta := range u.Accounts {
		base, exists := d.Accounts[id]
		if !exists {
			base = config.Account{Billing: "subscription", BlockAt: 0.90}
		}
		account := withAccountDefaults(delta, base)
		userAccounts[id] = account
		replaced[account.Harness] = true
	}
	for id, account := range d.Accounts {
		if !replaced[account.Harness] {
			r.Accounts[id] = account
		}
	}
	for id, account := range userAccounts {
		r.Accounts[id] = account
	}
	for k, v := range d.Harness {
		r.Harness[k] = v
	}
	for k, v := range u.Harness {
		base := r.Harness[k]
		if v.Bin != "" {
			base.Bin = v.Bin
		}
		if v.MinVersion != "" {
			base.MinVersion = v.MinVersion
		}
		if v.StripEnv != nil {
			base.StripEnv = v.StripEnv
		}
		if v.SoloTool != 0 {
			base.SoloTool = v.SoloTool
		}
		r.Harness[k] = base
	}
	for id, f := range d.Families {
		r.Families[id] = toFamily(id, f, nil)
	}
	for id, f := range u.Families {
		if base, ok := d.Families[id]; ok {
			r.Families[id] = toFamily(id, base, &f)
		} else {
			if f.Harness == nil {
				r.Warnings = append(r.Warnings, fmt.Sprintf("family %q in the user file names no harness (ignored)", id))
				continue
			}
			r.Families[id] = toFamily(id, f, nil)
		}
	}
	for t, chains := range d.Rules {
		r.Rules[Tier(t)] = cloneChains(chains)
	}
	for t, chains := range u.Rules {
		if r.Rules[Tier(t)] == nil {
			r.Rules[Tier(t)] = config.Chains{}
		}
		for cat, chain := range chains {
			r.Rules[Tier(t)][cat] = chain
		}
	}
	for k, v := range d.Prices {
		r.Prices[k] = v
	}
	for k, v := range u.Prices {
		r.Prices[k] = v
	}
	for k, v := range d.Aliases {
		r.Aliases[k] = v
	}
	for k, v := range u.Aliases {
		r.Aliases[k] = v
	}
	if u.Hook.Scope != "" {
		r.Hook.Scope = u.Hook.Scope
	}
	mergeRouter(&r.Router, u.Router)
	if id := r.Router.Orchestrator; id != "" {
		if f := r.Families[id]; f != nil {
			f.RequestOnly = true
		} else {
			r.Warnings = append(r.Warnings, fmt.Sprintf("orchestrator family %q is not in the Roster", id))
		}
	}
	mergeBudget(&r.Budget, u.Budget)
	mergeFailure(&r.Failure, u.Failure)
	mergeConcurrency(&r.Concurrency, u.Concurrency)
	r.bindAccounts()
	r.checkDrift(d, u)
	return r
}

func withAccountDefaults(a, base config.Account) config.Account {
	if a.Has("harness") {
		base.Harness = a.Harness
	}
	if a.Has("billing") {
		base.Billing = a.Billing
	}
	if a.Has("plan_price") {
		base.PlanPrice = a.PlanPrice
	}
	if a.Has("block_at") {
		base.BlockAt = a.BlockAt
	}
	if a.Has("reserve") {
		base.Reserve = a.Reserve
	}
	if a.Has("budget") && a.Cap != nil {
		cap := config.Cap{}
		if base.Cap != nil {
			cap = *base.Cap
		}
		if a.Has("budget.usd") {
			cap.USD = a.Cap.USD
		}
		if a.Has("budget.period") {
			cap.Period = a.Cap.Period
		}
		base.Cap = &cap
	}
	return base
}

// ValidateAccounts rejects logins that cannot be selected at launch.
func (r *Roster) ValidateAccounts() error {
	byHarness := map[string][]string{}
	for id, account := range r.Accounts {
		byHarness[account.Harness] = append(byHarness[account.Harness], id)
	}
	names := make([]string, 0, len(byHarness))
	for name := range byHarness {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ids := byHarness[name]
		if len(ids) > 1 {
			sort.Strings(ids)
			return fmt.Errorf("harness %s has multiple accounts (%s); use one account until account-specific launch settings exist", name, strings.Join(ids, ", "))
		}
	}
	return nil
}

func toFamily(id string, base config.Family, delta *config.Family) *Family {
	f := &Family{ID: id, Enabled: true, Levels: map[Level]string{}}
	apply := func(c config.Family) {
		if c.Harness != nil {
			f.Harness = *c.Harness
		}
		if c.Models != nil {
			f.Models = c.Models
		}
		if c.Levels != nil {
			f.Levels = map[Level]string{}
			for k, v := range c.Levels {
				f.Levels[Level(k)] = v
			}
		}
		if c.Account != nil {
			f.Account = *c.Account
		}
		if c.GoodFor != nil {
			f.GoodFor = *c.GoodFor
		}
		if c.NotFor != nil {
			f.NotFor = *c.NotFor
		}
		if c.RequestOnly != nil {
			f.RequestOnly = *c.RequestOnly
		}
		if c.Frontier != nil {
			f.Frontier = *c.Frontier
		}
		if c.Enabled != nil {
			f.Enabled = *c.Enabled
		}
	}
	apply(base)
	if delta != nil {
		apply(*delta)
	}
	return f
}

func cloneChains(c config.Chains) config.Chains {
	out := config.Chains{}
	for k, v := range c {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func mergeRouter(dst *config.Router, u config.Router) {
	if u.Orchestrator != "" {
		dst.Orchestrator = u.Orchestrator
	}
	if u.Backend != "" {
		dst.Backend = u.Backend
	}
	if u.BaseURL != "" {
		dst.BaseURL = u.BaseURL
	}
	if u.Model != "" {
		dst.Model = u.Model
	}
	for backend, th := range u.Thresholds {
		if dst.Thresholds[backend] == nil {
			dst.Thresholds[backend] = map[string]float64{}
		}
		for k, v := range th {
			dst.Thresholds[backend][k] = v
		}
	}
}

func mergeBudget(dst *config.Budget, u config.Budget) {
	if u.StaleAfter.Duration != 0 {
		dst.StaleAfter = u.StaleAfter
	}
	if u.BlockAt != 0 {
		dst.BlockAt = u.BlockAt
	}
	if u.WarnAt != 0 {
		dst.WarnAt = u.WarnAt
	}
	if u.AssumedHeadroom != 0 {
		dst.AssumedHeadroom = u.AssumedHeadroom
	}
}

func mergeFailure(dst *config.Failure, u config.Failure) {
	if u.IdleLimit.Duration != 0 {
		dst.IdleLimit = u.IdleLimit
	}
	for k, v := range u.WallLimit {
		dst.WallLimit[k] = v
	}
	if u.MaxDispatches != 0 {
		dst.MaxDispatches = u.MaxDispatches
	}
	if u.KeepAwake != nil {
		dst.KeepAwake = u.KeepAwake
	}
	if u.RunPauseAfterEscalations != 0 {
		dst.RunPauseAfterEscalations = u.RunPauseAfterEscalations
	}
	if u.RunPauseAfterSameError != 0 {
		dst.RunPauseAfterSameError = u.RunPauseAfterSameError
	}
}

func mergeConcurrency(dst *config.Concurrency, u config.Concurrency) {
	if u.MaxParallel != 0 {
		dst.MaxParallel = u.MaxParallel
	}
	if u.MaxPerAccount != 0 {
		dst.MaxPerAccount = u.MaxPerAccount
	}
	if u.MaxTotal != 0 {
		dst.MaxTotal = u.MaxTotal
	}
	if u.CloseTabs != "" {
		dst.CloseTabs = u.CloseTabs
	}
}

// bindAccounts binds every family to its Harness's single
// Account, or warns when the Harness has several.
func (r *Roster) bindAccounts() {
	byHarness := map[string][]string{}
	for id, a := range r.Accounts {
		byHarness[a.Harness] = append(byHarness[a.Harness], id)
	}
	for _, f := range r.Families {
		ids := byHarness[f.Harness]
		switch len(ids) {
		case 1:
			f.Account = ids[0]
		case 0:
			r.Warnings = append(r.Warnings, fmt.Sprintf("family %s: no account for harness %s", f.ID, f.Harness))
		default:
			sort.Strings(ids)
			r.Warnings = append(r.Warnings, fmt.Sprintf("family %s must name one of the %s accounts: %s", f.ID, f.Harness, strings.Join(ids, ", ")))
		}
	}
}

// checkDrift adds the warnings the roster spec names: a user override of a
// family the defaults no longer have, an account no family uses, a level that
// names a fan-out effort.
func (r *Roster) checkDrift(d, u *config.File) {
	for id := range u.Families {
		if _, ok := d.Families[id]; !ok {
			if _, ok := u.Families[id]; ok && u.Families[id].Harness == nil {
				r.Warnings = append(r.Warnings, fmt.Sprintf("user file overrides family %q, which the embedded defaults no longer have", id))
			}
		}
	}
	used := map[string]bool{}
	for _, f := range r.Families {
		used[f.Account] = true
	}
	for id := range r.Accounts {
		if !used[id] {
			r.Warnings = append(r.Warnings, fmt.Sprintf("account %q is used by no family", id))
		}
	}
	for _, f := range r.Families {
		for l, e := range f.Levels {
			switch e {
			case "max", "ultra", "ultracode":
				r.Warnings = append(r.Warnings, fmt.Sprintf("family %s level %s names fan-out effort %q; it is never used automatically", f.ID, l, e))
			}
		}
	}
}

// Family returns one family by id.
func (r *Roster) Family(id string) (*Family, bool) {
	f, ok := r.Families[id]
	return f, ok
}

// SortedFamilies returns the families in a stable display order: by harness,
// then by the lowest level they serve, then by id.
func (r *Roster) SortedFamilies() []*Family {
	order := map[string]int{"claude": 0, "codex": 1, "grok": 2, "agy": 3}
	out := make([]*Family, 0, len(r.Families))
	for _, f := range r.Families {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if order[a.Harness] != order[b.Harness] {
			return order[a.Harness] < order[b.Harness]
		}
		la, lb := lowestLevel(a), lowestLevel(b)
		if la != lb {
			return la < lb
		}
		return a.ID < b.ID
	})
	return out
}

func lowestLevel(f *Family) int {
	for i, l := range Levels {
		if _, ok := f.Levels[l]; ok {
			return i
		}
	}
	return len(Levels)
}

// Chain returns the rules-table chain for a Tier and category, falling back
// to the Tier's default chain.
func (r *Roster) Chain(t Tier, category string) []string {
	chains := r.Rules[t]
	if chains == nil {
		return nil
	}
	if c, ok := chains[category]; ok && category != "" {
		return c
	}
	return chains["default"]
}

// WallLimit is the Wall limit for a Tier.
func (r *Roster) WallLimit(t Tier) config.Duration {
	return r.Failure.WallLimit[string(t)]
}

// KeepAwake reports the caffeinate switch.
func (r *Roster) KeepAwake() bool {
	return r.Failure.KeepAwake == nil || *r.Failure.KeepAwake
}

// Threshold reads one router threshold for the active backend.
func (r *Roster) Threshold(key string) float64 {
	if th, ok := r.Router.Thresholds[r.Router.Backend]; ok {
		return th[key]
	}
	return 0
}

// StripEnv returns the environment names a Harness's Dispatches drop.
func (r *Roster) StripEnv(harness string) []string {
	return r.Harness[harness].StripEnv
}
