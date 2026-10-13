package router

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/jev"
	"github.com/jalendport/maestro/internal/roster"
)

// JevRoute is what the ledger keeps from one Jev Route call.
type JevRoute struct {
	Model         string             `json:"jev_model"`
	OptionCount   int                `json:"option_count"`
	Probabilities map[string]float64 `json:"probabilities"`
	Level         map[string]float64 `json:"level_probabilities,omitempty"`
	NeedsWrite    float64            `json:"needs_write"`
	SpecConcrete  float64            `json:"spec_is_concrete"`
	WantsView     float64            `json:"wants_independent_view"`
	Fits          map[string]float64 `json:"fits,omitempty"`
	ElapsedMS     int64              `json:"elapsed_ms"`
	InputTokens   int                `json:"input_tokens"`
	Error         string             `json:"error,omitempty"`
}

// Gate names are returned as AskOrchestrator reasons or as a Council signal.
const (
	GateVague   = "clarify the brief"
	GateCouncil = "council"
	GateAsk     = "ask_orchestrator"
)

// CouncilSignal is returned when the Council gate fires so the caller
// can start a Council or ask the Orchestrator.
type CouncilSignal struct{ Reason string }

func (c *CouncilSignal) Error() string { return c.Reason }

// jevRoute runs one Route call and applies the thresholds in order. It
// returns (nil, nil) when Jev is absent: error, timeout, or no key; the
// caller then falls back to the rules table.
func (rt *Router) jevRoute(req Request, cands []*roster.Family, level roster.Level, category string, defaults []string) (*Route, *JevRoute, error) {
	th := func(k string) float64 { return rt.Roster.Threshold(k) }
	timeout := time.Duration(th("timeout_s") * float64(time.Second))
	client, err := jev.New(rt.Roster.Router.BaseURL, rt.Roster.Router.Model, timeout)
	if err != nil {
		return nil, &JevRoute{Error: err.Error()}, nil
	}
	options := map[string]any{}
	for _, f := range cands {
		options[f.ID] = map[string]string{"good_for": f.GoodFor, "not_for": f.NotFor}
	}
	options[GateAsk] = map[string]string{"good_for": "the brief is unclear, contradicts itself, or no listed lane fits", "not_for": "a clear, bounded task that one listed lane can finish"}
	questions := map[string]jev.Question{
		"lane":                   jev.Choice("Pick the lane that is strong enough to finish this task on its own. Each lane lists what it is good for and not for.", options),
		"judgment_level":         jev.Score("How much must the lane decide on its own? none: the brief names every change. some: location and goal are known, small details open. significant: the approach spans several files, or the cause is not stated. expert: open design, unreproducible failure, concurrency, security, or architecture.", []string{"none", "some", "significant", "expert"}),
		"needs_write":            jev.Noul("Does this task change files in the repository?"),
		"spec_is_concrete":       jev.Noul("Is the brief concrete enough to start without asking a question?"),
		"wants_independent_view": jev.Noul("Does this task ask for an independent second opinion or a review rather than work?"),
	}
	for _, f := range cands {
		questions["fits_"+f.ID] = jev.Noul(fmt.Sprintf("Is %s a good fit for this task? It is good for: %s. It is not for: %s.", f.ID, f.GoodFor, f.NotFor))
	}
	if req.Project != nil {
		client.Redact = req.Project.Redact
	}
	state := map[string]any{"task": routeState(req)}
	resp, err := client.Ask(context.Background(), state, questions)
	if err != nil {
		return nil, &JevRoute{Error: err.Error()}, nil
	}
	lane := resp.Answers["lane"]
	jr := &JevRoute{
		Model: resp.Model, OptionCount: len(options), Probabilities: lane.Probabilities,
		Level:        resp.Answers["judgment_level"].Probabilities,
		NeedsWrite:   resp.Answers["needs_write"].Noul,
		SpecConcrete: resp.Answers["spec_is_concrete"].Noul,
		WantsView:    resp.Answers["wants_independent_view"].Noul,
		Fits:         map[string]float64{}, ElapsedMS: resp.Elapsed.Milliseconds(), InputTokens: resp.Usage.Input,
	}
	for _, f := range cands {
		if a, ok := resp.Answers["fits_"+f.ID]; ok {
			jr.Fits[f.ID] = a.Noul
		}
	}
	byID := map[string]*roster.Family{}
	for _, f := range cands {
		byID[f.ID] = f
	}
	scoreLevel := levelFromScore(resp.Answers["judgment_level"].Score)
	pickID := lane.Choice
	conf := lane.Confidence
	pick := byID[pickID]
	pickTier := roster.TierOf(scoreLevel)
	if pick != nil {
		pickTier = pick.TierFor(scoreLevel)
	}

	// 1. Vague-Brief gate.
	if jr.SpecConcrete < th("vague_brief") && !(pick != nil && pickTier.Rank() >= roster.TierHeavy.Rank() && conf >= th("heavy_override")) {
		return nil, jr, &AskOrchestrator{Reason: fmt.Sprintf("%s: spec_is_concrete %.2f", GateVague, jr.SpecConcrete)}
	}
	// 2. Council gate.
	if jr.WantsView > th("council_view") && jr.NeedsWrite < th("council_write") {
		return nil, jr, &CouncilSignal{Reason: fmt.Sprintf("%s: wants_independent_view %.2f, needs_write %.2f", GateCouncil, jr.WantsView, jr.NeedsWrite)}
	}
	// 3. Ask gate.
	if pickID == GateAsk && conf >= th("ask") {
		return nil, jr, &AskOrchestrator{Reason: fmt.Sprintf("router asked for the orchestrator (confidence %.2f)", conf)}
	}
	band := "high"
	tieBreak := ""
	switch {
	case conf >= th("dispatch"):
		// Near-tie between equally fit subscription families of one Tier: the
		// higher allocation weight wins.
		if second, p := secondOption(lane.Probabilities, pickID); second != "" && pick != nil && lane.Probabilities[pickID]-p <= 0.10 {
			if sf := byID[second]; sf != nil && sf.TierFor(scoreLevel) == pick.TierFor(scoreLevel) &&
				rt.Roster.Accounts[sf.Account].Billing != "metered" && rt.Roster.Accounts[pick.Account].Billing != "metered" &&
				rt.Weight(sf) > rt.Weight(pick) {
				pick, pickID = sf, second
				tieBreak = "equal fit, higher allocation"
			}
		}
	case conf >= th("step_up"):
		band = "medium"
		// 5. Step up to a higher-Tier second option, never down.
		if second, p := secondOption(lane.Probabilities, pickID); second != "" && p >= th("second_option") && scoreLevel.Rank() >= roster.LevelSome.Rank() {
			if sf := byID[second]; sf != nil && pick != nil && sf.TierFor(scoreLevel).Rank() > pick.TierFor(scoreLevel).Rank() {
				pick, pickID = sf, second
			}
		}
	default:
		// 6. Below the floor: the rules table decides.
		return nil, jr, nil
	}
	// Apply the computed level and all hard bounds again after Jev's choice.
	useLevel := scoreLevel
	if useLevel.Rank() < level.Rank() {
		useLevel = level
	}
	useLevel = constrainedLevel(req, useLevel)
	clamped := useLevel != scoreLevel
	if pick == nil || !pick.Serves(useLevel) || !allowedTier(req, pick.TierFor(useLevel)) || roster.FanOut(pick.Levels[useLevel]) {
		pick, _ = rt.rulesPick(cands, useLevel, category, req.Floor)
		clamped = true
	}
	if pick == nil {
		return nil, jr, nil
	}
	if clamped {
		defaults = append(defaults, "Jev pick clamped to allowed Tier "+string(pick.TierFor(useLevel)))
	}

	estimate, _ := roster.ParseLevel(req.Brief.Level)
	r := rt.finish(pick, useLevel, estimate, category, req.Access, "jev", defaults, nil)
	r.Confidence = conf
	r.Band = band
	r.TieBreak = tieBreak
	return r, jr, nil
}

// routeState is the Brief as Jev sees it: title, goal, done, facts. Never
// diffs, file lists, or numbers.
func routeState(req Request) map[string]any {
	s := map[string]any{"title": req.Brief.Title, "goal": req.Brief.Goal, "done": req.Brief.Done}
	if req.Brief.Category != "" {
		s["category"] = req.Brief.Category
	}
	if len(req.Brief.Facts) > 0 {
		s["facts"] = req.Brief.Facts
	}
	return s
}

// levelFromScore rounds Jev's 0..3 score to a level.
func levelFromScore(score float64) roster.Level {
	i := int(score + 0.5)
	if i < 0 {
		i = 0
	}
	if i >= len(roster.Levels) {
		i = len(roster.Levels) - 1
	}
	return roster.Levels[i]
}

func secondOption(probs map[string]float64, first string) (string, float64) {
	type kv struct {
		k string
		v float64
	}
	var all []kv
	for k, v := range probs {
		if k != first && k != GateAsk {
			all = append(all, kv{k, v})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
	if len(all) == 0 {
		return "", 0
	}
	return all[0].k, all[0].v
}

var _ = brief.AccessRead
