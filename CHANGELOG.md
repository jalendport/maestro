# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## Unreleased

### Added
- Added `maestro adapter install` to write the skill link and the hook file for each harness
- Added `maestro --version` to print the installed version

### Removed
- Removed the unused config keys `accounts.<id>.paid_overflow`, `accounts.<id>.budget_source`, `accounts.<id>.base_url`, `failure.cooling_period`, and project `pattern_setting`
- Removed the Claude Code plugin; anyone who installed it runs `claude plugin uninstall maestro@jalendport`

## 1.0.0 - 2026-10-13

### Added
- Added `maestro dispatch`, `wait`, and `run` to route a brief or plan to a lane across Claude Code, Codex, Grok, and Antigravity, then verify the result
- Added `maestro council` for parallel independent views with challenge rounds and a merged findings list
- Added `maestro budget`, `roster`, and `router stats` to track subscription windows, the rules table, and routing accuracy
- Added `maestro adapter` and `maestro hook` to install the shared skill and the spawn guard per harness
- Added Solo integration: tasks become todos, lanes become tabs, reports become comments, and a finished run wakes the orchestrator
