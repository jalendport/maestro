package cli

import (
	"fmt"
	"os"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/spf13/cobra"
)

const briefHelp = `A Brief file is Markdown: a YAML front matter of short fields, then fixed headings.
Required: title, ## Goal, ## Done. Everything else is optional.

---
title: Add the probe command
category: implement          # implement | fix | refactor | spec | research | review | prose | triage | second opinion | large input
level: some                  # Orchestrator's estimate: none | some | significant | expert
access: edit                 # read | edit; category default when absent (code categories edit, the rest read)
dirs: [../shared-fixtures]   # extra writable directories
touch: [internal/probe/**]   # scope guard; a diff outside it is a Bad result
checks: ["go test ./internal/probe/..."]   # added to the project checks in .agents/maestro.toml
# Project .agents/maestro.toml: allow = ["Bash(go run *)"] adds tool rules to edit Lanes only.
commit: false                # true lets Maestro commit after verification; the Lane never commits
lane: codex-sol:high         # request a Lane; also floor: / ceiling:
wall_limit: 45m              # raise the Tier's Wall limit for this Task
facts: {input: very large log}   # short named facts for the Router
todo: 247                    # adopt this Solo todo for the Task instead of creating one
---

Jev's Judgment level sets the Tier. The Brief's level is context for Jev and
the level for the rules fallback when Jev is absent or below the confidence
floor. It does not raise Jev's score or limit its pick. Only floor: and
ceiling:, or maestro run --floor and --ceiling, set hard Tier bounds.
An edit never goes Light. The Reason line shows when Jev's score is below
the estimate, for example: jev some, orchestrator estimated expert.

For a single Dispatch, commit: true lands one commit on your branch after the gate; commit: false lands the changes uncommitted.

## Goal
What to do, as definitions, not commands.

## Done
What must be true when the Lane stops. One line per item.

## Context
Free text. Optional.

## Files
- path/to/read/first.go
`

const planHelp = `A Plan file is one Markdown file for maestro run: a front matter of Run-wide
defaults (any Brief field), an optional "# title", then one "## <task title>"
section per Task. Each section may start with a "- key: value" list of the
same fields as a Brief plus blocked_by (plan numbers or titles), then
"### Goal", "### Done", "### Context", "### Files".

The text after the front matter or optional "# title" and before the first
"## <task title>" is the Plan's introduction. Maestro adds it to the start
of every Task's Context under "Shared context of the Plan:", before the
Task's own "### Context" text. An empty introduction adds nothing.

A Plan with two or more edit Tasks gets a Plan check before it starts; --no-plan-check skips it for one Run.

In Worktree mode, Maestro commits each verified edit Task with the user's
commit message template (default: the title and a Co-Authored-By trailer
for the Lane). Lanes never commit. At the end of the Run, the Plan's
commit: true keeps the Task commits on the user's branch; commit: false
leaves the changes uncommitted. In tree mode, commit: true lets Maestro
commit the Task's changed paths after verification; false leaves them
uncommitted.

---
access: edit
commit: false
---
# Probe feature

The probe uses the existing output format.

## Add the probe command
- category: implement
- level: some
- touch: [internal/probe/**]

### Goal
Add a probe verb that prints ok.

### Done
- ` + "`maestro probe`" + ` prints ok.

## Document the probe command
- category: prose
- access: edit
- blocked_by: [1]

### Goal
Describe the probe verb in the README.

### Done
- The README has a Probe section.
`

func (a *app) helpTopicCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "help [brief|plan|report]",
		Short: "Print the Brief file format or the Report shapes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				a.renderHelp()
				return nil
			}
			switch args[0] {
			case "brief":
				fmt.Fprint(os.Stdout, briefHelp)
			case "plan":
				fmt.Fprint(os.Stdout, planHelp)
			case "report":
				fmt.Fprintln(os.Stdout, "edit shape (Summary / Changed / Checks / Blocked / Deviations / Open, then STATUS):")
				fmt.Fprintln(os.Stdout)
				fmt.Fprint(os.Stdout, brief.ReportShape(brief.AccessEdit, "<run folder>/result.md", false))
				fmt.Fprintln(os.Stdout)
				fmt.Fprintln(os.Stdout, "read shape (Answer / Findings / Concerns, then STATUS; a Council Seat ends with POSITION instead):")
				fmt.Fprintln(os.Stdout)
				fmt.Fprint(os.Stdout, brief.ReportShape(brief.AccessRead, "<run folder>/result.md", false))
			default:
				return fmt.Errorf("no help topic %q (brief, plan, report)", args[0])
			}
			return nil
		},
	}
}
