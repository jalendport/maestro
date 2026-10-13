// Package harness holds one adapter per Harness: how to find it, read its
// catalog, build the one-shot and tab commands for a read or edit Dispatch, and parse
// its headless stream. A new Harness is a code change here, never config.
package harness

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Enforcement values, measured per Harness version.
const (
	Enforced   = "enforced"
	BestEffort = "best-effort"
	NoShape    = ""
)

// ErrNotReady marks an adapter that has a catalog but no launch shape yet.
var ErrNotReady = errors.New("this harness has no dispatch shape in this build")

// Model is one catalog entry.
type Model struct {
	ID            string   `json:"id"`
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
	Context       int      `json:"context,omitempty"`
}

// Launch is everything an adapter needs to build the command for one Dispatch.
type Launch struct {
	Model           string
	Effort          string
	Access          string
	Root            string
	Dirs            []string
	AllowRules      []string // project tool rules, edit only
	TaskID          string
	AllowedCommands []string // verification commands, allowed per edit Dispatch
	AllowCommit     bool
	RunFolder       string
	ReportPath      string
	Pointer         string
	Env             []string
	ClaudeAuto      bool
	InSolo          bool
	WallLimit       time.Duration // agy takes a print timeout
}

// Command is the built process plus the facts the ledger records.
type Command struct {
	Cmd          *exec.Cmd
	Flags        []string // the permission flags passed, for the ledger
	PromptStdin  bool     // the Pointer goes down stdin
	ReportByText bool     // the Lane cannot write result.md; its final message is the Report
}

// Tokens as a Harness reports them; nil counts stay null in the ledger.
type Tokens struct {
	Input       *int `json:"input"`
	CachedInput *int `json:"cached_input"`
	Output      *int `json:"output"`
	Reasoning   *int `json:"reasoning"`
}

// Denial is one Harness-reported permission denial.
type Denial struct {
	Tool  string `json:"tool"`
	Input string `json:"input,omitempty"`
}

// Window is one Budget window harvested from a stream.
type Window struct {
	Duration time.Duration `json:"duration,omitempty"`
	Kind     string        `json:"kind"`
	Used     float64       `json:"used"`
	ResetsAt time.Time     `json:"resets_at"`
}

// Signal classifies an error the stream reported.
type Signal struct {
	Class string // "limit" | "transient" | "lane"
	Text  string
}

// Outcome is what a parser knows when the process ends.
type Outcome struct {
	SessionID string
	ModelRan  string
	Text      string // the final assistant message
	IsError   bool
	ErrorText string
	Tokens    Tokens
	CostUSD   *float64
	Denials   []Denial
	Windows   []Window
	Signals   []Signal
}

// Parser consumes one stream line at a time.
type Parser interface {
	// Line reports whether the line counts as new output (resets the Idle limit).
	Line(line []byte) bool
	Outcome() *Outcome
}

// Harness is one adapter.
type Harness interface {
	Name() string
	Bin() string
	Version() (string, error)
	// Catalog returns the models the Harness can run today and a login hint.
	Catalog() ([]Model, string, error)
	// Enforcement for an Access value; NoShape when the Harness has no such shape.
	Enforcement(access string) string
	Build(l Launch) (*Command, error)
	BuildTab(l Launch) (*Command, error)
	NewParser() Parser
}

// Registry is the fixed set of adapters, keyed by Harness name.
type Registry struct {
	byName map[string]Harness
}

// New builds the registry; bins override the PATH lookup per Harness.
func New(bins map[string]string) *Registry {
	r := &Registry{byName: map[string]Harness{}}
	r.byName["claude"] = &Claude{bin: resolve("claude", bins["claude"])}
	r.byName["codex"] = &Codex{bin: resolve("codex", bins["codex"])}
	r.byName["grok"] = &Grok{bin: resolve("grok", bins["grok"])}
	r.byName["agy"] = &Agy{bin: resolve("agy", bins["agy"])}
	return r
}

// Get returns one adapter.
func (r *Registry) Get(name string) (Harness, bool) {
	h, ok := r.byName[name]
	return h, ok
}

// Names in display order.
func (r *Registry) Names() []string { return []string{"claude", "codex", "grok", "agy"} }

func resolve(name, override string) string {
	if override != "" {
		return override
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// Env builds a Lane's environment: the parent's minus the strip list, plus
// the fixed Maestro variables.
func Env(strip []string, set map[string]string) []string {
	drop := map[string]bool{}
	for _, k := range strip {
		drop[k] = true
	}
	var out []string
	for _, kv := range os.Environ() {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		if drop[k] {
			continue
		}
		if _, replaced := set[k]; replaced {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range set {
		out = append(out, k+"="+v)
	}
	return out
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// versionOf runs "<bin> --version" and returns the first x.y.z in its output.
func versionOf(bin string) (string, error) {
	if bin == "" {
		return "", errors.New("not installed")
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", err
	}
	m := versionRe.FindString(string(out))
	if m == "" {
		return "", fmt.Errorf("no version in %q", strings.TrimSpace(string(out)))
	}
	return m, nil
}

// VersionLess reports a < b for dotted numeric versions.
func VersionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func intp(v int) *int { return &v }
