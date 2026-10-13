package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// File is the shape of ~/.maestro/config.toml and of the embedded defaults.
// Families use pointer fields so a user delta can change one key and leave
// the rest of the embedded stanza alone.
type File struct {
	Version     int                `toml:"version"`
	Accounts    map[string]Account `toml:"accounts"`
	Harness     map[string]Harness `toml:"harness"`
	Families    map[string]Family  `toml:"families"`
	Rules       map[string]Chains  `toml:"rules"`
	Prices      map[string]Price   `toml:"prices"`
	Router      Router             `toml:"router"`
	Hook        Hook               `toml:"hook"`
	Budget      Budget             `toml:"budget"`
	Failure     Failure            `toml:"failure"`
	Concurrency Concurrency        `toml:"concurrency"`
	Aliases     map[string]string  `toml:"aliases"`
}

// Account is one login that Lanes bill against.
type Account struct {
	Harness      string  `toml:"harness"`
	Billing      string  `toml:"billing"` // subscription | metered
	PlanPrice    float64 `toml:"plan_price"`
	BlockAt      float64 `toml:"block_at"`
	Reserve      float64 `toml:"reserve"`
	PaidOverflow bool    `toml:"paid_overflow"`
	BudgetSource string  `toml:"budget_source"`
	Cap          *Cap    `toml:"budget"`
	BaseURL      string  `toml:"base_url"`
	defined      map[string]bool
}

// Has reports whether the user wrote an Account field. Programmatic Accounts
// supply a complete stanza.
func (a Account) Has(field string) bool { return a.defined == nil || a.defined[field] }

// Cap is the spend cap of a metered Account.
type Cap struct {
	USD    float64 `toml:"usd"`
	Period string  `toml:"period"`
}

// Harness holds the user overrides for one Harness adapter.
type Harness struct {
	Bin        string   `toml:"bin"`
	MinVersion string   `toml:"min_version"`
	StripEnv   []string `toml:"strip_env"`
	SoloTool   int      `toml:"solo_tool"`
}

// Family is one Harness × model pair, the Route option.
type Family struct {
	Harness     *string           `toml:"harness"`
	Models      []string          `toml:"models"`
	Levels      map[string]string `toml:"levels"`
	Account     *string           `toml:"account"`
	GoodFor     *string           `toml:"good_for"`
	NotFor      *string           `toml:"not_for"`
	RequestOnly *bool             `toml:"request_only"`
	Frontier    *bool             `toml:"frontier"`
	Enabled     *bool             `toml:"enabled"`
}

// Chains is the rules table for one Tier: a default chain plus category
// exceptions. TOML keys are category names; "default" is the fallback.
type Chains map[string][]string

// Price is USD per 1M tokens, standard tier only.
type Price struct {
	Input       float64 `toml:"input"`
	CachedInput float64 `toml:"cached_input"`
	Output      float64 `toml:"output"`
}

// Hook sets where the Adapter hook applies.
type Hook struct {
	Scope string `toml:"scope"` // projects | all
}

// Router is the `router` block.
type Router struct {
	Orchestrator string                        `toml:"orchestrator"`
	Backend      string                        `toml:"backend"` // rules | jev
	BaseURL      string                        `toml:"base_url"`
	Model        string                        `toml:"model"`
	Thresholds   map[string]map[string]float64 `toml:"thresholds"`
}

// Budget is the global `budget` block.
type Budget struct {
	StaleAfter      Duration `toml:"stale_after"`
	BlockAt         float64  `toml:"block_at"`
	WarnAt          float64  `toml:"warn_at"`
	AssumedHeadroom float64  `toml:"assumed_headroom"`
}

// Failure is the `failure` block.
type Failure struct {
	IdleLimit                Duration            `toml:"idle_limit"`
	WallLimit                map[string]Duration `toml:"wall_limit"`
	MaxDispatches            int                 `toml:"max_dispatches"`
	KeepAwake                *bool               `toml:"keep_awake"`
	CoolingPeriod            Duration            `toml:"cooling_period"`
	RunPauseAfterEscalations int                 `toml:"run_pause_after_escalations"`
	RunPauseAfterSameError   int                 `toml:"run_pause_after_same_error"`
}

// Concurrency is the `concurrency` block.
type Concurrency struct {
	MaxParallel   int    `toml:"max_parallel"`
	MaxPerAccount int    `toml:"max_per_account"`
	MaxTotal      int    `toml:"max_total"`
	CloseTabs     string `toml:"close_tabs"`
}

// Duration parses TOML strings such as "10m" or "1h".
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// Project is the shape of .agents/maestro.toml: five tighten-only keys plus
// the per-project Claude permission switch from the permission contract.
type Project struct {
	Forbid         []string `toml:"forbid"`
	PatternSetting string   `toml:"pattern_setting"`
	Checks         []string `toml:"checks"`
	Allow          []string `toml:"allow"`
	Router         string   `toml:"router"`
	Redact         []string `toml:"redact"`
	Councils       string   `toml:"councils"`
	ClaudeAuto     bool     `toml:"claude_auto"`
	// Dirs are extra writable directories for every edit Dispatch in this
	// project (a toolchain cache, for example). A Brief's dirs: adds to them.
	Dirs []string `toml:"dirs"`
}

// ExpandHome replaces a leading ~ with the user's home directory.
func ExpandHome(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// ParseFile decodes one TOML document into a File. Unknown keys are reported
// as warnings by the caller; they never stop a Route.
func ParseFile(src string) (*File, []string, error) {
	var f File
	md, err := toml.Decode(src, &f)
	if err != nil {
		return nil, nil, err
	}
	for id, account := range f.Accounts {
		account.defined = map[string]bool{}
		for _, field := range []string{"harness", "billing", "plan_price", "block_at", "reserve", "paid_overflow", "budget_source", "budget", "base_url"} {
			account.defined[field] = md.IsDefined("accounts", id, field)
		}
		for _, field := range []string{"usd", "period"} {
			account.defined["budget."+field] = md.IsDefined("accounts", id, "budget", field)
		}
		f.Accounts[id] = account
	}
	var unknown []string
	for _, k := range md.Undecoded() {
		unknown = append(unknown, k.String())
	}
	return &f, unknown, nil
}

// LoadUser reads the user file. A missing file is not an error: the embedded
// defaults alone make a working install.
func LoadUser() (*File, []string, error) {
	b, err := os.ReadFile(UserFile())
	if errors.Is(err, os.ErrNotExist) {
		return &File{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	f, unknown, err := ParseFile(string(b))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", UserFile(), err)
	}
	return f, unknown, nil
}

// LoadProject reads .agents/maestro.toml under root; missing is empty.
func LoadProject(root string) (*Project, error) {
	b, err := os.ReadFile(ProjectFile(root))
	if errors.Is(err, os.ErrNotExist) {
		return &Project{}, nil
	}
	if err != nil {
		return nil, err
	}
	var p Project
	if _, err := toml.Decode(string(b), &p); err != nil {
		return nil, fmt.Errorf("%s: %w", ProjectFile(root), err)
	}
	return &p, nil
}
