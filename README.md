<p align="center"><img src=".github/icon.svg" alt="Maestro" width="80" height="80"></p>

<h1 align="center">Maestro</h1>

Maestro is a command-line tool that takes the Tasks of an approved plan, routes each Task to a Lane (a Harness, a model, and an effort level), runs it, and checks the result. It can also put one question to several Lanes in a Council. It records every Dispatch, and the reason for its Route, in one Ledger.

## Features

- **Councils** — put one question to several Lanes, let them challenge each other in a second Round, and get every finding side by side
- **Routing with a reason** — each Task goes to a Lane that is strong enough for it, chosen by [Jev](https://typesafe.ai) or a rules table, with one line saying why
- **Runs** — a Plan file becomes ordered Tasks that run in parallel where they can, with a live status view and a report at the end
- **Checks and a gate** — your project's checks run after every edit, and Jev reads the diff against the Brief before a Task closes
- **Subscriptions first** — work spreads across the subscriptions you already pay for; token-billed Lanes get edit work only when you name them
- **Budgets that bind** — usage windows are read from every Dispatch, with a block point, a warning, and a reserve for your own session
- **An Access contract** — a Lane gets `read` or `edit` Access, held by a sandbox, a tool allowlist, or the Maestro hook
- **Solo integration** — inside [Solo](https://soloterm.com), Tasks are todos, results are comments, and your session wakes when a Run ends
- **Keeps the Mac awake** — `caffeinate` protects every Lane, Run, Council, and waiting verb, and the Orchestrator's turn in most Harnesses
- **Single static binary** — Go, no runtime dependencies; each Harness needs only a skill file and hook entries, and Grok one rules file

## Installation

Prerequisites: [git](https://git-scm.com), at least one of the four Harness CLIs ([Claude Code](https://code.claude.com/docs/en/quickstart), [Codex](https://developers.openai.com/codex/cli), [Grok](https://docs.x.ai/build/overview), or [Antigravity](https://antigravity.google/docs/getting-started?tab=cli)), and optionally [Solo](https://soloterm.com).

Install with [Homebrew](https://brew.sh) (macOS), by the full tap path:

```sh
brew install jalendport/tap/maestro
```

Upgrade by the full tap path too; the bare name `maestro` resolves to a different cask:

```sh
brew upgrade --cask jalendport/tap/maestro
```

Or with the install script (macOS and Linux):

```sh
curl -fsSL https://raw.githubusercontent.com/jalendport/maestro/master/install.sh | sh
```

Then check the machine and connect the Harnesses:

```sh
maestro doctor           # harness versions, logins, the Jev key, Solo
maestro key set          # store the Jev key in ~/.maestro/env (optional; the rules table works without it)
maestro adapter install  # connect every harness found on this machine
maestro init             # in each project: write .agents/maestro.toml
```

`maestro doctor` shows the Harness versions and starts no Dispatch. `maestro doctor --canary` runs one small read Dispatch on each installed Harness to check its launch, its Report, and the Maestro hook. See [Doctor and Canary](docs/reference.md#doctor-and-canary).

`maestro adapter` prints the steps for each Harness and writes nothing. `maestro adapter install` does the steps; `maestro adapter install claude` connects one Harness. See [Adapters](docs/reference.md#adapters).

`maestro init` writes the Project file `.agents/maestro.toml`, which puts the project in hook scope, and detects the stack from the files at the project root. `maestro init --write` merges the proposed additions into an existing Project file. See [Project file configuration](docs/configuration.md#agentsmaestrotoml).

### Connect Claude Code

Every Claude Lane runs in the Claude sandbox with the hook and settings that Maestro supplies. See [Claude Code](docs/reference.md#claude-code).

```sh
maestro adapter install claude
```

It writes two paths:

- `~/.claude/skills/maestro` is a link to the shared skill file in `~/.maestro/skills/maestro`.
- `~/.claude/settings.json` gets `PreToolUse`, `UserPromptSubmit`, and `Stop` hook entries for `maestro hook`; Maestro adds or replaces only its own entries and keeps all other settings.

Manual step: start a new Claude Code session. It loads the hook.

Verify: `maestro doctor` shows `adapter claude` as `ok`.

Probe the hook inside a project that has `.agents/maestro.toml`. A sub-agent that can write code must get a `deny`:

```sh
echo '{"tool_name":"Agent","tool_input":{"subagent_type":"general-purpose"}}' | maestro hook
```

### Connect Codex

The Codex sandbox holds the Access of a Codex Lane. See [Codex](docs/reference.md#codex).

```sh
maestro adapter install codex
```

It writes two paths:

- `~/.agents/skills/maestro` is a link to the shared skill file in `~/.maestro/skills/maestro`.
- `~/.codex/hooks.json` gets `PreToolUse`, `UserPromptSubmit`, and `Stop` hook entries for `maestro hook codex`; Maestro adds or replaces only its own entries and keeps all other hooks.

Manual step: start Codex, open `/hooks`, and trust the Maestro `PreToolUse`, `UserPromptSubmit`, and `Stop` hooks. This review is a security boundary. Only you cross it. An agent must not trust the hooks for you.

Verify: `maestro doctor` shows `adapter codex` as `ok`.

Probe the hook. A sub-agent that can write code must get a `deny`:

```sh
echo '{"tool_name":"Agent","tool_input":{"subagent_type":"general-purpose"}}' | maestro hook
```

### Connect Grok

A Grok Lane runs in always-approve mode and never prompts. The Maestro hook and the workspace sandbox hold its Access. See [Grok](docs/reference.md#grok).

```sh
maestro adapter install grok
```

It writes three paths:

- `~/.agents/skills/maestro` is a link to the shared skill file in `~/.maestro/skills/maestro`.
- `~/.grok/hooks/maestro.json` is a file that Maestro owns; Maestro writes the whole file.
- `~/.grok/rules/maestro.md` is the rules file that Maestro owns; it carries the Reminder, because Grok hooks cannot add context.

Manual step: start a new Grok session. It binds the hook.

Verify: `maestro doctor` shows `adapter grok` as `ok`.

Probe the hook. A sub-agent that can write code must get a `deny`:

```sh
echo '{"toolName":"spawn_subagent","toolInput":{"agent":"general-purpose"}}' | maestro hook
```

### Connect Antigravity

Antigravity has no kernel sandbox, so the Maestro hook is the only boundary of an Antigravity Lane. See [Antigravity](docs/reference.md#antigravity).

```sh
maestro adapter install agy
```

It writes two paths:

- `~/.gemini/antigravity-cli/skills/maestro` is a link to the shared skill file in `~/.maestro/skills/maestro`.
- `~/.gemini/config/hooks.json` gets one `maestro` key that holds `PreToolUse`, `PreInvocation`, and `Stop` entries; Maestro sets only that key and keeps all other keys.

Manual step: start a new Antigravity session. It loads the hook.

Verify: `maestro doctor` shows `adapter agy` as `ok`.

Probe the hook. A sub-agent that can write code must get a `deny`:

```sh
echo '{"toolCall":{"name":"invoke_subagent","args":{"agent":"general-purpose"}}}' | maestro hook
```

## Usage

You install once and plan with your agent. The agent hands finished plans to Maestro on its own. You answer only when Maestro needs you.

1. Install Maestro and connect the Harnesses once. See [Installation](#installation).
2. Plan with your agent as you do now: a grilling session, a spec, a Plan file. The Maestro skill tells the agent what a finished plan looks like.
3. When you approve the plan, the agent writes the Plan file and runs `maestro run` itself. Nobody asks you to type anything.
4. Inside Solo, each Task is a todo, each result is a comment, and a finished Run wakes the agent; see [Solo](docs/reference.md#solo). Outside Solo, the agent reports from `maestro wait`.
5. Maestro needs you when a verb exits with code 2. The agent reads the item or report and either answers or asks you one question.

For example, you end a grilling session with a plan for a new probe feature. The agent writes `.agents/plans/probe-feature.md` and starts the Run. Some minutes later it tells you that the three Tasks are done and the checks pass. If a Lane needs an answer, the agent asks you one question and passes your answer on.

These are the commands the agent runs. A person can run them by hand too.

Write a Brief file (`maestro help brief` prints the format), then dispatch it:

```sh
maestro dispatch .agents/briefs/add-probe.md        # returns at once with a task id
maestro wait 7f3a                                   # prints the status, changes, checks, and report
maestro dispatch .agents/briefs/add-probe.md --wait # or block
```

Several ordered Tasks go in a Plan file (`maestro help plan`). A Run can hold for approval; see [Approval holds](docs/reference.md#approval-holds). `maestro wait` returns early when Maestro needs the Orchestrator; see [Verbs](docs/reference.md#verbs).

```sh
maestro run .agents/plans/probe-feature.md   # provisional routes; holds for approval when a trigger fires
maestro approve r-0012
maestro status r-0012
maestro wait r-0012                          # the run report
maestro resume r-0012 --task 4 --lane claude-opus:high   # or --brief <file>, or --skip
maestro answer 7f3a allow --relay <id>       # answer a Lane's permission prompt (a Relay)
```

Ask several Lanes one question:

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

Exit codes: `0` ok, `1` failed, `2` needs the Orchestrator (an open Relay, a held breakdown, a paused Run, a blocked or failed Task, or a single-opinion Council). `--json` prints one object per verb.

## Configuration

Maestro works with no configuration: the Roster, the rules table, the prices, and the thresholds are compiled in. Two optional files refine it. Every key, its type, and its default are in [docs/configuration.md](docs/configuration.md); what Maestro does with them is in [docs/reference.md](docs/reference.md).

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
backend      = "jev"           # or rules
orchestrator = "claude-opus"   # the family your orchestrator runs on; Maestro never spends it automatically
```

`.agents/maestro.toml` in a project changes things for that repository only; `forbid`, `checks`, `router`, `redact`, `councils`, and `worktrees` tighten, and `allow`, `dirs`, and `claude_auto` widen:

```toml
checks   = ["go build ./...", "go vet ./...", "go test ./..."]   # run after every edit
forbid   = ["grok"]                                              # families this project never uses
dirs     = ["~/.cache/go-build"]                                 # extra writable directories for edits: your Go build cache, `go env GOCACHE`
router   = "rules"                                               # keep this project's text off the network
redact   = ["ACME"]                                              # strings replaced before any Jev call
councils = "always"                                              # an end-of-run council on every run
```

Worktrees are on by default; see [`[run]`](docs/configuration.md#run) to turn them off, [`[commit]`](docs/configuration.md#commit) to set the commit message, and [Worktrees and landing](docs/reference.md#worktrees-and-landing) for how they work.

Maestro writes the Ledger, the Budget, the discovery cache, and the Run and Council folders itself; see [Generated state](docs/reference.md#generated-state) for the paths and `maestro clean`.

## Support

Found a bug or need help? Open an [issue](https://github.com/jalendport/maestro/issues).

<hr>

<p align="center">Made by <a href="https://jalendport.com">Jalen Davenport</a></p>
