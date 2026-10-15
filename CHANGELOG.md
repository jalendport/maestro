# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## Unreleased

### Added
- Added Antigravity edit Lanes and Solo tabs, with command prompts relayed through the Maestro hook
- Added the Reminder for the Orchestrator in every Harness: a `UserPromptSubmit` hook entry for Claude Code and Codex, a `PreInvocation` hook entry for Antigravity, and the rules file `~/.grok/rules/maestro.md` for Grok; run `maestro adapter install` again, trust the new Codex hook in `/hooks`, and until then `maestro doctor` reports the Adapter as `partial`
- Added `maestro init` to write the Project file `.agents/maestro.toml`
- Added custom read-only agents to the spawn rule: a sub-agent whose definition file under `.claude/agents` or `~/.claude/agents` has a `tools` list of read tools only may be spawned

### Changed
- Changed Antigravity read enforcement to the Maestro hook, so it no longer depends on the Antigravity settings file
- Changed the Antigravity hook entry to cover commands and file writes; run `maestro adapter install agy` again
- Changed edit Enforcement to use the sandbox and Maestro hook for Claude and Grok, the sandbox for Codex, and the Maestro hook for Antigravity; `claude_auto` and Grok edit Lanes with `dirs` outside the project are `best-effort`
- Changed every Claude Lane, read and edit, to ignore the user, project, and local settings files; `CLAUDE.md` still loads
- Changed the Grok hook to check every tool call; agy and grok Lanes fail pre-flight when their Maestro hook is missing, wrong, or disabled; run `maestro adapter install agy` and `maestro adapter install grok` again
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
- Fixed Grok Seats and read Lanes in Solo tabs so they can write their Report: absolute path grants permit Run folder writes, the Maestro hook holds read Access, and no deny rule blocks the Report grant
- Fixed the Lane signal for a project-relative Pointer and for Codex context messages of a kind Maestro has not seen
- Fixed the diff a retried or resumed Task is judged on: changes are saved on every exit path, so the gate and the end-of-run Council see every attempt, including deleted untracked files
- Fixed Relay lock recovery after its owner exits and kept each completed answer with its request id
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
