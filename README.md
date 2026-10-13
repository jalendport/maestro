<p align="center"><img src=".github/icon.svg" alt="Maestro" width="80" height="80"></p>

<h1 align="center">Maestro</h1>

Maestro is a local command-line tool that owns agent work after the thinking is done. An orchestrating session writes a brief or a plan; Maestro routes each task to a lane that is strong enough for it across Claude Code, Codex, Grok, and Antigravity, dispatches it, checks the result, and runs councils for second opinions, with one ledger of what ran and why.

## Features

- **Routing with a reason** — every task goes to one harness, model, and effort, picked by [Jev](https://typesafe.ai) with a deterministic rules table as the fallback, and every route prints one reason line
- **Subscriptions first** — day-to-day work spreads across the subscriptions you already pay for; token-billed lanes take only read-only work, and the orchestrator's own model is never spent automatically
- **Budgets that bind** — usage windows are harvested from every dispatch and probed when stale, with a block point, a warning, and a reserve for the orchestrator
- **A permission contract** — a lane gets `read` or `edit` access through each harness's own sandbox or allowlist; Maestro never passes a bypass flag
- **Checks and a gate** — the project's verification commands run after every edit, a `touch:` list guards the scope, and Jev judges the diff against the brief before a task closes
- **Councils** — several lanes answer one question in parallel, with challenge rounds, and Maestro merges the findings without synthesizing them
- **Runs** — a plan file becomes ordered tasks under concurrency caps, with a live status, a short report, a pause rule, and one-answer resumes
- **Solo integration** — inside [Solo](https://soloterm.com), tasks are todos, lanes are tabs, results are comments, and the orchestrator is woken when a run ends
- **Single static binary** — Go, no runtime dependencies; each harness needs only a skill file and one hook

## Installation

Install with [Homebrew](https://brew.sh), by the full tap path:

```sh
brew install jalendport/tap/maestro
```

Or with the install script (macOS and Linux):

```sh
curl -fsSL https://raw.githubusercontent.com/jalendport/maestro/main/install.sh | sh
```

Then check the machine and connect the harnesses:

```sh
maestro doctor      # harness versions, logins, the Jev key, Solo
maestro key set     # store the Jev key in ~/.maestro/env (optional; the rules table works without it)
maestro adapter     # the skill file and hook to add per harness
```

## Usage

Write a brief file (`maestro help brief` prints the format), then dispatch it:

```sh
maestro dispatch .agents/briefs/add-probe.md        # returns at once with a task id
maestro wait 7f3a                                   # prints the status, changes, checks, and report
maestro dispatch .agents/briefs/add-probe.md --wait # or block
```

Several ordered tasks go in a plan file (`maestro help plan`):

```sh
maestro run .agents/plans/probe-feature.md   # provisional routes; holds for approval when a trigger fires
maestro approve r-0012
maestro status r-0012
maestro wait r-0012                          # the run report
maestro resume r-0012 --task 4 --lane claude-opus:high   # or --brief <file>, or --skip
```

Ask several lanes one question:

```sh
maestro council "Is the retry-once rule safe for limit hits?" --challenge
maestro wait is-the-retry-once-rule-safe
maestro council --task 7f3a   # review one task's result
```

Facts:

```sh
maestro roster          # families, models, levels, pre-flight state
maestro roster refresh  # re-read the harness catalogs
maestro budget          # usage per account
maestro router stats    # Jev vs rules agreement, verified rates
```

Exit codes: `0` ok, `1` failed, `2` needs the orchestrator (a held breakdown, a paused run, a blocked task). `--json` prints one object per verb.

## Configuration

Maestro works with no configuration: the roster, the rules table, the prices, and the thresholds are compiled in. Two optional files refine it.

`~/.maestro/config.toml` holds your accounts, harness overrides, and deltas on families and chains. Maestro never rewrites it.

```toml
version = 1

[accounts.claude-max]
harness    = "claude"
billing    = "subscription"   # or metered
plan_price = 200
reserve    = 0.35             # share of the weekly window kept for the orchestrator

[families.grok]
enabled = false

[rules.heavy]
implement = ["codex-sol", "claude-opus"]

[router]
backend = "jev"               # or rules
```

`.agents/maestro.toml` in a project tightens things for that repository only:

```toml
checks   = ["go build ./...", "go vet ./...", "go test ./..."]   # run after every edit
forbid   = ["grok"]                                              # families this project never uses
dirs     = ["~/Library/Caches/go-build"]                         # extra writable directories for edits
router   = "rules"                                               # keep this project's text off the network
redact   = ["ACME"]                                              # strings replaced before any Jev call
councils = "always"                                              # an end-of-run council on every run
```

Generated state lives in `~/.maestro/`: `ledger.jsonl` (append only), `budget.json`, `cache/discovery.json`, and the live-run index. Per-project run folders live under `.agents/maestro/runs/` and council folders under `.agents/councils/`; `maestro clean` removes old ones and never touches the ledger.

## Support

Found a bug or need help? Open an [issue](https://github.com/jalendport/maestro/issues).

<hr>

<p align="center">Made by <a href="https://jalendport.com">Jalen Davenport</a></p>
