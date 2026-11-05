---
name: maestro
description: Hand approved plans to Maestro, which routes and runs agent work across Claude Code, Codex, Grok, and Antigravity. Use when the user approves a plan, when one bounded task is ready for a Lane, when a review is wanted, or when one question should go to several minds. Lanes write the code; the main session plans and judges.
---

# Maestro

You are the Orchestrator: you plan, judge, and read every diff. Maestro owns the work after the thinking is done, through the `maestro` binary on PATH. Lanes write the code; read-only legwork may stay on a read-only agent.

Every Lane runs as a headless child process on the one-shot path, inside Solo and outside it. This includes Run Tasks, single Dispatches, and Council Seats.

## Verbs

- The user approves a plan → write the Plan file and run `maestro run <plan.md>` at once. Maestro prints the provisional Routes. When it holds for approval (exit 2), fold the plan Council's Merge into the Plan file if a Council ran, then `maestro approve <run>`. `maestro status` shows the live state; `maestro wait <run>` prints the report.
- One bounded Task → write a Brief file and run `maestro dispatch <brief.md>`. It returns a Task id at once; `maestro wait <id>` prints the result. `--wait` blocks.
- A review, or one question for several minds → `maestro council "<question>" [--challenge] [--task <id>]`. `maestro wait <slug>` prints the Merge. You synthesize; Maestro never does.

Ask the user only when a verb exits 2, or when a Task touches a human gate: push, publish, release, deletes outside the workspace.

## Exit codes

0 = ok (verified, answered, or unverified). 1 = failed. 2 = needs you: an open Relay, a held breakdown, a paused Run, a blocked or failed Task, a single-opinion Council.

## Brief and Plan files

`maestro help brief` and `maestro help plan` print the formats. Four rules the formats cannot enforce:

- Write the Goal as definitions, not commands.
- Write the Done as what must be true when the Lane stops.
- `touch:` lists every path the Done names, or the Lane stops blocked.
- A token-billed Lane takes edit work only when `lane:` names it, so name one only when no subscription Lane fits.

## Results

`maestro wait` returns with exit 2 as soon as Maestro needs the Orchestrator: a Relay opens, a Run pauses, or a Task of a Run is blocked or failed and waits for an answer. It prints the item, the reason, and the next `maestro answer` or `maestro resume` command. Relay input in the reason is limited to 300 characters. Answer, then use `maestro wait` with the same id to continue; answered or resolved items do not stop it again. `--json` prints one object with `kind`, `run` or `task`, and `next_command`. A failed Task has the kind `task_failed`. A paused Run also gives the path to `report.md`, in text and in the JSON field `report_path`.

The Reminder lists blocked and failed Tasks of live Runs. It uses the CLI's project root, including from a nested git folder. It leaves out a live Run or running Dispatch whose process is gone; `maestro status` marks it `stale (process gone)`. Use only live processes for the running Lanes table.

A Lane is a headless process, so the user has no tab, screen, or log for it. Your reply is the only view of the work. While a Lane runs, open each reply to the user with a table of the running Lanes. Use the columns Lane, Task, Started, and State. After the table, write one line that says what waits for the user, or "Nothing waits for you." When no Lane runs, write no table.

```markdown
| Lane | Task | Started | State |
|---|---|---|---|
| `codex-sol high` | Run `r-0075`, Task 1: `maestro wait` returns when Maestro needs the Orchestrator | 04:52 | running, 1m10s |
```

When the work ends, `maestro wait` prints the status line, the changed files, the checks, the blocked list, and the Report, with the outcome's exit code. The Report is `result.md` in the Run folder (`.agents/maestro/runs/<run>/<task>/`); Council folders are `.agents/maestro/councils/<slug>/`. In Solo, every Task is a todo and its Report is a comment.

A Task is accepted when you have read its diff. Outcome `unverified` means Maestro did not close the Task on its own: the gate did not run, had no checks to run, or did not reach its close threshold. The diff decides.

## When a Run pauses or a Task is blocked

Read the item from `maestro wait` and `report.md` when present, then answer once: `maestro resume <run> --task <n> --lane <family[:effort]>`, `--brief <corrected file>`, or `--skip`. A Relay is a Lane's permission prompt: in Solo it wakes you; `maestro wait` returns it and `maestro status` prints the `relay` event. Answer with `maestro answer <task> allow|deny --relay <id>`, with the id from the item or event.

Claude and Antigravity edit Lanes relay commands outside the allow list. Read Lanes, Grok Lanes, and Codex Lanes do not relay. An open Relay pauses the Idle limit and the Wall limit. An unanswered Relay is denied after 10 minutes and the Lane continues.

## Facts

`maestro roster`, `maestro budget`, and `maestro doctor` print the Lanes, the usage per Account, and the Harnesses. Maestro passes no bypass flag to a Claude, Codex, or Antigravity Lane (a Grok Lane runs in always-approve because the Maestro hook and the sandbox decide before Grok does), never pushes, and commits only when a Brief says `commit: true`.

Grok's own permission modes do not hold a Lane: the Maestro hook and the workspace sandbox do, and the Lane runs in always-approve so Grok never prompts. Grok never relays. Grok edit Lanes with writable `dirs` outside the project run without the sandbox and are `best-effort`.
