<p align="center"><img src=".github/icon.svg" alt="Maestro" width="80" height="80"></p>

<h1 align="center">Maestro</h1>

Maestro is a small command-line tool that takes over once you and your coding agent have finished planning. The agent you are talking to writes down what needs doing as a brief or a plan, and Maestro picks a lane for each task (one of Claude Code, Codex, Grok, or Antigravity, at a given model and effort level), runs it, checks the result, and can put the same question to several models at once when you want a second opinion. Everything that ran, and why it went where it did, lands in one ledger.

## Features

- **Councils** — put one question to several models at once (e.g. "is this diff safe to merge?"), let them challenge each other's answers in a second round, and get every finding back side by side, with the weak ones marked rather than dropped, so the final call stays with you
- **Routing with a reason** — each task goes to the harness, model, and effort level that is strong enough for it, chosen by [Jev](https://typesafe.ai) (with a plain rules table as the fallback when there is no key), and every choice prints one line saying why
- **Runs** — a plan file turns into ordered tasks that run in parallel where they can, with a live status view, a short report at the end, and a pause rule so a stuck task asks you one question instead of guessing
- **Checks and a gate** — your project's own verification commands run after every edit, a `touch:` list keeps a task inside the files it was given, and Jev reads the diff against the brief before a task is allowed to close
- **Subscriptions first** — everyday work spreads across the subscriptions you already pay for, while lanes billed per token get read-only work on their own and edit work only when you ask for them by name; set `[router] orchestrator = "<family>"` in `~/.maestro/config.toml` and the model you are talking to is never spent automatically
- **Budgets that bind** — usage windows are read from every dispatch (and probed when stale), with a block point, a warning, and a reserve kept back for the session you are in
- **A permission contract** — a Lane gets `read` or `edit` Access through a sandbox, a tool allowlist, or the Maestro hook; Maestro passes no bypass flag to Claude, Codex, or Antigravity, and a Grok Lane runs in always-approve only because the hook and the sandbox decide before Grok does
- **Solo integration** — inside [Solo](https://soloterm.com), tasks are todos, lanes are tabs, results are comments, and your session is woken when a run ends
- **Keeps the Mac awake** — `caffeinate` runs for as long as a dispatch or a run is live, so the machine does not go to sleep under a lane that is still working, and a notice tells you when the run ends; set `[failure] keep_awake = false` to turn it off
- **Single static binary** — Go, no runtime dependencies, and each harness needs only a skill file, a hook entry or two, and for Grok one rules file

## Installation

Prerequisites: [git](https://git-scm.com), at least one of the four harness CLIs ([Claude Code](https://code.claude.com/docs/en/quickstart), [Codex](https://developers.openai.com/codex/cli), [Grok](https://docs.x.ai/build/overview), or [Antigravity](https://antigravity.google/docs/getting-started?tab=cli)), and optionally [Solo](https://soloterm.com).

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

Then check the machine and connect the harnesses:

```sh
maestro doctor           # harness versions, logins, the Jev key, Solo
maestro key set          # store the Jev key in ~/.maestro/env (optional; the rules table works without it)
maestro adapter install  # connect every harness found on this machine
maestro init             # in each project: write .agents/maestro.toml
```

`maestro adapter` prints the steps for each harness and writes nothing. `maestro adapter install` does the steps. Maestro touches nothing else in the configuration of a harness. To connect one harness only, give its name: `maestro adapter install claude`.

`maestro init` writes the Project file `.agents/maestro.toml`. That file puts the project in hook scope.

An install from before this version reports `partial` in `maestro doctor` until `maestro adapter install` runs again.

### Connect Claude Code

```sh
maestro adapter install claude
```

It writes two paths:

- `~/.claude/skills/maestro` is a link to the shared skill file in `~/.maestro/skills/maestro`.
- `~/.claude/settings.json` gets one `PreToolUse` hook entry and one `UserPromptSubmit` hook entry for `maestro hook`; Maestro adds or replaces only its own entries and keeps all other settings.

Manual step: start a new Claude Code session. It loads the hook.

A Claude tab in a project you have not opened in Claude Code will wait on its folder trust dialog; open the project in Claude Code once first.

Every Claude Lane ignores the user, project, and local settings files. `CLAUDE.md` still loads. Maestro supplies the Lane hook and sandbox settings at launch. A Claude edit Lane runs shell commands in the Claude sandbox: writes stay in the project, the Run folder, the project's `dirs`, and the per-user temp directory. Shell commands have no outbound network, so a check that must download something fails inside a Claude Lane. Caches already on disk are readable; a check that writes a cache needs that cache in `dirs`. A shell command that names a network host is denied without a prompt in every Claude Lane, one-shot or tab (`sandbox.network.strictAllowlist`, Claude Code 2.1.219 or later); a check that needs a module download must find it in the caches first. `claude_auto = true` makes Claude edit Lanes `best-effort`.

Verify: `maestro doctor` shows `adapter claude` as `ok`.

Probe the hook. A sub-agent that can write code must get a `deny`:

```sh
echo '{"tool_name":"Agent","tool_input":{"subagent_type":"general-purpose"}}' | maestro hook
```

The probe denies only where the hook is in scope: with the default `[hook] scope = "projects"` that is inside a project that has `.agents/maestro.toml`, and `scope = "all"` in `~/.maestro/config.toml` makes it global.

### Connect Codex

```sh
maestro adapter install codex
```

It writes two paths:

- `~/.agents/skills/maestro` is a link to the shared skill file in `~/.maestro/skills/maestro`.
- `~/.codex/hooks.json` gets one `PreToolUse` hook entry and one `UserPromptSubmit` hook entry for `maestro hook codex`; Maestro adds or replaces only its own entries and keeps all other hooks.

Manual step: start Codex, open `/hooks`, and trust both the Maestro `PreToolUse` hook and the Maestro `UserPromptSubmit` hook. This review is a security boundary. Only you cross it. An agent must not trust the hooks for you.

Maestro passes `-c 'projects={"<root>"={trust_level="trusted"}}'` to each Codex tab to trust the project root for that process.

Verify: `maestro doctor` shows `adapter codex` as `ok`.

Probe the hook. A sub-agent that can write code must get a `deny`:

```sh
echo '{"tool_name":"Agent","tool_input":{"subagent_type":"general-purpose"}}' | maestro hook
```

The probe denies only where the hook is in scope: with the default `[hook] scope = "projects"` that is inside a project that has `.agents/maestro.toml`, and `scope = "all"` in `~/.maestro/config.toml` makes it global.

### Connect Grok

Grok takes read and edit work. Grok's own permission modes do not hold a Lane: `dontAsk` is a Claude mode name that Grok accepts but maps to `default`, which prompts in a tab, and headless Grok runs with full tool access. So a Grok Lane runs in always-approve mode and never prompts, and the Maestro hook is the policy: it runs before every tool call and denies what the Lane may not do, and the workspace sandbox backs it at the kernel level:

- A read Lane runs the built-in read-only commands and writes only its Report in the Run folder
- An edit Lane also runs the checks and the project `allow` list, and writes inside the project, the Run folder, and the extra `dirs`; the workspace sandbox also permits writes to temp directories and `~/.grok`
- Grok never relays: a command outside the allow list is denied; add it to `checks` or the project `allow` list

The hook judges a command as written. A read-only command that carries a shell expansion (`$`, a backquote, a brace list, or an unquoted glob) is denied, because the shell could turn the expansion into a flag the list forbids; quote the pattern (`find . -name '*.go'`) or name the file. The sandbox is the boundary that holds a shell; the hook is the second line that holds paths, tool names, and the command list.

Grok Lanes run under `--sandbox workspace` when every writable directory is inside the project. For edit Lanes, this includes `dirs`. If a directory lies outside the project, as this repository's Go caches do, the Grok edit Lane runs without the sandbox and its Route is `best-effort`. A read Lane does not use the extra edit `dirs`.

Maestro cannot detect four limits. Grok warns and continues without the kernel sandbox when the OS refuses a built-in profile. A Grok tab carries no Lane variable, so the hook finds the Lane through the `transcriptPath` key of the hook payload; a Grok release that renames or drops that key leaves the tab under the Orchestrator rule, in always-approve, with only the workspace sandbox as its boundary. A Solo tab whose transcript is not yet on disk at its first tool call runs that call under the Orchestrator rule; the first call of a Lane is the read of its Brief. A check that runs Lane-written code under the Grok workspace sandbox can write `~/.grok`, Grok's own home. Grok's `hashline` file toolset is not supported in a Lane: its write tools are denied as unknown.

The hook entry changed, so an existing install must run `maestro adapter install grok` again.

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

The probe denies only where the hook is in scope: with the default `[hook] scope = "projects"` that is inside a project that has `.agents/maestro.toml`, and `scope = "all"` in `~/.maestro/config.toml` makes it global.

### Connect Antigravity

Antigravity takes read and edit work, like the other harnesses. The Maestro hook entry carries each Lane's permissions:

- A read Lane runs read-only commands and writes only its Report in the Run folder
- An edit Lane also runs commands on the checks and `allow` lists, and writes inside the project, the Run folder, and the extra `dirs`
- Only an edit Lane in a tab relays other commands; a read Lane or a one-shot Lane denies them

Antigravity has no kernel sandbox, so the hook is the only boundary of an Antigravity Lane. It judges a command as written: a read-only command that carries a shell expansion (`$`, a backquote, a brace list, or an unquoted glob) is denied; quote the pattern or name the file.

The hook entry changed, so an existing install must run `maestro adapter install agy` again.

```sh
maestro adapter install agy
```

It writes two paths:

- `~/.gemini/antigravity-cli/skills/maestro` is a link to the shared skill file in `~/.maestro/skills/maestro`.
- `~/.gemini/config/hooks.json` gets one `maestro` key that holds a `PreToolUse` entry and a `PreInvocation` entry; Maestro sets only that key and keeps all other keys.

Manual step: start a new Antigravity session. It loads the hook.

Verify: `maestro doctor` shows `adapter agy` as `ok`.

Probe the hook. A sub-agent that can write code must get a `deny`:

```sh
echo '{"toolCall":{"name":"invoke_subagent","args":{"agent":"general-purpose"}}}' | maestro hook
```

The probe denies only where the hook is in scope: with the default `[hook] scope = "projects"` that is inside a project that has `.agents/maestro.toml`, and `scope = "all"` in `~/.maestro/config.toml` makes it global.

## Usage

You install once and plan with your agent. The agent hands finished plans to Maestro on its own. You answer only when Maestro needs you.

1. Install Maestro and connect the harnesses once. See [Installation](#installation).
2. Plan with your agent as you do now: a grilling session, a spec, a plan file. The Maestro skill tells the agent what a finished plan looks like.
3. When you approve the plan, the agent writes the Plan file and runs `maestro run` itself. Nobody asks you to type anything.
4. Inside Solo, each task is a todo and each result is a comment, and a finished run wakes the agent. Outside Solo, the agent reports from `maestro wait`.
5. Maestro needs you when a verb exits with code 2: a held breakdown, a paused run, a blocked task, or a single-opinion council. The agent reads the report and either answers or asks you one question.

Inside hook scope, the Orchestrator gets a one-line Reminder on every prompt in Claude Code and Codex, and before every model call in Antigravity. In Grok, the rules file loads in every session and states the scope condition in words, so there the scope is best-effort.

For example, you end a grilling session with a plan for a new probe feature. The agent writes `.agents/plans/probe-feature.md` and starts the run. Some minutes later it tells you that the three tasks are done and the checks pass. If a lane needs an answer, the agent asks you one question and passes your answer on.

### Verbs

These are the commands the agent runs. A person can run them by hand too.

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

A run holds for approval, with exit code 2, when any one of these is true: the plan has more than four tasks, a task has a medium or fallback route, the level you gave a task is two levels from the router's, the router asks the orchestrator a question, or the router signals a council. A held Run starts a plan Council only with `--council`, when the Plan has at least `[run] council_tasks` Tasks (default `10`; `0` disables the size rule), or when the Router asks the Orchestrator or signals a Council for a Task. `--no-council` stops the plan Council in every case. Fold any findings into the Plan file, then use `maestro approve <run>`: it reads the file again, routes the current Tasks, and starts under the same Run id, with no second plan Council.

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

Answer a Relay with `maestro answer <task> allow|deny --relay <id>`. Use the id from the `relay` event printed by `maestro wait` or `maestro status`.

## Configuration

Maestro works with no configuration: the roster, the rules table, the prices, and the thresholds are compiled in. Two optional files refine it.

Without a Jev key the rules table routes every task, and edit tasks end `unverified`; the orchestrator reads the diff.

Every key, its type, and its default are in [docs/configuration.md](docs/configuration.md).

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

`.agents/maestro.toml` in a project changes things for that repository only; `forbid`, `checks`, `router`, `redact`, and `councils` tighten, and `allow`, `dirs`, and `claude_auto` widen:

```toml
checks   = ["go build ./...", "go vet ./...", "go test ./..."]   # run after every edit
forbid   = ["grok"]                                              # families this project never uses
dirs     = ["~/.cache/go-build"]                                 # extra writable directories for edits: your Go build cache, `go env GOCACHE`
router   = "rules"                                               # keep this project's text off the network
redact   = ["ACME"]                                              # strings replaced before any Jev call
councils = "always"                                              # an end-of-run council on every run
```

Generated state lives in `~/.maestro/`: `ledger.jsonl` (append only), `budget.json`, `cache/discovery.json`, and the live-run index. Per-project run folders live under `.agents/maestro/runs/` and council folders under `.agents/maestro/councils/`; `maestro clean` removes old ones and never touches the ledger. Maestro writes a `.gitignore` into each run and council folder it creates, so nothing under them reaches git.

Keep-awake and the end-of-run notice are macOS only; on Linux they do nothing.

## Support

Found a bug or need help? Open an [issue](https://github.com/jalendport/maestro/issues).

<hr>

<p align="center">Made by <a href="https://jalendport.com">Jalen Davenport</a></p>
