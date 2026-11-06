---
name: maestro
description: Maestro is a local tool that routes agent work to Lanes on Claude Code, Codex, Grok, and Antigravity, runs the work, and checks it. Use it when the user approves a plan, when one bounded Task is ready, when the user wants a review, or when one question should go to several minds.
---

# Maestro

You are the Orchestrator: you plan, hand the work to Maestro through the `maestro` binary on PATH, answer what Maestro returns, and read every diff. Lanes write the code. Read-only legwork can stay on a read-only agent.

## Work loop

Do these steps in order. Each step ends when its "Done when" condition is true.

1. **Write the input.** For an approved plan, write a Plan file. For one bounded Task, write a Brief file. For a review or one question for several minds, write the question. Use [Brief and Plan files](#brief-and-plan-files). Done when the file or the question is ready and each Task obeys the four rules.
2. **Start the verb.** When the user approves a plan, start it at once.
   - Plan file: `maestro run <plan.md>`. It prints the mode line and the provisional Routes.
   - Brief file: `maestro dispatch <brief.md>`. It returns a Task id at once. `--wait` blocks until the result.
   - Question: `maestro council "<question>" [--challenge] [--task <id>]`.

   Done when the verb printed a Run id, a Task id, or a Council slug. When the verb exits 2, go to step 4.
3. **Wait.** Run `maestro wait <run|task|slug>`. While a Lane runs, open each reply to the user with the [running-Lanes table](#running-lanes-table). Done when `maestro wait` returns.
4. **Answer exit 2.** When a verb exits 2, answer the item as [Answering exit 2](#answering-exit-2) says. Then do step 3 again with the same id. Done when `maestro wait` exits 0 or 1.
5. **Read every diff.** Read the diff of each Task, whatever its outcome. A Task is accepted only when you have read its diff. For a Council, `maestro wait <slug>` prints the Merge, and you write the synthesis; Maestro does not. Use [Reading results](#reading-results). Done when you have read every diff, or synthesized the Merge, and told the user the result.

## Running-Lanes table

Each Lane is a headless child process, in Solo and outside it. This includes Run Tasks, single Dispatches, and Council Seats. The user has no tab, screen, or log for a Lane, so your reply is the only view of the work.

While a Lane runs, open each reply to the user with a table of the running Lanes, with the columns Lane, Task, Started, and State. After the table, write one line that says what waits for the user, or "Nothing waits for you." Put only live processes in the table: `maestro status` shows the live state and marks a Run or Dispatch whose process is gone as `stale (process gone)`. When no Lane runs, write no table.

```markdown
| Lane | Task | Started | State |
|---|---|---|---|
| `codex-sol high` | Run `r-0075`, Task 1: `maestro wait` returns when Maestro needs the Orchestrator | 04:52 | running, 1m10s |
```

## Guardrails

- Ask the user only when a verb exits 2, or when a Task touches a human gate: push, publish, release, or deletes outside the workspace. In all other cases, decide and continue the work loop. Maestro never pushes.
- Quote braces in read-only commands, for example `git log '@{u}'..`.

## Brief and Plan files

`maestro help brief` and `maestro help plan` print the formats. Four rules the formats cannot enforce:

- Write the Goal as definitions, not commands, and the Done as what must be true when the Lane stops.
- `level:` is your estimate. Jev sets the Tier and uses the estimate as context. Write the level you believe, and let Maestro choose a stronger Lane: it uses a Frontier Lane when the work calls for it. For hard Tier bounds, set `floor:` and `ceiling:` in the Brief, or `maestro run --floor` and `--ceiling`. `maestro dispatch --level` and `maestro run --level` replace the estimate.
- `touch:` lists the paths the Task may change. Test files are always in scope. List every other path the Task needs, including fixtures, docs, and `CHANGELOG.md` when the Done asks for an entry. A Lane that must change a file outside `touch` stops blocked, and the Task comes back to you with Cause `brief`.
- A token-billed Lane takes edit work only when `lane:` names it. Name a token-billed Lane only when no subscription Lane fits.

Size each Task for ten to twenty Lane minutes. Four to six independent Tasks are better than fifteen small Tasks; with disjoint `Files`, they end a Run in 20 to 40 minutes. Keep work on the same files in one Task, or chain the Tasks with `blocked_by`. In `Files`, give the precise list of code files that the Task changes, so that Maestro can order Tasks whose `Files` overlap.

## Plans for Worktree mode

Parallel Tasks in Worktrees merge at the end of each Task. Write the Plan so that they do not conflict:

- With the default commit template, a Task title is the commit subject. Write it the way your project writes commit subjects.
- Make `touch` wide: the whole packages the Task changes, every test file that asserts the behavior (in other packages too), and every doc file the shared rules name (`docs/**`, `CONTEXT.md`, `skills/maestro/**`). A `touch` that leaves out a test or a doc costs one blocked attempt and one resume.
- In a Plan with more than one edit Task, keep `CHANGELOG.md` and `README.md` with you: parallel edits to the same section conflict at every merge. Tell each Lane to put its changelog line in the Summary of its Report. After the Run, write `CHANGELOG.md` and the README once.
- Put a change that removes or renames an exported function in the same Task as every caller. Otherwise the build fails inside the Task's `touch`.
- The Report has fixed headings. Ask for extra Report content under an existing heading, such as the Summary.

## Worktree mode and landing

### Mode choice

A Run or a standalone edit Dispatch uses Worktree mode when all of these are true: Worktrees are on, the repository has a commit, HEAD is on a branch, no Task's `touch` names a refused dependency folder, and no check is container-bound. A container-bound check starts with `ddev`, `docker`, `docker-compose`, `lando`, `sail`, or a path that ends in `/sail`. Worktree mode also applies to a dirty tree and to a Run with one edit Task. In all other cases, the Run or Dispatch uses tree mode. The mode line gives the mode and its reason.

`maestro approve` chooses the mode again from the current Plan. The mode stays the same on resume.

### Worktree mode

Each edit Task works in its own Worktree, which starts from the HEAD commit. The main checkout changes only at landing. Tasks whose files overlap run in Plan order. Each live Run has its own Run branch and base, so Worktree-mode Runs can share a project: `another Run is live: <id>` is information and does not hold the new Run.

Each read Task starts in a detached Worktree at the Run branch head, with the dependency links. A review with `blocked_by` on the edit Tasks sees their merged work.

### Tree mode

Tree mode needs the checkout to itself and runs edit Tasks one at a time.

### Commits

Lanes never commit. Maestro commits a verified Task with the user's commit message template. The default template is the title and a `Co-Authored-By` trailer for the Lane. In Worktree mode, the Plan's `commit: true` keeps the Task commits on the user's branch, and `commit: false` lands the changes uncommitted and leaves the user's own staged work staged. In tree mode, Maestro commits only when the Brief says `commit: true`.

### Dirty tree

For a dirty tree, `maestro run` prints `tree has uncommitted changes: Tasks start from <short hash>; your uncommitted work is not in them`.

A dirty tree permits landing when the dirty paths do not overlap the Run. Overlap pauses the Run with `landing blocked: uncommitted changes in <paths> overlap the Run; commit or stash them, then maestro resume <run> --finish`. Maestro never stashes, resets, or checks out the user's work to clear a block. Commit or stash the overlapping paths as the message says, then use `maestro resume <run> --finish`.

### Detached HEAD

A new Run with a detached HEAD uses tree mode with `HEAD is not on a branch`. A saved Run keeps its user branch, even with a detached HEAD. When the user's branch is not checked out at landing, the Run pauses with `user branch <name> is not checked out`. Check out that branch, then use `maestro resume <run> --finish`.

### Refused folders

A Task whose `touch` names a shared dependency folder puts the Run in tree mode with `task <n> touches <folder>`.

### Landing check

Before a Worktree-mode Run or a standalone edit Dispatch lands, Maestro runs the project's `checks` once on the merged Run branch, after any landing rebase. A failed landing check keeps the Run branch and every Task commit. Use the [fix flow](#fix-flow).

### Landing conflicts

When the user's branch gained commits, Finish rebases only the Run branch's own commits onto the user's branch head. A conflict pauses the Run with `landing conflict in <paths>; Run branch <name>`. Maestro aborts the rebase, so the Run branch stays intact. To recover:

1. On the Run branch, resolve the conflict and complete the rebase onto the user's branch.
2. Return to the user's branch.
3. Retry the landing: `maestro resume <run> --finish` for a Run, or `maestro dispatch --finish <task>` for a standalone Dispatch.

### Finish

Finish is the landing step. `maestro resume <run> --finish` lands a paused Run and retries a failed or blocked landing. For a standalone edit Dispatch, `maestro dispatch --finish <task>` does the same.

### Fix flow

After a failed landing check:

- **Run.** `maestro add <brief>` queues a fix Task on the paused Run. When more than one Run can take the Task, name the Run: `maestro add <brief> --run <id>`. Then `maestro resume <run> --finish` runs the fix and lands.
- **Standalone edit Dispatch.** Put the fix in the kept Worktree that `maestro wait` names, then use `maestro dispatch --finish <task>`.

## Plan check

Before a Run with two or more edit Tasks starts, Maestro runs a Plan check: one quick read Lane checks the Plan's paths, symbols, scope, and order against the project tree.

- `maestro wait <run>` waits for the Plan check.
- Findings hold the Run. Correct the Plan file, then use `maestro approve <run>`.
- A failed Plan check never blocks the Run.
- `maestro run --no-plan-check` or `[run] plan_check = false` skips the Plan check.

## Answering exit 2

`maestro wait` returns with exit 2 as soon as Maestro needs you. It prints the item, the reason, and the next `maestro answer`, `maestro resume`, or `maestro approve` command. A paused Run also gives the path to `report.md`. Read the item and `report.md` when present, answer once, then use `maestro wait` with the same id. Answered or resolved items do not stop it again. `maestro wait <id> --json` prints one object with the item's `kind` and `next_command`.

`maestro wait <task>` follows only that Task in its Run and gives its Cause, Blocked items, and resume command. After an answer, it waits for that Task.

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

A Relay is a Lane's permission prompt. Claude and Antigravity edit Lanes relay commands outside the allow list; all other Lanes do not relay. In Solo, a Relay wakes you. `maestro wait` returns it, and `maestro status` prints the `relay` event. Answer with `maestro answer <task> allow|deny --relay <id>`, with the id from the item or the event. An unanswered Relay is denied after 10 minutes, and the Lane continues.

### Merge conflicts

A merge conflict gets one attempt in the Task's Worktree. Then the Task blocks with the paths and the Worktree location. To recover, rebase the Task branch onto the Run branch in that Worktree, resolve the conflicts, and complete the rebase. Then `maestro resume <run> --task <n> --merge` retries the merge with no new Lane.

### Landing pauses

For a landing check failure, a dirty-tree overlap, a landing conflict, or a user branch that is not checked out, use [Worktree mode and landing](#worktree-mode-and-landing).

## Reading results

- Outcome `unverified` means Maestro did not close the Task on its own: the gate did not run, had no checks to run, had critical risk, or did not reach its close threshold. The diff decides.
- Before a Task ends `unverified`, Maestro can give the same Lane one nudge for missing Done items. `maestro wait` prints the last attempt's Report.
- `maestro wait` names each test file that a Lane changed outside its own `touch`. Read those diffs for weakened assertions.
- The Report is `result.md` in the Run folder (`.agents/maestro/runs/<run>/<task>/`). Council folders are `.agents/maestro/councils/<slug>/`. In Solo, every Task is a todo, and its Report is a comment.

## Exit codes

- 0: ok (verified, answered, or unverified).
- 1: failed.
- 2: needs you: an open Relay, a held breakdown, a paused Run, a blocked or failed Task, or a single-opinion Council.

## Grok facts

- A Grok Lane runs in Grok's always-approve mode, so Grok never prompts. The Maestro hook and the workspace sandbox hold its Access.
- A Grok edit Lane with writable `dirs` outside the project, or outside its Worktree, runs without the sandbox and is `best-effort`. The Route records the launch's Enforcement.
