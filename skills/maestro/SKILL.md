---
name: maestro
description: Hand finished plans to Maestro, the local magic box that routes, dispatches, checks, and reviews agent work across Claude Code, Codex, Grok, and Antigravity. Use when work is planned and ready to be done by a lane, when several lanes should answer one question, or when a review is wanted. Do not write feature code in the main session; dispatch it. Hands off on its own when a plan is finished.
---

# Maestro

You are the Orchestrator. You plan and judge; Maestro owns the work after the thinking is done. Maestro is the `maestro` binary on PATH.

## When to use which verb

- A planning session (a grilling, a spec, a plan) ends with a finished plan → write the Plan file and run it at once, without being asked. Ask the user only when a verb exits 2.
- One bounded Task → write a Brief file, then `maestro dispatch <brief.md>`. It returns at once with a Task id; `maestro wait <id>` prints the result. `--wait` blocks.
- Several Tasks with an order → write a Plan file, then `maestro run <plan.md>`. Maestro prints the provisional Routes; when it holds for approval (exit 2), read the plan Council's Merge, edit the Plan file if needed, then `maestro approve <run>`. `maestro status` shows the live state; `maestro wait <run>` prints the report.
- A question for several minds, or a review → `maestro council "<question>" [--challenge] [--task <id>]`. `maestro wait <slug>` prints the Merge. You synthesize; Maestro never does.
- Never spawn a code-writing sub-agent or Solo lane yourself; the Adapter hook denies it. Read-only legwork may stay on a read-only agent.

## Exit codes

0 = ok (verified, answered, or unverified). 1 = failed. 2 = needs you: a held breakdown, a paused Run, a blocked Task, a single-opinion Council.

## Brief file

`maestro help brief` prints the format. Required: `title`, `## Goal`, `## Done`. Write the Goal as definitions, not commands; write the Done as what must be true when the Lane stops. The `touch:` list includes every path the Done names, or the Lane stops blocked. Optional: `category` (implement, fix, refactor, spec, research, review, prose, triage, second opinion, large input), `level` (none, some, significant, expert), `access` (read, edit), `touch:` (scope guard globs), `dirs:` (extra writable directories), `checks:`, `commit: true`, `lane:` (also `floor:` and `ceiling:`), `wall_limit:`, `facts:`, `todo:` (adopt an existing Solo todo), `## Files`.

## Plan file

`maestro help plan` prints the format: a front matter of Run-wide defaults, then one `## <task title>` section per Task with a `- key: value` list (the Brief fields plus `blocked_by`), then `### Goal`, `### Done`, optional `### Context`, `### Files`.

## Results

A Task's Report is `result.md` in its Run folder (`.agents/maestro/runs/<run>/<task>/`). Council folders live under `.agents/maestro/councils/<slug>/`. `maestro wait` prints the status line, the changed files, the checks, the blocked list, and the Report. Outcome `unverified` means the deterministic checks passed and the gate saw no fault but did not reach the close threshold: read the diff before you accept it. In Solo, every Task is a todo and its Report is a comment.

## When a Run pauses or a Task is blocked

Read `report.md`, then answer once: `maestro resume <run> --task <n> --lane <family[:effort]>`, `--brief <corrected file>`, or `--skip`. In Solo, a Relay wakes the Orchestrator with the permission request. Outside Solo, `maestro wait` and `maestro status` print the `relay` event. In both cases, answer with `maestro answer <task> allow|deny`. Ask the user for anything that touches their human gates: push, publish, release, deletes outside the workspace.

## Facts

`maestro roster` shows the families and their state; `maestro budget` the usage per Account; `maestro doctor` the Harnesses, the Jev key, and Solo. Maestro never passes a bypass flag to a Lane, never pushes, and commits only when a Brief says `commit: true`.
