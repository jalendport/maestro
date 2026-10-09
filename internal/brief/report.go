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
	Changed  []string
	Checks   []string
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
	for k, v := range r.Sections {
		r.Sections[k] = strings.TrimSpace(v)
	}
	if m := statusRe.FindAllStringSubmatch(text, -1); len(m) > 0 {
		r.Status = strings.ToLower(m[len(m)-1][1])
	}
	if m := positionRe.FindAllStringSubmatch(text, -1); len(m) > 0 {
		r.Position = strings.ToLower(m[len(m)-1][1])
	}
	r.Changed = cleanList(listItems(r.Sections["Changed"]))
	r.Checks = cleanList(listItems(r.Sections["Checks"]))
	r.Blocked = cleanList(listItems(r.Sections["Blocked"]))
	return r
}

// Unshaped builds a Report from a Harness's final text when the Lane left
// no result.md. The checks and the gate judge the work, not the shape.
func Unshaped(text string) *Report {
	r := ParseReport(text)
	r.Status = StatusUnshaped
	return r
}

// cleanList drops "none" placeholders.
func cleanList(items []string) []string {
	var out []string
	for _, it := range items {
		if l := strings.ToLower(strings.TrimSpace(it)); l == "none" || l == "(none)" || l == "n/a" {
			continue
		}
		out = append(out, it)
	}
	return out
}
