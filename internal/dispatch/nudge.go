package dispatch

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/jalendport/maestro/internal/brief"
)

var doneItemRE = regexp.MustCompile(`(?m)^([ \t]*)(?:[-*+]|[0-9]+[.)])[ \t]+`)

func doneItems(text string) []string {
	text = strings.Trim(text, "\r\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	matches := doneItemRE.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return []string{strings.TrimSpace(text)}
	}
	indent := len(text)
	for _, match := range matches {
		indent = min(indent, match[3]-match[2])
	}
	var top [][]int
	for _, match := range matches {
		if match[3]-match[2] == indent {
			top = append(top, match)
		}
	}
	var items []string
	for i, match := range top {
		end := len(text)
		if i+1 < len(top) {
			end = top[i+1][0]
		}
		items = append(items, strings.TrimSpace(text[match[1]:end]))
	}
	return items
}

func nudgeFindings(env *Env, t *Task, rec *Record, b *brief.Brief) []string {
	if t.Nudged || rec.Outcome.Outcome != OutcomeUnverified || len(rec.Verification.Checks) == 0 {
		return nil
	}
	for _, check := range rec.Verification.Checks {
		if !check.OK || check.Skipped {
			return nil
		}
	}
	var gate Gate
	if json.Unmarshal(rec.Verification.Gate, &gate) != nil || gate.Skipped != "" || gate.Verdict != "pass" || gate.Satisfies >= env.Roster.Threshold("satisfies") {
		return nil
	}
	switch gate.Risk {
	case "none", "low", "medium":
	default:
		return nil
	}
	var findings []string
	for i, item := range doneItems(b.Done) {
		if score, ok := gate.DoneItems[i+1]; ok && score < 0.5 {
			findings = append(findings, fmt.Sprintf("Done item %d: %s", i+1, item))
		}
	}
	return findings
}
