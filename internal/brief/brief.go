// Package brief reads the Brief file the Orchestrator writes, renders the
// brief.md a Lane reads, and parses the Report a Lane writes back. One parser
// serves Brief files, Plan files, and Reports: a YAML front matter of short
// fields, then "## " headings with free text.
package brief

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Categories is the fixed list the Orchestrator may name.
var Categories = []string{"implement", "fix", "refactor", "spec", "research", "review", "prose", "triage", "second opinion", "large input"}

// Access values on a Brief.
const (
	AccessRead = "read"
	AccessEdit = "edit"
)

// DefaultAccess returns the category default: code categories edit, the rest read.
func DefaultAccess(category string) string {
	switch category {
	case "implement", "fix", "refactor":
		return AccessEdit
	}
	return AccessRead
}

// Fields is the front matter of a Brief file (and of a Plan file's Run-wide
// defaults and Task sections).
type Fields struct {
	Todo      int               `yaml:"todo,omitempty"`
	Title     string            `yaml:"title"`
	Category  string            `yaml:"category"`
	Level     string            `yaml:"level"`
	Access    string            `yaml:"access"`
	Dirs      []string          `yaml:"dirs"`
	Touch     []string          `yaml:"touch"`
	Checks    []string          `yaml:"checks"`
	Commit    *bool             `yaml:"commit"`
	Lane      string            `yaml:"lane"`
	Floor     string            `yaml:"floor"`
	Ceiling   string            `yaml:"ceiling"`
	WallLimit string            `yaml:"wall_limit"`
	Facts     map[string]string `yaml:"facts"`
	BlockedBy []string          `yaml:"blocked_by"`
}

// Brief is one parsed Brief file.
type Brief struct {
	Fields
	Goal    string
	Done    string
	Context string
	Files   []string
	Path    string
	// Sections keeps every heading as written, for headings the format does not name.
	Sections map[string]string
}

// Parse reads and validates a Brief file.
func Parse(path string) (*Brief, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	b, err := ParseText(string(src))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, _ := filepath.Abs(path)
	b.Path = abs
	return b, nil
}

// ParseText parses Brief file text.
func ParseText(src string) (*Brief, error) {
	front, body := SplitFrontMatter(src)
	b := &Brief{Sections: map[string]string{}}
	if front != "" {
		if err := yaml.Unmarshal([]byte(front), &b.Fields); err != nil {
			return nil, fmt.Errorf("front matter: %w", err)
		}
	}
	for name, text := range SplitSections(body) {
		b.Sections[name] = text
		switch strings.ToLower(name) {
		case "goal":
			b.Goal = text
		case "done":
			b.Done = text
		case "context":
			b.Context = text
		case "files":
			b.Files = listItems(text)
		}
	}
	if b.Title == "" {
		if h1 := firstH1(body); h1 != "" {
			b.Title = h1
		}
	}
	return b, b.Validate()
}

// Validate enforces the required fields and the fixed vocabularies.
func (b *Brief) Validate() error {
	var missing []string
	if strings.TrimSpace(b.Title) == "" {
		missing = append(missing, "title")
	}
	if strings.TrimSpace(b.Goal) == "" {
		missing = append(missing, "## Goal")
	}
	if strings.TrimSpace(b.Done) == "" {
		missing = append(missing, "## Done")
	}
	if len(missing) > 0 {
		return fmt.Errorf("brief is missing %s", strings.Join(missing, ", "))
	}
	if b.Category != "" && !contains(Categories, b.Category) {
		return fmt.Errorf("unknown category %q (one of: %s)", b.Category, strings.Join(Categories, ", "))
	}
	switch b.Access {
	case "", AccessRead, AccessEdit:
	default:
		return fmt.Errorf("access must be read or edit, not %q", b.Access)
	}
	if b.WallLimit != "" {
		if _, err := time.ParseDuration(b.WallLimit); err != nil {
			return fmt.Errorf("wall_limit: %w", err)
		}
	}
	return nil
}

// EffectiveAccess is the Brief field, else the category default.
func (b *Brief) EffectiveAccess() string {
	if b.Access != "" {
		return b.Access
	}
	return DefaultAccess(b.Category)
}

// WantsCommit reports the commit flag, default false.
func (b *Brief) WantsCommit() bool { return b.Commit != nil && *b.Commit }

// WallLimitDuration parses the optional wall_limit field.
func (b *Brief) WallLimitDuration() time.Duration {
	if b.WallLimit == "" {
		return 0
	}
	d, _ := time.ParseDuration(b.WallLimit)
	return d
}

// SplitFrontMatter separates a leading "---" block from the body.
func SplitFrontMatter(src string) (front, body string) {
	src = strings.TrimLeft(src, "\ufeff")
	if !strings.HasPrefix(src, "---\n") && !strings.HasPrefix(src, "---\r\n") {
		return "", src
	}
	rest := src[strings.Index(src, "\n")+1:]
	end := regexp.MustCompile(`(?m)^---[ \t]*\r?$`).FindStringIndex(rest)
	if end == nil {
		return "", src
	}
	return rest[:end[0]], rest[end[1]:]
}

var headingRe = regexp.MustCompile(`(?m)^## +(.+?)[ \t]*$`)

// SplitSections maps each "## " heading to the trimmed text under it.
func SplitSections(body string) map[string]string {
	out := map[string]string{}
	locs := headingRe.FindAllStringSubmatchIndex(body, -1)
	for i, loc := range locs {
		name := body[loc[2]:loc[3]]
		start := loc[1]
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		out[name] = strings.TrimSpace(body[start:end])
	}
	return out
}

// SectionOrder returns the headings in file order.
func SectionOrder(body string) []string {
	var out []string
	for _, m := range headingRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func firstH1(body string) string {
	m := regexp.MustCompile(`(?m)^# +(.+?)[ \t]*$`).FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// listItems reads "- item" or bare lines into a list, stripping backticks.
func listItems(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		if strings.HasPrefix(line, "`") && strings.HasSuffix(line, "`") && strings.Count(line, "`") == 2 {
			line = strings.Trim(line, "`")
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// sortedKeys is used for deterministic rendering of facts.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
