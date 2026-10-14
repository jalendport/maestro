package brief

import (
	"fmt"
	"strings"
)

// RenderOpts is what the rendered brief.md needs beyond the Brief itself.
type RenderOpts struct {
	TaskID        string
	RunID         string
	Access        string
	ProjectChecks []string
	BriefChecks   []string
	ReportPath    string
	Retry         *Retry
	// ReportByMessage: the Lane cannot write files (Codex read-only sandbox);
	// its final message is the Report and the Harness writes it to the path.
	ReportByMessage bool
	// Council: the read shape ends with a POSITION line instead of STATUS.
	Council bool
}

// Retry is the "previous attempt" block on a retry Brief.
type Retry struct {
	Attempt  int
	Reason   string // Failure class or verdict
	Findings []string
}

// Pointer is the one-line prompt every Lane receives.
func Pointer(briefPath string) string {
	return fmt.Sprintf("Read the file `%s` in full, then follow it. Write your report as the file says.", briefPath)
}

// Render produces brief.md: Goal, Done, Context, Files, Checks, Rules, Report.
// The Lane never sees the Tier, the level, the reason, effort or cost words.
func Render(b *Brief, o RenderOpts) string {
	var w strings.Builder
	fmt.Fprintf(&w, "# %s\n\n", b.Title)
	fmt.Fprintf(&w, "Task %s · Run %s · Access: %s\n\n", o.TaskID, o.RunID, o.Access)
	fmt.Fprintf(&w, "## Goal\n\n%s\n\n", strings.TrimSpace(b.Goal))
	fmt.Fprintf(&w, "## Done\n\n%s\n\n", strings.TrimSpace(b.Done))
	if strings.TrimSpace(b.Context) != "" {
		fmt.Fprintf(&w, "## Context\n\n%s\n\n", strings.TrimSpace(b.Context))
	}
	if len(b.Files) > 0 {
		w.WriteString("## Files\n\nRead these first.\n\n")
		for _, f := range b.Files {
			fmt.Fprintf(&w, "- Read the file `%s`\n", f)
		}
		w.WriteString("\n")
	}
	checks := append(append([]string{}, o.ProjectChecks...), o.BriefChecks...)
	if len(checks) > 0 {
		w.WriteString("## Checks\n\nRun these before you report. Record each command and its result under `## Checks` in your report.\n\n")
		for _, c := range checks {
			fmt.Fprintf(&w, "- `%s`\n", c)
		}
		w.WriteString("\n")
	}
	w.WriteString("## Rules\n\n")
	w.WriteString("- Do not push.\n")
	if b.WantsCommit() {
		w.WriteString("- Commit as your own commit guidance says. Stage explicit paths, never `git add -A`. Never push.\n")
	} else {
		w.WriteString("- Do not commit.\n")
	}
	if len(b.Touch) > 0 {
		fmt.Fprintf(&w, "- Change nothing outside these paths: %s\n", strings.Join(quoteAll(b.Touch), ", "))
	}
	if o.Access == AccessRead {
		w.WriteString("- This is a read task: write nothing in the repository except your report.\n")
	}
	if o.Access == AccessEdit {
		w.WriteString("- Edit files with your editor tools (Write, Edit), not with shell scripts or heredocs; run the checks exactly as listed, without pipes.\n")
	}
	if o.Access == AccessRead || o.Council {
		w.WriteString("- Report every blocked or denied command in `## Concerns`.\n")
	} else {
		w.WriteString("- Report every blocked or denied command in `## Blocked`.\n")
	}
	if !o.Council {
		w.WriteString("- When the Done cannot be met, stop and report `STATUS: blocked` or `STATUS: partial`.\n")
	}
	w.WriteString("\n")
	if o.Council {
		w.WriteString(CouncilShape(o.ReportPath, o.ReportByMessage))
	} else {
		w.WriteString(ReportShape(o.Access, o.ReportPath, o.ReportByMessage))
	}
	if o.Retry != nil {
		fmt.Fprintf(&w, "\n## Previous attempt\n\nThis is attempt %d. A previous attempt stopped partway (%s). Its changes are in the working tree. Check them, then keep or discard them.\n", o.Retry.Attempt, o.Retry.Reason)
		if len(o.Retry.Findings) > 0 {
			w.WriteString("\nConfirmed findings from the last attempt:\n\n")
			for _, f := range o.Retry.Findings {
				fmt.Fprintf(&w, "- %s\n", f)
			}
		}
	}
	return w.String()
}

// ReportShape is the Report section of brief.md for one Access value.
func ReportShape(access, reportPath string, byMessage bool) string {
	var w strings.Builder
	if byMessage {
		w.WriteString("## Report\n\nYour final message is your report. It must contain exactly these headings, in this order, and end with the status line. Do not try to write a file.\n\n")
	} else {
		fmt.Fprintf(&w, "## Report\n\nWhen you finish, write the file `%s` with exactly these headings, in this order, and end with the status line.\n\n", reportPath)
	}
	if access == AccessRead {
		w.WriteString("```markdown\n## Answer\n(your answer)\n\n## Findings\n- `file:line` — one finding, with a confidence word (high, medium, low)\n\n## Concerns\n- (anything the answer rests on that you could not verify)\n\nSTATUS: done\n```\n")
	} else {
		w.WriteString("```markdown\n## Summary\n(what you did, two to five lines)\n\n## Changed\n- path/to/file\n\n## Checks\n- `command` — ok | failed: (one line)\n\n## Blocked\n- (denied or impossible commands; or \"none\")\n\n## Deviations\n- (where you departed from the brief; or \"none\")\n\n## Open\n- (questions; or \"none\")\n\nSTATUS: done\n```\n")
	}
	w.WriteString("\nThe status line is one of `STATUS: done` (the Done is met), `STATUS: partial` (some of it), or `STATUS: blocked` (you cannot continue without a person).\n")
	return w.String()
}

func quoteAll(xs []string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = "`" + x + "`"
	}
	return out
}

// CouncilShape is the Report section for a Council Seat.
func CouncilShape(reportPath string, byMessage bool) string {
	var w strings.Builder
	if byMessage {
		w.WriteString("## Report\n\nYour final message is your report. It must contain exactly these headings, in this order, and end with the position line. Do not try to write a file.\n\n")
	} else {
		fmt.Fprintf(&w, "## Report\n\nWhen you finish, write the file `%s` with exactly these headings, in this order, and end with the position line.\n\n", reportPath)
	}
	w.WriteString("```markdown\n## Answer\n(your answer; when nothing real is wrong, say so)\n\n## Findings\n- `file:line` — one finding, with a confidence word (high, medium, low)\n\n## Concerns\n- (anything the answer rests on that you could not verify)\n\nPOSITION: confirmed\n```\n")
	w.WriteString("\nThe position line is one of `POSITION: confirmed` (you stand by your answer), `POSITION: changed` (you refined it after reading the other answers), or `POSITION: withdrawn` (you no longer hold it).\n")
	return w.String()
}
