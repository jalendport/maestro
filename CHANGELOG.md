# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## Unreleased

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

## 1.0.0 - 2026-10-13

### Added
- Added `maestro dispatch`, `wait`, and `run` to route a brief or plan to a lane across Claude Code, Codex, Grok, and Antigravity, then verify the result
- Added `maestro council` for parallel independent views with challenge rounds and a merged findings list
- Added `maestro budget`, `roster`, and `router stats` to track subscription windows, the rules table, and routing accuracy
- Added `maestro adapter` and `maestro hook` to install the shared skill and the spawn guard per harness
- Added Solo integration: tasks become todos, lanes become tabs, reports become comments, and a finished run wakes the orchestrator
