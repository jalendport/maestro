package brief

import (
	"regexp"
	"strings"
)

// Status values of a Report.
const (
	StatusDone     = "done"
	StatusPartial  = "partial"
	StatusBlocked  = "blocked"
	StatusUnshaped = "unshaped"
)

// Report is a parsed result.md.
type Report struct {
	Sections map[string]string
	Order    []string
	Status   string
	Position string // Council Seats end with POSITION instead
	Blocked  []string
	Raw      string
}

var statusRe = regexp.MustCompile(`(?mi)^\**STATUS\**:\s*\**(done|partial|blocked)\**\s*$`)
var positionRe = regexp.MustCompile(`(?mi)^\**POSITION\**:\s*\**(confirmed|changed|withdrawn)\**\s*$`)

// ParseReport reads a Report. A missing status line yields StatusUnshaped.
func ParseReport(text string) *Report {
	r := &Report{Raw: text, Order: SectionOrder(text), Status: StatusUnshaped}
	// The status and position lines sit after the last heading; keep them out of that section's body.
	r.Sections = SplitSections(positionRe.ReplaceAllString(statusRe.ReplaceAllString(text, ""), ""))
	if m := statusRe.FindAllStringSubmatch(text, -1); len(m) > 0 {
		r.Status = strings.ToLower(m[len(m)-1][1])
	}
	if m := positionRe.FindAllStringSubmatch(text, -1); len(m) > 0 {
		r.Position = strings.ToLower(m[len(m)-1][1])
	}
	r.Blocked = cleanList(listItems(r.Sections["Blocked"]))
	return r
}

var noCommandBlockedRe = regexp.MustCompile(`^no (command was|commands were) (denied|blocked)\.?$`)

// cleanList drops placeholders and statements that no command was denied or blocked.
func cleanList(items []string) []string {
	var out []string
	for _, it := range items {
		if l := strings.ToLower(strings.TrimSpace(it)); l == "none" || l == "(none)" || l == "n/a" || noCommandBlockedRe.MatchString(l) {
			continue
		}
		out = append(out, it)
	}
	return out
}
