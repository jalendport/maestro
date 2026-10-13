package council

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jalendport/maestro/internal/ui"
)

// PrintMerge writes the Merge.
func PrintMerge(c *Council, asJSON bool) {
	if asJSON {
		b, _ := json.MarshalIndent(c, "", "  ")
		fmt.Fprintln(os.Stdout, string(b))
		return
	}
	fmt.Fprint(os.Stdout, Merge(c))
}

// Merge renders the Merge as text; the same text goes on a Solo todo comment.
func Merge(c *Council) string {
	var w strings.Builder
	answered := 0
	for _, s := range c.Seats {
		if r := last(s); r != nil && r.Status == "ok" {
			answered++
		}
	}
	header := fmt.Sprintf("council %s  %s  %d of %d seats answered", c.Slug, titleCase(string(c.Tier)), answered, len(c.Seats))
	if c.RoundsRun > 1 {
		header = fmt.Sprintf("council %s  %s  %d rounds  %d of %d seats answered", c.Slug, titleCase(string(c.Tier)), c.RoundsRun, answered, len(c.Seats))
	}
	if !c.EndedAt.IsZero() {
		header += "  " + ui.Duration(c.EndedAt.Sub(c.StartedAt))
	}
	if c.SingleOpinion {
		header += "  (single opinion, not a council)"
	}
	w.WriteString(header + "\n")
	fmt.Fprintf(&w, "  %-16s %-8s", "seat", "status")
	for r := 1; r <= max(1, c.RoundsRun); r++ {
		fmt.Fprintf(&w, " %-10s", fmt.Sprintf("round %d", r))
	}
	w.WriteString(" relaxed\n")
	for _, s := range c.Seats {
		status := "—"
		if r := last(s); r != nil {
			status = r.Status
		}
		name := s.Family + " " + s.Effort
		fmt.Fprintf(&w, "  %-16s %-8s", name, status)
		for r := 1; r <= max(1, c.RoundsRun); r++ {
			pos := "—"
			if sr := seatRound(s, r); sr != nil {
				if sr.Status == "ok" {
					pos = sr.Position
				} else {
					pos = sr.Status
				}
			}
			fmt.Fprintf(&w, " %-10s", pos)
		}
		tags := []string{}
		if s.Author {
			tags = append(tags, "author")
		}
		if s.Requested {
			tags = append(tags, "requested")
		}
		if r := last(s); r != nil && r.Status != "ok" && r.Error != "" {
			tags = append(tags, r.Error)
		}
		w.WriteString(" " + strings.Join(tags, ", ") + "\n")
	}
	if len(c.Relaxed) > 0 {
		fmt.Fprintf(&w, "relaxed  %s\n", strings.Join(c.Relaxed, "; "))
	}
	if len(c.Findings) > 0 {
		w.WriteString("findings (jev p ≥ 0.50)\n")
		for _, f := range c.Findings {
			fmt.Fprintf(&w, "  %-14s %s  (%.2f)\n", f.Seat, f.Text, f.Probability)
		}
	} else {
		w.WriteString("findings  (none)\n")
	}
	if len(c.Filtered) > 0 {
		w.WriteString("filtered (below 0.50)\n")
		for _, f := range c.Filtered {
			fmt.Fprintf(&w, "  %-14s %s  (%.2f)\n", f.Seat, f.Text, f.Probability)
		}
	}
	if c.FilterSkipped != "" {
		fmt.Fprintf(&w, "filter   skipped (%s)\n", c.FilterSkipped)
	}
	var paths []string
	for _, s := range c.Seats {
		if r := last(s); r != nil && r.Report != "" {
			paths = append(paths, r.Report)
		}
	}
	if len(paths) > 0 {
		fmt.Fprintf(&w, "reports  %s\n", strings.Join(paths, "  "))
	}
	return w.String()
}
