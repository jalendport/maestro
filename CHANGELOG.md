# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## Unreleased

### Added
- Added the Worktree switches: `[run] worktrees` in `~/.maestro/config.toml` defaults to `true`, and project `worktrees = false` can turn it off; `maestro doctor` shows the setting, dependency links, and a warning for uncommitted changes
- Added Worktree mode for a Run: when Worktrees are on, the repository has a commit, the tree has no uncommitted changes, and the Plan has more than one edit Task, each edit Task runs at the same time as the others in its own Worktree on its own Task branch; otherwise the Run uses tree mode with `tree busy`, as before. The mode and the tree-mode reason go into `run.json` once, when the Run starts, and `maestro run`, `maestro status`, and the Run report show `worktrees: on` or `worktrees: off (<reason>: Tasks run one at a time)`
- Added the overlap order for Worktree mode: an edit Task waits with `files overlap with <n>` while an earlier edit Task that is not its dependency and is not done shares a path with it, and the Routes, status, and report show `after <n> (files overlap)`; a Task whose `touch` can write a shared dependency folder runs in the tree, one at a time, and status shows `in tree (touches <folder>)`
- Added Maestro commits after the gate: a `verified` edit Task, or an `unverified` edit Task with a Report and changed files, gets one commit of its changed paths with the user as author; in Worktree mode Maestro then merges the Task branch into the Run branch, and a `blocked_by` Task starts from the merged Run branch. The message comes from `[commit] message` in `~/.maestro/config.toml` (default `{title}\n\nCo-Authored-By: {lane}`, with `{title}`, `{run}`, `{task}`, `{family}`, `{model}`, `{lane}`, and `{summary}`), or from `commit_message` in `.agents/maestro.toml`; `[commit.identities]` replaces the `{lane}` text per Family prefix
- Added the end of a Worktree-mode Run: a `done` Run with the Plan's `commit: true` fast-forwards the user's branch and keeps the Task commits, and with `commit: false` lands the changes uncommitted and deletes the Run branch; when the user's branch moved, the Run pauses with the error and `maestro resume <run> --finish` retries. A cancelled Run removes its Worktrees and keeps the Run branch, which the report names; `maestro clean` removes the Worktrees and branches of Runs that ended before `--older-than`
- Added the merge conflict rule: when a Task branch conflicts with the Run branch, Maestro starts the rebase in the Task's Worktree and gives the Task one more attempt with a conflict Brief; the Lane edits the conflicted files, and Maestro continues the rebase after the checks and the gate and merges again. A second conflict or a failed attempt blocks the Task with `merge conflict in <paths>` and keeps its Worktree, which `maestro wait` and the Reminder name; after a hand fix, `maestro resume <run> --task <n> --merge` retries the merge with no new Lane
- Added two roots for a Dispatch: `Project` keeps the Run folder, Brief, Project file, allow rules, and Ledger; `Workdir` is the Worktree for Lane edits, checks, the baseline, and the gate diff, or `Project` when no Worktree is set
- Added the Worktree package for Runs: Task branches from a Run branch, shared dependency folders that stay unlinked when `touch` can write them, commits of given paths with a filled message template, and merges through the Run branch
- Added the Plan's introduction to the start of every Task's Context under `Shared context of the Plan:`, before the Task's own Context
- Added a stale Adapter line to the Reminder in Claude Code, Codex, and Antigravity when the installed skill or Grok rules differ from the binary; run `maestro adapter install` to clear it
- Added project warnings in `maestro doctor` for no active checks and for Docker checks without `~/.docker` in `dirs`
- Added sleep protection for every verb that blocks on live work and for the Orchestrator's turn in Claude Code, Codex, and Antigravity, with a refreshed 30-minute lease and `Stop` hooks; run `maestro adapter install` again and trust the new Codex hook in `/hooks`
- Added project live items to the Reminder in Claude Code, Codex, and Antigravity: open Relays with answer commands, paused Runs with resume commands, blocked and failed Tasks of live Runs, and running Lanes with their Tasks and elapsed times; Grok uses `maestro status`
- Added Solo wake-ups for a paused Run and a single Dispatch that ends failed, blocked, timed out, or with an unshaped Report, with the same next command as `maestro wait`
- Added `maestro doctor --canary [harness]` to check each Harness with a small read Dispatch and store version-specific launch, hook, and Report results; the hook part does not apply to Codex, and the Canary uses only its Wall limit

### Changed
- Changed Lanes to never commit: every rendered Brief says `Do not commit`, Claude Lanes no longer get the `git add` and `git commit` tools, and the hook no longer approves these commands for a Lane. A Brief's `commit: true` now means that Maestro commits the Task's changed paths after the gate, on the user's branch in tree mode and for a single Dispatch
- Changed slow tests to use short limits, poll intervals, and wake-up waits that each test sets, and to run independent cases in parallel; `go test ./...` takes about 15 seconds instead of 105, tests check that the production defaults stay the same, and a `wait` test no longer starts the installed Harnesses
- Changed `maestro init` to detect project stacks, write cache directories and command allow rules, suggest checks in comments, and merge missing values with `--write` while it keeps existing values and comments
- Changed the hook's read-only command list to a baseline for every project, with text and file commands, Git list forms, and Go, Node, PHP, Python, Rust, and Docker read forms; tool caches and logs are allowed, while project writes and test, build, format, and install runners need project rules
- Changed passing Tasks under the gate's close threshold to get one nudge on the same Lane for missing Done items, with one extra attempt in the Dispatch and Run caps and a Ledger mark
- Changed routing so Jev's Judgment level sets the Tier; the Brief's `level` is context for Jev and the level for the rules fallback, while explicit Tier bounds and the edit rule still apply
- Changed the skill file and README to tell the Orchestrator to write the `level` it believes, with no raised level or named Lane to reach a stronger Lane, and to list in `touch:` every path the Task may change, including tests, fixtures, docs, and `CHANGELOG.md`; the skill file also says what a blocked Task's Cause means and what one nudge is
- Changed Run escalation to use the Cause of each Bad result; Brief and environment blockers go to the Orchestrator with the Lane's Blocked items, while capability failures can re-Route one Tier up; the Ledger records the Cause, its source, and the decision
- Changed checks to use the Idle limit and a separate shared Wall limit budget, with one Docker pre-flight per Task; a limit or failed pre-flight skips the remaining checks and records the reason and Failure class
- Changed `maestro clean` to remove old or orphan control folders across all project keys and old cache folders; it keeps live Run controls and the latest Canary files per Harness, and never touches the Ledger or `budget.json`
- Changed `maestro wait` to return with exit 2 and the next command for an open Relay, a paused Run, or a blocked or failed Task that needs the Orchestrator; while a Lane runs, each user reply opens with a table of the running Lanes (Lane, Task, Started, State) and one line for what waits for the user
- Changed `close_tabs` to a key with no effect; old user files still load, and `maestro doctor` reports the key
- Changed Canary evidence to name the hook record's folder, accept a Run folder record when the control write fails, and keep failed check files at the path shown by `maestro doctor` until the next Canary for that Harness
- Changed the default of `max_per_account` to no Account cap; `max_total` still applies across Runs
- Changed `solo_tool` to a key with no effect; old user files still load, and `maestro doctor` reports the key
- Changed every Lane to run as a headless child process inside Solo and outside it. Claude Lanes with a Solo link get a read-only Solo MCP set for process data and todo comments. The Brief gives the required `project_id`. Other Solo tools are denied without a Relay, and all `SOLO_` variables stay removed
- Changed rendered Briefs and Brief format help to leave push rules to the user; a Lane that may commit must commit only the files it changed for that Task
- Changed the default Frontier chain to try `codex-astra`, then `claude-fable`
- Changed Claude and Antigravity edit Lanes to relay commands outside the allow list on the one-shot path; Antigravity edit Lanes use Maestro's limits with no vendor print timeout

### Removed
- Removed the Lane tab path, its launch shapes, tab Budget Harvest, and Solo agent tool discovery; every Lane uses the one-shot path
- Removed `maestro tell` and `maestro roster solo`, which used Lane tabs and Solo agent tool discovery

### Fixed
- Fixed command matching for environment prefixes so `env`, `env -u NAME`, `env -i`, and variable assignments use the command's allow rules and read-only limits
- Fixed resumed Tasks judged on files from other Tasks and stopped by an earlier answer's Dispatch cap; the Run report now lists the files judged
- Fixed Run Tasks shown as stale while their supervisor runs the checks and the gate; status, the Reminder, and wait now use the Run supervisor, and status and the Reminder name the checks phase
- Fixed Run notifications during tests and added the project folder name to each Run notification
- Fixed Mac idle sleep during live work: every Maestro `caffeinate` now uses `-i -s -w <pid>` to protect work on battery and AC power
- Fixed blank Ledger rows from tests with an empty-`kind` check and a required temporary `MAESTRO_HOME` in tests; Budget saves now drop Accounts that are absent from the loaded Roster
- Fixed resume answers that appeared again before a slow Task start; the Run state is saved before the answer marker is removed, and a second answer cannot restart a running Task
- Fixed Run ownership during a pause; the supervisor and CLI make one decision about which process takes a resume answer, and a second supervisor cannot take a live Run
- Fixed stale Lanes in the Reminder and status; the Reminder leaves out processes that are gone, and status marks them `stale (process gone)`
- Fixed the Reminder's project root in a nested git folder to match the CLI
- Fixed the label for a failed Task in `maestro wait` to be `task_failed`, limited Relay input in the Reminder and wait reason to 300 characters, and added the paused Run report path in text and JSON
- Fixed the temp path symlink test to use the per-user temp folder and skip with a reason when it cannot make a symlink
- Fixed Runs that started one caffeinate for each Dispatch; each Maestro process now shares one until the last caller stops
- Fixed Lanes stopped at the Idle limit while a tool call was in progress; that time no longer counts toward the Idle limit, and the Wall limit still applies
- Fixed Antigravity read Lanes that could not write `result.md` in headless mode; they now return the Report in their final message
- Fixed Tasks that wait for an Account slot so they do no Budget refresh, Router call, or snapshot while they wait; a lost slot keeps its Route for the next claim
- Fixed Route statistics to leave out Canary Ledger rows
- Fixed the stated Grok transcript limit
- Fixed a race between Run supervisors that could start more Dispatches than `max_total` or a set `max_per_account` permits
- Fixed one-shot Grok Lanes that stopped at the Idle limit during work: Maestro now reads `streaming-json` output line by line

## 1.2.0 - 2026-10-14

### Added
- Added Antigravity edit Lanes and Solo tabs, with command prompts relayed through the Maestro hook
- Added the Reminder for the Orchestrator in every Harness: a `UserPromptSubmit` hook entry for Claude Code and Codex, a `PreInvocation` hook entry for Antigravity, and the rules file `~/.grok/rules/maestro.md` for Grok; run `maestro adapter install` again, trust the new Codex hook in `/hooks`, and until then `maestro doctor` reports the Adapter as `partial`
- Added `maestro init` to write the Project file `.agents/maestro.toml`
- Added custom read-only agents to the spawn rule: a sub-agent whose definition file under `.claude/agents` or `~/.claude/agents` has a `tools` list of read tools only may be spawned

### Changed
- Changed the effort of an automatic Council Seat to the Family's `council` key when it is set; the default Roster sets `council = "medium"` for `grok`, whose Seats took two to three times as long as the others
- Changed the Solo tab name to the Brief's new `tab` field (at most 18 characters), or the first words of the Task title cut at a word boundary when the Brief has none
- Changed held Runs to start a plan Council only on request, at `[run] council_tasks` Tasks (default 10), or for a Router question or Council signal; `--no-council` stops it, and approval reads and routes the Plan file again under the same Run id without a second plan Council
- Changed Antigravity read enforcement to the Maestro hook, so it no longer depends on the Antigravity settings file
- Changed the Antigravity hook entry to cover commands and file writes; run `maestro adapter install agy` again
- Changed edit Enforcement to use the sandbox and Maestro hook for Claude and Grok, the sandbox for Codex, and the Maestro hook for Antigravity; `claude_auto` and Grok edit Lanes with `dirs` outside the project are `best-effort`
- Changed every Claude Lane, read and edit, to ignore the user, project, and local settings files; `CLAUDE.md` still loads
- Changed the Grok hook to check every tool call; agy and grok Lanes fail pre-flight when their Maestro hook is missing, wrong, or disabled; run `maestro adapter install agy` and `maestro adapter install grok` again
- Changed Grok Lanes to run in Grok's always-approve mode with no allow or deny rules: Grok has no mode that is prompt-free and deny-by-default (`dontAsk` prompts in a tab and headless mode has full tool access), so the Maestro hook decides every tool call before Grok does and the workspace sandbox backs it
- Changed Grok edit Lanes to use `--sandbox workspace` when every writable directory is inside the project; extra `dirs` outside it turn the sandbox off
- Changed Claude edit Lanes to run shell commands in the Claude sandbox, with no outbound network and writes limited to the project, Run folder, `dirs`, and per-user temp directory
- Changed Codex Lanes to pin writable roots instead of inheriting extra roots from user settings
- Changed the Adapter state to `disabled` when an installed agy or grok hook will not run; the pre-flight gate and `maestro doctor` name the cause
- Changed the Codex Carrier to run `maestro hook codex`; run `maestro adapter install codex` again and trust both hooks in `/hooks`
- Changed the token-billed rule: a requested Lane on a `metered` Account now takes edit work with a warning instead of a refusal; automatic Routes still give it read work only
- Changed where Lane control files live: the Lane policy, Relay requests, and answer markers now sit under a project key in `~/.maestro/control`; let running Lanes finish before the upgrade, because a Lane that is live during the upgrade loses its policy
- Changed Relay answers to require `maestro answer <task> allow|deny --relay <id>`, with the id from the Relay event; stale answers cannot approve a later request
- Changed the read-only command list to deny write flags, program execution flags, branch creation, and `uniq` with two operands
- Changed the deny reasons to name every accepted read token and every read-only agent type
- Changed the Council brief to tell a Seat what to say when nothing is wrong
- Changed the skill to be shorter, to name three verbs, and to point at `maestro help` for the formats

### Fixed
- Fixed Grok Seats and read Lanes in Solo tabs so they can write their Report: Grok passes no allow or deny rules, and the Maestro hook permits the Report write in the Run folder and holds read Access
- Fixed the Lane signal for a project-relative Pointer and for Codex context messages of a kind Maestro has not seen
- Fixed the diff a retried or resumed Task is judged on: changes are saved on every exit path, so the gate and the end-of-run Council see every attempt, including deleted untracked files
- Fixed Relay lock recovery after its owner exits and kept each completed answer with its request id
- Fixed the read-only command list: `--` no longer ends the check for `find`, whose expression comes after the paths, and a read-only command that carries a shell expansion (`$`, a backquote, a brace list, or an unquoted glob) is denied, because the hook judges the command as written
- Fixed the `.git` deny of an edit Lane to compare the name without case, so `.GIT/config` is denied on a case-insensitive file system
- Fixed Claude `NotebookEdit` in a Lane, which reads `notebook_path`
- Fixed ordinary Claude sessions whose transcript the hook cannot read: they run under the Orchestrator rule instead of a deny, because a Claude Lane names its Run folder in its launch settings
- Fixed the whole-Task baseline for a Task whose first attempt started before the repository's first commit
- Fixed a Relay lock older than the Relay deadline to be taken over even when its owner's pid is in use again
- Fixed a wake-up for a Relay that was already answered before the tracker saw it
- Fixed the recorded changed files of a judged attempt so they match what the gate saw, not what the checks created afterwards
- Fixed a Lane or Seat whose Solo tab was closed by hand: it ends at once instead of at its wall limit
- Fixed the shell check to accept `2>&1`, which names no file, and to deny the zsh forms `=(` and an unquoted `(` in a read-only command
- Fixed the `.git` deny of an edit Lane when `.git` is a symlink into the project: both names are denied
- Fixed the closed-tab rule to count only a `not found` answer from Solo, so a timeout or a helper restart does not end a Lane
- Fixed Claude Lane tabs that waited on the sandbox's network prompt: every Claude Lane now denies a host outside the allowlist without a prompt (`strictAllowlist`)
- Fixed `maestro hook` for Grok and Antigravity to deny an unreadable stdin or payload inside a one-shot Lane instead of allowing it; a Solo tab carries no Lane variable, so there the hook finds the Lane through the transcript only
- Fixed tests that read the developer's real Grok home or the Lane's own `MAESTRO_TASK_FOLDER`
- Fixed Council todo titles longer than 200 characters by cutting them at a word boundary with a trailing `…`
- Fixed `maestro status` to show `—` for a Task's elapsed time when the Task never started
- Fixed the Homebrew cask so it no longer prints a deprecation warning for `postflight` on install
- Fixed the README so the Homebrew upgrade command uses the full tap path

## 1.1.0 - 2026-10-13

### Added
- Added `maestro adapter install` to write the skill link and the hook file for each harness
- Added `maestro --version` to print the installed version
- Added `maestro status` output for live dispatches and councils
- Added `maestro cancel <run>` to stop a run
- Added relay of permission requests from Claude Lanes through a hook, answered with `maestro answer`
- Added the project `allow` key for extra permission rules on edit dispatches
- Added `[hook] scope` to choose where the spawn guard applies
- Added `[router] orchestrator` to name the family the orchestrator runs on
- Added a Solo tab for every Lane except Antigravity
- Added the configuration reference in `docs/configuration.md`
- Added support for `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, and `MAESTRO_SOLO_MCP`
- Added the `todo` brief field to attach a task to an existing Solo todo

### Changed
- Changed Antigravity read Enforcement to use its settings command permissions, so safe settings permit automatic read Routes and Council Seats
- Changed `strip_env` to remove API credentials only for subscription Accounts, so metered Accounts keep their API keys and auth tokens
- Changed the `sonnet` and `haiku` aliases to Sonnet 5.5 and Haiku 5.5, and refreshed the price table against the vendors' pages
- Changed the default gate `satisfies` threshold from `0.90` to `0.65`
- Changed the supervisor to block a Task and pause the Run when two consecutive attempts fail with the same error text
- Councils now live under `.agents/maestro/councils/`.
- Changed the rules table to route without a warning when there is no Jev key
- Changed `reserve` to apply to any account
- Changed the embedded accounts to one neutral account per harness
- Changed `close_tabs = "on-success"` to also close the tab on `unverified`
- Changed `maestro doctor` to show one row per harness adapter and to pass when a harness is not installed

### Removed
- Removed the unused config keys `accounts.<id>.paid_overflow`, `accounts.<id>.budget_source`, `accounts.<id>.base_url`, `failure.cooling_period`, and project `pattern_setting`
- Removed the Claude Code plugin; anyone who installed it runs `claude plugin uninstall maestro@jalendport`

### Fixed
- Fixed redaction so it applies to every Jev call
- Fixed a user account so it replaces the embedded account of its harness
- Fixed two projects that shared a run id
- Fixed the metered spend cap so it blocks routes
- Fixed Codex window durations
- Fixed a task that was charged with the files of an earlier task
- Fixed a finished Lane that was reported as a stall
- Fixed stream lines over 64 MB
- Fixed the limit probe that could hang
- Fixed a Run that hung behind a blocked Task; a Run with a failed or blocked Task now ends paused, and a Plan with a dependency cycle is rejected
- Fixed the plan Council that stopped when `maestro run` exited, and the end-of-run Council that delayed the end of a Run
- Fixed `maestro resume --brief` and `maestro run --level` so the new values reach the Task
- Fixed Run and Task ids so two starts never share a folder, and a reused short id finds the newest Run
- Fixed committed work that escaped the `touch:` guard and the gate
- Fixed `maestro cancel`, `wait`, and `clean` for a standalone Dispatch, which could hang or remove a live folder
- Fixed lost last lines of Lane output, quota messages in a Solo tab, and check commands that outlived their time limit
- Fixed repeated-error detection so it names the failed check
- Fixed Jev picks that broke the floor, the ceiling, or the edit rule, and the Frontier Tier that no Route could reach
- Fixed fan-out efforts such as `ultra` that a Route could pick; they now run only when a Brief names the Lane and effort
- Fixed `[aliases]` overrides that did not choose the launched model
- Fixed the Grok read shape to be enforced by the harness, so Grok takes automatic read Routes and Council Seats again
- Fixed the Jev key `TYPESAFE_API_KEY` so it is never passed to a Lane
- Fixed unknown keys in the project file, a Brief, and a Plan, which now give a warning or an error that names the key
- Fixed `budget.json` and the ledger so several Maestro processes no longer overwrite each other
- Fixed budget blocks: an explicit block wins over a missing reading, and a Reserve never weakens the block point
- Fixed `maestro budget reset codex` so it ends within its deadline
- Fixed `maestro adapter install` so it keeps other hook handlers in the same entry
- Fixed the project root so it is never the home directory
- Fixed `--json` so every verb prints one JSON object
- Fixed a Council with `--challenge` that stopped after round 1, a blocked Seat that counted as an answer, and a Council that lost its `--task` context
- Fixed the help text and the example Plan to match the verbs
- Fixed `install.sh` so it verifies the archive against `checksums.txt` and names `maestro adapter install`

## 1.0.0 - 2026-10-13

### Added
- Added `maestro dispatch`, `wait`, and `run` to route a brief or plan to a lane across Claude Code, Codex, Grok, and Antigravity, then verify the result
- Added `maestro council` for parallel independent views with challenge rounds and a merged findings list
- Added `maestro budget`, `roster`, and `router stats` to track subscription windows, the rules table, and routing accuracy
- Added `maestro adapter` and `maestro hook` to install the shared skill and the spawn guard per harness
- Added Solo integration: tasks become todos, lanes become tabs, reports become comments, and a finished run wakes the orchestrator
