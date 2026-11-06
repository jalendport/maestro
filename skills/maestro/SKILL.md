---
name: maestro
description: Hand approved plans to Maestro, which routes and runs agent work across Claude Code, Codex, Grok, and Antigravity. Use when the user approves a plan, when one bounded task is ready for a Lane, when a review is wanted, or when one question should go to several minds. Lanes write the code; the main session plans and judges.
---

# Maestro

You are the Orchestrator: you plan, judge, and read every diff. Maestro owns the work after the thinking is done, through the `maestro` binary on PATH. Lanes write the code; read-only legwork may stay on a read-only agent.

Every Lane runs as a headless child process on the one-shot path, inside Solo and outside it. This includes Run Tasks, single Dispatches, and Council Seats.

Lanes never commit. Maestro commits a verified Task with the user's commit message template. The default is the title and a `Co-Authored-By` trailer for the Lane. A Run uses Worktree mode when Worktrees are on, the repository has a commit, the tree is clean, and the Plan has more than one edit Task. Other Runs use tree mode and run edit Tasks one at a time. The mode line gives the mode and its reason. In Worktree mode, Tasks with files that overlap run in Plan order. The Plan's `commit: true` keeps the Task commits on the user's branch; `commit: false` lands the changes uncommitted.

## Verbs

- The user approves a plan → write the Plan file and run `maestro run <plan.md>` at once. Maestro prints the provisional Routes. When it holds for approval (exit 2), fold the plan Council's Merge into the Plan file if a Council ran, then `maestro approve <run>`. `maestro status` shows the live state; `maestro wait <run>` prints the report.
- One bounded Task → write a Brief file and run `maestro dispatch <brief.md>`. It returns a Task id at once; `maestro wait <id>` prints the result. `--wait` blocks.
- A review, or one question for several minds → `maestro council "<question>" [--challenge] [--task <id>]`. `maestro wait <slug>` prints the Merge. You synthesize; Maestro never does.

Ask the user only when a verb exits 2, or when a Task touches a human gate: push, publish, release, deletes outside the workspace.

## Exit codes

0 = ok (verified, answered, or unverified). 1 = failed. 2 = needs you: an open Relay, a held breakdown, a paused Run, a blocked or failed Task, a single-opinion Council.

## Brief and Plan files

`maestro help brief` and `maestro help plan` print the formats. Four rules the formats cannot enforce:

- Write the Goal as definitions, not commands, and the Done as what must be true when the Lane stops.
- `level:` is your estimate. Jev sets the Tier. Write the level you believe; do not raise it to reach a stronger Lane, and do not name a Lane to reach one. Maestro uses a Frontier Lane when the work calls for it.
- `touch:` lists every path the Task may change, including the tests, fixtures, and docs the Done names, and `CHANGELOG.md` when the Done asks for an entry. A Lane that must change a file outside `touch` stops blocked, and Maestro sends the Task back to you with Cause `brief`.
- A token-billed Lane takes edit work only when `lane:` names it, so name one only when no subscription Lane fits.

A blocked Task comes back to you with its Cause: for `brief`, correct the Brief; for `environment`, resolve the blocker; for `capability`, a stronger Lane can help.

Size a Task for ten to twenty Lane minutes. Prefer four to six independent Tasks over fifteen small Tasks. Keep work on the same files in one Task, or chain the Tasks with `blocked_by`. Name every path in `Files`, so Maestro can order Tasks whose files overlap.

One nudge is one more attempt on the same Lane for the Done items that the gate scored under `0.5`; it runs when the checks pass, the verdict is `pass`, the risk is at most `medium`, and the score is under the close threshold.

Jev sees the `level` as context. The rules fallback uses it when Jev is absent or below the confidence floor. Set `floor:` and `ceiling:` in the Brief, or `maestro run --floor` and `--ceiling`, for hard Tier bounds. An edit never goes Light. The Reason line shows when Jev's score is below the estimate.

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

A Task is accepted when you have read its diff. Outcome `unverified` means Maestro did not close the Task on its own: the gate did not run, had no checks to run, had critical risk, or did not reach its close threshold. With passing checks, verdict `pass`, risk at most `medium`, and a score under the close threshold, Maestro gives the same Lane one nudge for Done items scored under `0.5`. With no such items, it ends `unverified` at once. Each Task gets at most one nudge. The nudge adds one attempt to the Dispatch and Run caps; its result follows the normal outcome rules. The Ledger marks that the nudge ran, and `maestro wait` prints the last attempt's Report. The diff decides.

## When a Run pauses or a Task is blocked

Read the Cause and the Lane's Blocked items. Cause `brief` means the Brief excludes required work or contradicts itself; correct the Brief. Cause `environment` means a tool or service was denied, down, or missing; resolve that blocker before you resume. Maestro does not re-Route these Tasks automatically. Cause `capability` can use a stronger Lane. A blocked Report still needs your answer, regardless of its Cause. If Jev is absent, a rule classifies a touch violation or blocked Report as `brief`, a partial Report with Blocked items as `environment`, and other failures as `capability`. The Run report, `maestro wait`, the Reminder, and the Solo wake-up give the Cause and the Blocked items.

Read the item from `maestro wait` and `report.md` when present, then answer once: `maestro resume <run> --task <n> --lane <family[:effort]>`, `--brief <corrected file>`, or `--skip`. A Relay is a Lane's permission prompt: in Solo it wakes you; `maestro wait` returns it and `maestro status` prints the `relay` event. Answer with `maestro answer <task> allow|deny --relay <id>`, with the id from the item or event.

A merge conflict gets one attempt in the Task's Worktree, then blocks with the paths and the Worktree location. In that Worktree, rebase the Task branch onto the Run branch, resolve the conflicts, and complete the rebase; then use `maestro resume <run> --task <n> --merge` to retry the merge with no new Lane. If landing the Run branch fails, correct the condition in the error (when the user's branch moved, rebase the Run branch onto it) and use `maestro resume <run> --finish`.

Claude and Antigravity edit Lanes relay commands outside the allow list. Read Lanes, Grok Lanes, and Codex Lanes do not relay. An open Relay pauses the Idle limit and the Wall limit. An unanswered Relay is denied after 10 minutes and the Lane continues.

## Facts

With `[failure] keep_awake = true` (the default), Maestro keeps the Mac awake for every Lane, Run, Council, and blocking verb. In hook scope, Reminder events in Claude Code, Codex, and Antigravity also protect the Orchestrator's turn. Each event refreshes one 30-minute lease per Harness process; `Stop`, process exit, or the lease limit ends it. An idle session holds no protection after that. Grok has no Reminder event, so it has no Orchestrator turn protection. Run `maestro adapter install` again to add `Stop`; `maestro doctor` checks it. The user must review and trust the new Codex hook in `/hooks`.

`maestro roster`, `maestro budget`, and `maestro doctor` print the Lanes, the usage per Account, and the Harnesses. Maestro passes no bypass flag to a Claude, Codex, or Antigravity Lane (a Grok Lane runs in always-approve because the Maestro hook and the sandbox decide before Grok does), and never pushes. In tree mode, Maestro commits only when a Brief says `commit: true`.

Grok's own permission modes do not hold a Lane: the Maestro hook and the workspace sandbox do, and the Lane runs in always-approve so Grok never prompts. Grok never relays. Grok edit Lanes with writable `dirs` outside the project run without the sandbox and are `best-effort`.
