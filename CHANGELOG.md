# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## Unreleased

### Added
- Added Worktree mode for Runs: each edit Task runs in parallel in its own Worktree and Task branch, and Maestro merges it into the Run branch; other Runs use tree mode, as before
- Added Worktrees for standalone edit Dispatches, under the same conditions as a Run; `maestro dispatch --finish <task>` retries a blocked landing without a new Lane
- Added read Worktrees: each read Task of a Worktree-mode Run reads a detached Worktree at the Run branch head, so a review Task sees the merged work
- Added the mode line `worktrees: on` or `worktrees: off (<reason>)`, for example for a container-bound check, to `maestro run`, `maestro dispatch`, `maestro status`, and the Run report
- Added `[run] worktrees` in `~/.maestro/config.toml` (default `true`) and the project key `worktrees = false` to turn Worktrees off
- Added dependency links in Worktrees (`node_modules`, `vendor`, `.venv`, `.bundle`, `storage/framework`, `.env`, `.env.local`) and the project key `worktree_links` for more paths
- Added the overlap order: in Worktree mode, an edit Task waits with `files overlap with <n>` while an earlier edit Task with an overlapping `Files` or `touch` path is not done
- Added Maestro commits after the gate: Maestro commits a verified edit Task with the user as author; in tree mode it commits only when the Brief says `commit: true`
- Added the commit message template: `[commit] message` in `~/.maestro/config.toml` or the project key `commit_message`, with placeholders such as `{title}` and `{lane}`, and `[commit.identities]`
- Added landing for a Worktree-mode Run: with `commit: true` the Task commits land on the user's branch, and with `commit: false` the changes land uncommitted; `maestro resume <run> --finish` retries
- Added landing on a busy checkout: Maestro rebases the Run branch onto a user branch that gained commits, and uncommitted changes block a landing only when their paths overlap the Run's
- Added the landing check: the project's `checks` run on the merged Run branch before it lands; a failure pauses the Run for a fix Task from `maestro add`
- Added the merge conflict rule: a Task that conflicts with the Run branch gets one more attempt to resolve it; after a hand fix, `maestro resume <run> --task <n> --merge` retries the merge
- Added Worktree cleanup: a cancelled Run removes its Worktrees, and `maestro clean` removes old Worktrees and keeps and names each branch whose commits did not land
- Added live Runs that share a project in Worktree mode; tree mode needs the checkout to itself, and `maestro add <brief> --run <id>` names the Run when more than one is live
- Added the Plan check: one read Dispatch checks the Plan against the tree before a Run with two or more edit Tasks; findings hold the Run, and `--no-plan-check` or `[run] plan_check = false` skips it
- Added the Plan's introduction to the start of every Task's Context under `Shared context of the Plan:`
- Added the project's live items to the Reminder in Claude Code, Codex, and Antigravity: open Relays, paused Runs, blocked and failed Tasks of live Runs, and running Lanes
- Added a stale Adapter line to the Reminder when the installed skill or Grok rules differ from the binary; run `maestro adapter install` to clear it
- Added sleep protection for blocking verbs and for the Orchestrator's turn in Claude Code, Codex, and Antigravity; run `maestro adapter install` again and trust the new Codex hook in `/hooks`
- Added Solo wake-ups for a paused Run and for a single Dispatch that ends failed, blocked, timed out, or with an unshaped Report
- Added read-only Solo MCP tools for Claude Lanes with a Solo link, for process output and todo comments; other Solo tools are denied without a Relay
- Added `maestro doctor --canary [harness]` to check each installed Harness with a small read Dispatch; Canary Dispatches do not count in Route statistics
- Added `maestro doctor` warnings for no active project checks, Docker checks without `~/.docker` in `dirs`, and ignored `.env*` files that Worktrees do not link

### Changed
- Changed every Lane to run as a headless child process, inside Solo and outside it
- Changed Lanes to never commit: every Brief says `Do not commit` and has no push rules, and Claude Lanes no longer get `git add` and `git commit`; a Brief's `commit: true` now means Maestro commits
- Changed Claude and Antigravity edit Lanes to relay commands outside the allow list in every launch, and Antigravity edit Lanes to have no vendor print timeout
- Changed routing so Jev's Judgment level sets the Tier; the Brief's `level` and `--level` are the Orchestrator's estimate, and only `floor` and `ceiling` set hard Tier bounds
- Changed Run escalation to use the Cause of a Bad result: only `capability` re-Routes one Tier up, and `brief`, `environment`, and `none` block the Task for the Orchestrator
- Changed passing Tasks under the gate's close threshold to get one nudge on the same Lane for missing Done items, with one extra attempt in the Dispatch and Run caps
- Changed the scope guard to accept test files outside `touch` and flag them as `tests outside touch` for the gate and the end-of-Run Council; a deleted test file outside `touch` stays a violation
- Changed checks to use the Idle limit and a shared Wall limit budget, with one Docker pre-flight per Task; a limit or a failed pre-flight skips the remaining checks
- Changed a failed check to run once more when the Wall limit budget permits; a pass records `flaky: true`, and failed output keeps the failure lines before its tail
- Changed the hook's read-only command list to a baseline for every project, with text and file commands, Git list forms, and Go, Node, PHP, Python, and Docker read forms
- Changed the hook to permit a glob after a fixed prefix in a read-only command, such as `ls ./*_test.go`; a bare glob and an unquoted brace stay denied, and the deny reason tells how to write them
- Changed the hook to remove `env` words and assignments to harmless names such as `CI` and `CGO_ENABLED` before it matches a command; other assignments keep the full command
- Changed `maestro init` to detect project stacks, write cache `dirs` and `allow` rules, suggest `checks` and `worktree_links` in comments, and merge missing values with `--write`
- Changed `maestro wait` to return exit 2 for a blocked or failed Task (kind `task_failed`) and to give the `report.md` path of a paused Run; `maestro wait <task>` follows only that Task
- Changed `maestro clean` to remove old or orphan control folders across all project keys and old cache folders; it never touches the Ledger or `budget.json`
- Changed the default of `max_per_account` to no Account cap; `max_total` still applies across Runs
- Changed the default Frontier chain to try `codex-astra`, then `claude-fable`
- Changed each Run notification to include the project folder name
- Changed the Orchestrator skill to ask for the `level` it believes, a `touch:` list with every required path, and a table of the running Lanes at the start of each reply while a Lane runs
- Changed the Orchestrator skill to add `Plans for Worktree mode`: precise `Files`, wide `touch`, no `CHANGELOG.md` or `README.md` edits by Lanes in a parallel Plan, and four to six Tasks

### Deprecated
- Deprecated the keys `close_tabs` and `solo_tool`: old user files still load, the keys have no effect, and `maestro doctor` reports them

### Removed
- Removed Lane tabs in Solo, tab Budget Harvest, and Solo agent tool discovery
- Removed `maestro tell` and `maestro roster solo`

### Fixed
- Fixed Lanes stopped at the Idle limit while a tool call was in progress; the Wall limit still applies
- Fixed Grok Lanes that stopped at the Idle limit during work
- Fixed Antigravity read Lanes that could not write `result.md`; they now return the Report in their final message
- Fixed resumed Tasks judged on files from other Tasks or stopped by an earlier answer's Dispatch cap; the Run report now lists the files judged
- Fixed Run Tasks shown as stale while their supervisor runs the checks and the gate; `maestro status` now names the checks phase
- Fixed `maestro status` to mark a Lane whose process is gone as `stale (process gone)`
- Fixed the Reminder's project root in a nested git folder to match the CLI
- Fixed resume answers that appeared again before a slow Task start; a second answer cannot restart a running Task
- Fixed Run ownership during a pause, so only one process takes a resume answer and a second supervisor cannot take a live Run
- Fixed `maestro approve` to reuse the held Task ids and leave no orphan Task folders
- Fixed Gate scores of zero to stay recorded and printed as scores
- Fixed Tasks that wait for an Account slot to make no Budget refresh or Router call while they wait; a lost slot keeps its Route
- Fixed a race between Run supervisors that could start more Dispatches than `max_total` or a set `max_per_account` permits
- Fixed Mac idle sleep during live work on battery power: every Maestro `caffeinate` now uses `-i -s -w <pid>`
- Fixed Runs that started one `caffeinate` for each Dispatch; each Maestro process now shares one
- Fixed `budget.json` to drop Accounts that are absent from the loaded Roster

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
