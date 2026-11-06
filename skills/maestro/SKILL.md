---
name: maestro
description: Hand work to Maestro when the user approves a plan, when one bounded Task is ready, when the user wants a review, or when one question should go to several minds.
---

# Maestro

You are the Orchestrator: you plan, hand the work to Maestro through the `maestro` binary on PATH, answer what Maestro returns, and read every diff. Lanes on Claude Code, Codex, Grok, and Antigravity write the code. Read-only legwork can stay on a read-only agent.

## Work loop

Do these steps in order. Each step ends when its "Done when" condition is true.

1. **Write the input.** For an approved plan, write a Plan file. For one bounded Task, write a Brief file. For a review or one question for several minds, write the question. Use [Brief and Plan files](#brief-and-plan-files). Done when the file or the question exists and every Task passes each of the four rules.
2. **Start the verb.** When the user approves a plan, start it at once.
   - Plan file: `maestro run <plan.md>`. It prints the mode line and the provisional Routes.
   - Brief file: `maestro dispatch <brief.md>`. It returns a Task id at once. `--wait` blocks until the result.
   - Question: `maestro council "<question>" [--challenge] [--task <id>]`.

   Done when the verb printed a Run id, a Task id, or a Council slug. When the verb exits 2, go to step 4.
3. **Wait.** Run `maestro wait <run|task|slug>`. While a Lane runs, open each reply to the user with the [running-Lanes table](#running-lanes-table). Done when `maestro wait` returns.
4. **Answer exit 2.** When a verb exits 2, answer the item as [Answering exit 2](#answering-exit-2) says. Then do step 3 again with the same id. Done when `maestro wait` exits 0 or 1.
5. **Read every diff.** Read the diff of each Task, whatever its outcome. A Task is accepted only when you have read its diff. For a Council, `maestro wait <slug>` prints the Merge, and you write the synthesis. Use [Reading results](#reading-results). Done when you have read every diff, or synthesized the Merge, and told the user the result.

## Running-Lanes table

Each Lane is a headless child process, in Solo and outside it. This includes Run Tasks, single Dispatches, and Council Seats. The user has no tab, screen, or log for a Lane, so your reply is the only view of the work.

While a Lane runs, open each reply to the user with a table of the running Lanes, with the columns Lane, Task, Started, and State. After the table, write one line that says what waits for the user, or "Nothing waits for you." Put only live processes in the table: `maestro status` shows the live state and marks a Run or Dispatch whose process is gone as `stale (process gone)`. When no Lane runs, write no table.

```markdown
| Lane | Task | Started | State |
|---|---|---|---|
| `codex-sol high` | Run `r-0075`, Task 1: `maestro wait` returns when Maestro needs the Orchestrator | 04:52 | running, 1m10s |
```

## Guardrails

- Ask the user only when a verb exits 2, or when a Task reaches a human gate: push, publish, release, or deletes outside the workspace. In all other cases, decide and continue the work loop. Maestro never pushes.
- Quote braces in read-only commands, for example `git log '@{u}'..`.

## Brief and Plan files

`maestro help brief` and `maestro help plan` print the formats. Four rules the formats cannot enforce:

- Write the Goal as definitions of the work, and the Done as checkable conditions: what must be true when the Lane stops.
- `level:` is your estimate. Jev sets the Tier and uses the estimate as context, and uses a Frontier Lane when the work calls for it. Write the level you believe. For hard Tier bounds, set `floor:` and `ceiling:` in the Brief, or `maestro run --floor` and `--ceiling`. `maestro dispatch --level` and `maestro run --level` replace the estimate.
- `touch:` lists the paths the Task may change. List every path the Task needs, including fixtures, docs, and `CHANGELOG.md` when the Done asks for an entry. A Lane that must change a file outside `touch` stops blocked, and the Task comes back to you with Cause `brief`. Test files are always in scope. A Brief that expects a test edit outside `touch` says so in its Context and names the test file; otherwise the gate scores the edit as scope creep and Maestro does not land the Task.
- A token-billed Lane takes edit work only when `lane:` names it. Name a token-billed Lane only when no subscription Lane fits.

Size each Task for ten to twenty Lane minutes. Four to six independent Tasks with disjoint `Files` end a Run in 20 to 40 minutes; fifteen small Tasks do not. Keep work on the same files in one Task, or chain the Tasks with `blocked_by`. In `Files`, give the precise list of code files that the Task changes, so that Maestro can order Tasks whose `Files` overlap.

## Plans for Worktree mode

Parallel Tasks in Worktrees merge at the end of each Task. Write the Plan so that they do not conflict:

- With the default commit template, a Task title is the commit subject. Write it the way your project writes commit subjects.
- Make `touch` wide: the whole packages the Task changes, every test file that asserts the behavior (in other packages too), and every doc file the shared rules name (`docs/**`, `CONTEXT.md`, `skills/maestro/**`). A doc that `touch` leaves out costs one blocked attempt and one resume.
- In a Plan with more than one edit Task, keep `CHANGELOG.md` and `README.md` with you: parallel edits to the same section conflict at every merge. Tell each Lane to put its changelog line in the Summary of its Report. After the Run, write `CHANGELOG.md` and the README once.
- Put a change that removes or renames an exported function in the same Task as every caller. Otherwise the build fails inside the Task's `touch`.
- The Report has fixed headings. Ask for extra Report content under an existing heading, such as the Summary.

## Worktree mode and landing

Worktree mode is the default; the mode line names the mode, and the reason for tree mode. `maestro approve` chooses the mode again from the current Plan. The mode stays the same on resume.

Each edit Task works in its own Worktree, which starts from the HEAD commit. The main checkout changes only at landing. Each live Run has its own Run branch and base, so Worktree-mode Runs can share a project: `another Run is live: <id>` is information and does not hold the new Run. A read Task with `blocked_by` on edit Tasks starts at the Run branch head and sees their merged work.

Tree mode needs the checkout to itself and runs edit Tasks one at a time.

Maestro commits each verified Task; a Lane leaves its changes uncommitted. In Worktree mode, the Plan's `commit: true` keeps the Task commits on the user's branch, and `commit: false` lands the changes uncommitted and leaves the user's own staged work staged. In tree mode, Maestro commits only when the Brief says `commit: true`.

### Landing

Before a Worktree-mode Run or a standalone edit Dispatch lands, Maestro runs the project's `checks` once on the merged Run branch, after any landing rebase. A failed landing check keeps the Run branch and every Task commit. Use the [fix flow](#fix-flow).

A paused landing ends with a retry: `maestro resume <run> --finish` for a Run, or `maestro dispatch --finish <task>` for a standalone Dispatch. A commit on the user's branch while a landing check runs stops that landing with "landing branches changed"; retry with `maestro dispatch --finish <task>` or `maestro resume <run> --finish`. Before the retry, recover by hand:

- **Landing conflict.** When the user's branch gained commits, landing rebases the Run branch's own commits onto it. A conflict pauses the Run and leaves the Run branch intact. On the Run branch, resolve the conflict and complete the rebase onto the user's branch. Then return to the user's branch.
- **User branch not checked out.** Check out that branch.
- **Dirty-tree overlap.** A dirty tree lands when its dirty paths do not overlap the Run. On overlap, commit or stash those paths; Maestro leaves the user's work as it is.

### Fix flow

After a failed landing check:

- **Run.** `maestro add <brief>` queues a fix Task on the paused Run. When more than one Run can take the Task, name the Run: `maestro add <brief> --run <id>`. Then `maestro resume <run> --finish` runs the fix and lands.
- **Standalone edit Dispatch.** Put the fix in the kept Worktree that `maestro wait` names, then use `maestro dispatch --finish <task>`.

### Merge conflicts

A merge conflict gets one attempt in the Task's Worktree. Then the Task blocks. To recover, rebase the Task branch onto the Run branch in that Worktree, resolve the conflicts, and complete the rebase. Then `maestro resume <run> --task <n> --merge` retries the merge with no new Lane.

## Plan check

Before a Run with two or more edit Tasks starts, Maestro runs a Plan check: one quick read Lane checks the Plan's paths, symbols, scope, and order against the project tree.

- Findings hold the Run. Correct the Plan file, then use `maestro approve <run>`.
- A failed Plan check lets the Run start.

## Answering exit 2

`maestro wait` returns with exit 2 as soon as Maestro needs you. Read the item it prints, and `report.md` when it gives that path. Answer once with the printed `maestro answer`, `maestro resume`, or `maestro approve` command, then use `maestro wait` with the same id. Answered or resolved items do not stop it again. `maestro wait <id> --json` prints one object with the item's `kind` and `next_command`.

`maestro wait <task>` follows only that Task in its Run.

### Held Run

When a Run holds for approval, fold the plan Council's Merge into the Plan file if a Council ran. Then use `maestro approve <run>`.

### Causes

Read the Cause and the Lane's Blocked items. The Run report names the Worktree of each blocked or failed Task.

- `brief`: the Brief excludes required work or contradicts itself. Correct the Brief, then resume with `--brief`.
- `environment`: a tool or service was denied, down, or missing. Resolve that blocker, then resume.
- `none`: nothing went wrong. On a Bad result, judge the result, then resume.
- `capability`: the Lane's work is wrong or incomplete. This is the only Cause that can re-Route a Bad result to a stronger Lane.

A blocked Report needs your answer, whatever its Cause.

### Resume

Answer a paused Run or a blocked Task with one command:

- `maestro resume <run> --task <n> --lane <family[:effort]>`
- `maestro resume <run> --task <n> --brief <corrected file>`
- `maestro resume <run> --task <n> --skip`

### Relay

A Relay is a Lane's permission prompt. Claude and Antigravity edit Lanes relay commands outside the allow list; all other Lanes do not relay. In Solo, a Relay wakes you. Answer with `maestro answer <task> allow|deny --relay <id>`, with the id from the item or the `relay` event in `maestro status`. An unanswered Relay is denied after 10 minutes, and the Lane continues without that step. Answer each open Relay before you ask the user a question, because a Relay can reach that limit while you wait for the user.

## Reading results

- Outcome `unverified` means Maestro did not close the Task on its own. The diff decides.
- `maestro wait` names each test file that a Lane changed outside its own `touch`. Read those diffs for weakened assertions.
- The Report is `result.md` in the Run folder (`.agents/maestro/runs/<run>/<task>/`). In Solo, every Task is a todo, and its Report is a comment.

## Exit codes

- 0: ok (verified, answered, or unverified).
- 1: failed.
- 2: needs you: an open Relay, a held breakdown, a paused Run, a blocked or failed Task, or a single-opinion Council.
