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
level: some                  # your Judgment estimate: none | some | significant | expert
access: edit                 # read | edit; category default when absent (code categories edit, the rest read)
dirs: [../shared-fixtures]   # extra writable directories
touch: [internal/probe/**]   # scope guard; a diff outside it is a Bad result
checks: ["go test ./internal/probe/..."]   # added to the project checks in .agents/maestro.toml
# Project .agents/maestro.toml: allow = ["Bash(go run *)"] adds tool rules to edit Lanes only.
commit: false                # true lets the Lane commit as its own guidance says; never push
lane: codex-sol:high         # request a Lane; also floor: / ceiling:
wall_limit: 45m              # raise the Tier's Wall limit for this Task
facts: {input: very large log}   # short named facts for the Router
---

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

---
access: edit
commit: false
---
# Probe feature

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
- access: read
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
