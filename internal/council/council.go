// Package council runs several read Lanes on one question in parallel, in
// one or more Rounds, and returns a mechanical Merge: a status table, the
// findings per Seat with Jev's marks, and the report paths.
package council

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

// Triggers.
const (
	TriggerManual   = "manual"
	TriggerPlan     = "plan"
	TriggerEndOfRun = "end_of_run"
	TriggerTask     = "task"
	TriggerJevGate  = "jev_gate"
)

// Opts are the inputs of one Council.
type Opts struct {
	Question string
	Slug     string
	Seats    int
	Lanes    []string // --lane family[:effort], repeatable
	Level    roster.Level
	Tier     roster.Tier // set by automatic points; beats Level
	Rounds   int
	Trigger  string
	Run      string
	Task     string   // --task: the Task whose result is reviewed
	Files    []string // file references every Seat reads first
	Authors  []string // families that authored work under review
	Context  string
}

// Seat is one Lane's place.
type Seat struct {
	Family    string         `json:"family"`
	Harness   string         `json:"harness"`
	Effort    string         `json:"effort"`
	Model     string         `json:"model"`
	Account   string         `json:"account"`
	Requested bool           `json:"requested"`
	Author    bool           `json:"author"`
	Rounds    []SeatRound    `json:"rounds"`
	Route     *router.Route  `json:"-"`
	Family_   *roster.Family `json:"-"`
}

// SeatRound is one Seat's result in one Round.
type SeatRound struct {
	Round     int     `json:"round"`
	Status    string  `json:"status"` // ok | failed | dropped
	Position  string  `json:"position,omitempty"`
	Report    string  `json:"report,omitempty"`
	DurationS float64 `json:"duration_s"`
	Error     string  `json:"error,omitempty"`
}

// Finding is one bullet from a Seat's ## Findings with Jev's mark.
type Finding struct {
	Seat        string  `json:"seat"`
	Round       int     `json:"round"`
	Text        string  `json:"text"`
	Probability float64 `json:"probability"`
}

// Council is council.json.
type Council struct {
	Kind          string       `json:"kind"`
	Project       string       `json:"project"`
	Slug          string       `json:"slug"`
	Question      string       `json:"question"`
	Trigger       string       `json:"trigger"`
	Run           string       `json:"run,omitempty"`
	Task          string       `json:"task,omitempty"`
	Todo          int          `json:"todo,omitempty"`
	Tier          roster.Tier  `json:"tier"`
	Level         roster.Level `json:"level"`
	RoundsAsked   int          `json:"rounds_asked"`
	RoundsRun     int          `json:"rounds"`
	Seats         []*Seat      `json:"seats"`
	Relaxed       []string     `json:"relaxed_rules"`
	SingleOpinion bool         `json:"single_opinion"`
	Findings      []Finding    `json:"findings"`
	Filtered      []Finding    `json:"filtered"`
	FilterSkipped string       `json:"filter_skipped,omitempty"`
	State         string       `json:"state"` // running | finished
	Pid           int          `json:"pid,omitempty"`
	StartedAt     time.Time    `json:"started_at"`
	EndedAt       time.Time    `json:"ended_at,omitempty"`
	DurationS     float64      `json:"duration_s"`
	Path          string       `json:"path"`
	Files         []string     `json:"files,omitempty"`
	Context       string       `json:"context,omitempty"`
}

const FileManifest = "council.json"

// Save writes council.json atomically.
func (c *Council) Save() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(c.Path, FileManifest+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(c.Path, FileManifest))
}

// Load reads council.json from a Council folder.
func Load(folder string) (*Council, error) {
	b, err := os.ReadFile(filepath.Join(folder, FileManifest))
	if err != nil {
		return nil, err
	}
	var c Council
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Find resolves a slug to its folder.
func Find(root, slug string) (string, bool) {
	p := filepath.Join(config.CouncilsDir(root), slug)
	if _, err := os.Stat(filepath.Join(p, FileManifest)); err == nil {
		return p, true
	}
	return "", false
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify makes a folder name from a question.
func Slugify(q string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(q), "-"), "-")
	words := strings.Split(s, "-")
	if len(words) > 6 {
		words = words[:6]
	}
	s = strings.Join(words, "-")
	if s == "" {
		s = "council"
	}
	return s
}

// Create picks the Seats and lays out the folder. It starts nothing.
func Create(env *dispatch.Env, o Opts) (*Council, error) {
	if strings.TrimSpace(o.Question) == "" {
		return nil, fmt.Errorf("a council needs a question")
	}
	if o.Seats == 0 {
		o.Seats = 3
	}
	if o.Seats < 2 || o.Seats > 4 {
		return nil, fmt.Errorf("--seats must be 2 to 4")
	}
	if o.Rounds == 0 {
		o.Rounds = 1
	}
	if o.Rounds > 4 {
		return nil, fmt.Errorf("--rounds is capped at 4")
	}
	if o.Trigger == "" {
		o.Trigger = TriggerManual
	}
	slug := o.Slug
	if slug == "" {
		slug = Slugify(o.Question)
	}
	if err := config.EnsureIgnoredDir(config.CouncilsDir(env.Root)); err != nil {
		return nil, err
	}
	folder := filepath.Join(config.CouncilsDir(env.Root), slug)
	for i := 2; ; i++ {
		if _, err := os.Stat(folder); os.IsNotExist(err) {
			break
		}
		folder = filepath.Join(config.CouncilsDir(env.Root), fmt.Sprintf("%s-%d", slug, i))
	}
	slug = filepath.Base(folder)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return nil, err
	}
	c := &Council{Kind: "council", Project: env.Root, Slug: slug, Question: o.Question, Trigger: o.Trigger, Run: o.Run, Task: o.Task,
		RoundsAsked: o.Rounds, State: "running", StartedAt: time.Now(), Path: folder, Files: o.Files, Context: o.Context, Relaxed: []string{}, Findings: []Finding{}, Filtered: []Finding{}}
	if err := os.WriteFile(filepath.Join(folder, "question.md"), []byte(o.Question+"\n"), 0o644); err != nil {
		return nil, err
	}
	// Tier: given by an automatic point, else --level, else Jev's score, else Daily.
	level := o.Level
	if o.Tier != "" {
		level = roster.LevelForTier(o.Tier)
	}
	if level == "" {
		level = jevLevel(env, o.Question)
	}
	if level == "" {
		level = roster.LevelSome
	}
	c.Level = level
	c.Tier = roster.TierOf(level)
	if o.Tier != "" {
		c.Tier = o.Tier
	}
	seats, relaxed, err := pickSeats(env, c, o)
	if err != nil {
		return nil, err
	}
	c.Seats = seats
	c.Relaxed = relaxed
	if len(seats) == 1 {
		c.SingleOpinion = true
	}
	if err := c.Save(); err != nil {
		return nil, err
	}
	todoFor(env, c)
	return c, nil
}

// jevLevel asks the Router's Jev backend for the question's Judgment level.
func jevLevel(env *dispatch.Env, question string) roster.Level {
	if env.Roster.Router.Backend == "rules" || (env.Project != nil && env.Project.Router == "rules") {
		return ""
	}
	b := &brief.Brief{Goal: question, Done: "An answer with findings.", Sections: map[string]string{}}
	b.Title = "council"
	b.Category = "second opinion"
	rt := env.NewRouter()
	r, err := rt.Route(router.Request{Brief: b, Access: brief.AccessRead, Project: env.Project})
	if err != nil || r == nil || r.Jev == nil {
		return ""
	}
	return r.Level
}

// pickSeats: one per vendor at the Tier, subscription vendors first by plan
// price, agy only to fill or on --seats 4; author families may sit; relax
// in order until two Seats exist.
func pickSeats(env *dispatch.Env, c *Council, o Opts) ([]*Seat, []string, error) {
	rt := env.NewRouter()
	authors := map[string]bool{}
	for _, a := range o.Authors {
		authors[a] = true
	}
	forbidden := map[string]bool{}
	if env.Project != nil {
		for _, f := range env.Project.Forbid {
			forbidden[f] = true
		}
	}
	var seats []*Seat
	var relaxed []string
	taken := map[string]bool{}
	add := func(f *roster.Family, effort string, requested bool) {
		if effort == "" {
			effort = f.Levels[c.Level]
			if !f.Serves(c.Level) {
				effort = f.Levels[nearest(f, c.Level)]
			}
		}
		model, _ := env.Disc.Resolve(f.Harness, f.Models)
		seats = append(seats, &Seat{Family: f.ID, Harness: f.Harness, Effort: effort, Model: model, Account: f.Account, Requested: requested, Author: authors[f.ID], Family_: f})
		taken[f.ID] = true
	}
	// Requested Seats first.
	for _, lane := range o.Lanes {
		id, effort := lane, ""
		if i := strings.IndexByte(lane, ':'); i >= 0 {
			id, effort = lane[:i], lane[i+1:]
		}
		f, ok := env.Roster.Family(id)
		if !ok || !f.Enabled {
			return nil, nil, fmt.Errorf("unknown or disabled family %q", id)
		}
		if why := rt.Preflight(f, brief.AccessRead); why != "" {
			return nil, nil, fmt.Errorf("family %s fails pre-flight: %s", id, why)
		}
		add(f, effort, true)
	}
	// Candidates: enabled, not request-only, not forbidden, enforced read, pre-flight ok.
	type cand struct {
		f       *roster.Family
		metered bool
		price   float64
	}
	byVendor := map[string][]cand{}
	var vendors []string
	for _, f := range env.Roster.SortedFamilies() {
		if !f.Enabled || f.RequestOnly || forbidden[f.ID] || taken[f.ID] {
			continue
		}
		h, _ := env.Reg.Get(f.Harness)
		if h == nil || h.Enforcement(brief.AccessRead) != harness.Enforced {
			continue
		}
		if rt.Preflight(f, brief.AccessRead) != "" {
			continue
		}
		acct := env.Roster.Accounts[f.Account]
		if _, seen := byVendor[f.Harness]; !seen {
			vendors = append(vendors, f.Harness)
		}
		byVendor[f.Harness] = append(byVendor[f.Harness], cand{f: f, metered: acct.Billing == "metered", price: acct.PlanPrice})
	}
	// Best family per vendor for the Tier: serves the level, else the nearest below, else above.
	best := func(cs []cand) *roster.Family {
		var exact, below, above *roster.Family
		for _, x := range cs {
			switch {
			case x.f.Serves(c.Level):
				if exact == nil {
					exact = x.f
				}
			case x.f.HighestLevel().Rank() < c.Level.Rank():
				if below == nil || x.f.HighestLevel().Rank() > below.HighestLevel().Rank() {
					below = x.f
				}
			default:
				if above == nil {
					above = x.f
				}
			}
		}
		if exact != nil {
			return exact
		}
		if below != nil {
			return below
		}
		return above
	}
	// Vendors ordered: subscription by plan price desc, then metered.
	sort.SliceStable(vendors, func(i, j int) bool {
		a, b := byVendor[vendors[i]][0], byVendor[vendors[j]][0]
		if a.metered != b.metered {
			return !a.metered
		}
		return a.price > b.price
	})
	vendorSeated := map[string]bool{}
	for _, s := range seats {
		vendorSeated[s.Harness] = true
	}
	// Pass 1: non-author subscription vendors.
	for _, v := range vendors {
		if len(seats) >= o.Seats {
			break
		}
		cs := byVendor[v]
		if cs[0].metered || vendorSeated[v] {
			continue
		}
		f := best(cs)
		if f == nil || authors[f.ID] {
			continue
		}
		add(f, "", false)
		vendorSeated[v] = true
	}
	// Pass 2: author families.
	for _, v := range vendors {
		if len(seats) >= o.Seats {
			break
		}
		cs := byVendor[v]
		if cs[0].metered || vendorSeated[v] {
			continue
		}
		if f := best(cs); f != nil {
			add(f, "", false)
			vendorSeated[v] = true
		}
	}
	// Pass 3: metered vendors fill a missing Seat, or sit on --seats 4.
	for _, v := range vendors {
		if len(seats) >= o.Seats {
			break
		}
		cs := byVendor[v]
		if !cs[0].metered || vendorSeated[v] {
			continue
		}
		if f := best(cs); f != nil {
			add(f, "", false)
			vendorSeated[v] = true
		}
	}
	// Relax until two Seats: two Seats from one vendor with different models.
	if len(seats) < 2 {
		for _, v := range vendors {
			if len(seats) >= 2 {
				break
			}
			for _, x := range byVendor[v] {
				if len(seats) >= 2 {
					break
				}
				if !taken[x.f.ID] {
					add(x.f, "", false)
					relaxed = append(relaxed, "two seats from one vendor: "+v)
				}
			}
		}
	}
	for _, s := range seats {
		if s.Author {
			relaxed = append(relaxed, "author family seated: "+s.Family)
		}
	}
	if len(seats) == 0 {
		return nil, nil, fmt.Errorf("no family can take a council seat (an enforced read lane that passes pre-flight)")
	}
	if len(seats) == 1 {
		relaxed = append(relaxed, "single opinion")
	}
	return seats, relaxed, nil
}

func nearest(f *roster.Family, want roster.Level) roster.Level {
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
