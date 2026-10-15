---
name: maestro
description: Hand approved plans to Maestro, which routes and runs agent work across Claude Code, Codex, Grok, and Antigravity. Use when the user approves a plan, when one bounded task is ready for a Lane, when a review is wanted, or when one question should go to several minds. Lanes write the code; the main session plans and judges.
---

# Maestro

You are the Orchestrator: you plan, judge, and read every diff. Maestro owns the work after the thinking is done, through the `maestro` binary on PATH. Lanes write the code; read-only legwork may stay on a read-only agent.

## Verbs

- The user approves a plan → write the Plan file and run `maestro run <plan.md>` at once. Maestro prints the provisional Routes. When it holds for approval (exit 2), fold the plan Council's Merge into the Plan file if a Council ran, then `maestro approve <run>`. `maestro status` shows the live state; `maestro wait <run>` prints the report.
- One bounded Task → write a Brief file and run `maestro dispatch <brief.md>`. It returns a Task id at once; `maestro wait <id>` prints the result. `--wait` blocks.
- A review, or one question for several minds → `maestro council "<question>" [--challenge] [--task <id>]`. `maestro wait <slug>` prints the Merge. You synthesize; Maestro never does.

Ask the user only when a verb exits 2, or when a Task touches a human gate: push, publish, release, deletes outside the workspace.

## Exit codes

0 = ok (verified, answered, or unverified). 1 = failed. 2 = needs you: a held breakdown, a paused Run, a blocked Task, a single-opinion Council.

## Brief and Plan files

`maestro help brief` and `maestro help plan` print the formats. Five rules the formats cannot enforce:

- Write the Goal as definitions, not commands.
- Write the Done as what must be true when the Lane stops.
- In Solo, give each Task a `tab:` of at most 18 characters that says what the Task does; it names the Solo tab.
- `touch:` lists every path the Done names, or the Lane stops blocked.
- A token-billed Lane takes edit work only when `lane:` names it, so name one only when no subscription Lane fits.

## Results

`maestro wait` prints the status line, the changed files, the checks, the blocked list, and the Report. The Report is `result.md` in the Run folder (`.agents/maestro/runs/<run>/<task>/`); Council folders are `.agents/maestro/councils/<slug>/`. In Solo, every Task is a todo and its Report is a comment.

A Task is accepted when you have read its diff. Outcome `unverified` means the checks passed and the gate saw no fault but did not reach the close threshold: the diff decides.

## When a Run pauses or a Task is blocked

Read `report.md`, then answer once: `maestro resume <run> --task <n> --lane <family[:effort]>`, `--brief <corrected file>`, or `--skip`. A Relay is a Lane's permission prompt: in Solo it wakes you; outside Solo, `maestro wait` and `maestro status` print the `relay` event. Answer with `maestro answer <task> allow|deny --relay <id>`, with the id from that event.

## Facts

`maestro roster`, `maestro budget`, and `maestro doctor` print the Lanes, the usage per Account, and the Harnesses. Maestro passes no bypass flag to a Claude, Codex, or Antigravity Lane (a Grok Lane runs in always-approve because the Maestro hook and the sandbox decide before Grok does), never pushes, and commits only when a Brief says `commit: true`.

Grok's own permission modes do not hold a Lane: the Maestro hook and the workspace sandbox do, and the Lane runs in always-approve so Grok never prompts. Grok never relays. Grok edit Lanes with writable `dirs` outside the project run without the sandbox and are `best-effort`.
