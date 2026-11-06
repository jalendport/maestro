# Configuration

This file lists each configuration key, its type, its default, and its accepted values. For what Maestro does with a key, follow the link to the [behavior reference](reference.md).

Maestro runs with no configuration file. It reads three layers, in this order: the defaults compiled into the binary, then `~/.maestro/config.toml` (your deltas), then `.agents/maestro.toml` in the project root. `MAESTRO_HOME` moves the `~/.maestro` folder.

A delta is a change on top of the defaults: a family or a chain that you name in your file replaces that one entry, and every entry you do not name keeps its default. A release updates the defaults. Maestro never rewrites your files. A key that Maestro does not know gives a warning. It does not stop a Route.

## ~/.maestro/config.toml

This file holds only your deltas. A missing file is not an error.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `version` | integer | `1` | Names the version of the file format. |

### [accounts]

An Account is one login that Lanes bill against. Each Account is a table named `[accounts.<id>]`.

Keys of `accounts.<id>`:

| Key | Type | Default | Meaning |
|---|---|---|---|
| `harness` | string | none | Binds the Account to one Harness, and so to every Family of that Harness. |
| `billing` | string | `"subscription"` | Sets the billing kind: `subscription` reads a usage window, `metered` counts money against a cap. An automatic Route gives a `metered` Account read work only, and only when no subscription Lane serves the level; a requested Lane on a `metered` Account takes edit work with a warning. |
| `plan_price` | float | none | Gives the plan price in USD, which Maestro multiplies by Headroom to break a tie between equally fit subscription Lanes. |
| `block_at` | float | `0.90` | Sets the used share of a window at which the Account's Lanes leave the Route list. |
| `reserve` | float | none | Keeps this share of the weekly window for the Orchestrator, so automatic Dispatches cannot use it. The reserve applies to any Account. |
| `budget` | table | none | Sets the spend cap of a `metered` Account. |
| `budget.usd` | float | none | Sets the cap in USD. |
| `budget.period` | string | none | Sets the period of the cap, for example `30d`. |

When your file names an Account for a Harness, that Account replaces every default Account of the same Harness. Maestro binds each Family to the one Account of its Harness. If a Harness has more than one Account, Maestro stops with an error, so keep one Account for each Harness.

A new Account in your file starts from `billing = "subscription"` and `block_at = 0.90`. For an Account that has a default entry, Maestro changes only the keys you write.

Default entries:

| Account | `harness` | `billing` | `block_at` |
|---|---|---|---|
| `claude` | `"claude"` | `"subscription"` | `0.90` |
| `codex` | `"codex"` | `"subscription"` | `0.90` |
| `grok` | `"grok"` | `"subscription"` | `0.90` |
| `agy` | `"agy"` | `"subscription"` | `0.90` |

### [harness]

A table named `[harness.<name>]` holds the settings for one Harness: `claude`, `codex`, `grok`, or `agy`. A key that you write replaces the default for that key only. For how each Harness holds a Lane's Access, see [Adapters](reference.md#adapters).

| Key | Type | Default | Meaning |
|---|---|---|---|
| `bin` | string | none | Sets the path of the Harness program; when it is empty, Maestro finds the program itself. |
| `min_version` | string | see below | Sets the oldest Harness version that `maestro doctor` accepts. |
| `strip_env` | array of strings | see below | Lists the environment variables that Maestro removes before it starts a Lane or a Probe on this Harness. Names that contain `API_KEY` or `AUTH_TOKEN` are removed only for subscription Accounts; other names are always removed. `TYPESAFE_API_KEY` is always removed for every Billing kind, even if it is absent from `strip_env`, because it is Maestro’s own credential. |
| `solo_tool` | integer | none | Accepted for compatibility; has no effect. `maestro doctor` reports the key when it is present. |

Default entries:

| Harness | `min_version` | `strip_env` |
|---|---|---|
| `claude` | `"2.1.283"` | `["ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_EFFORT_LEVEL", "CLAUDE_CODE_SUBAGENT_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL_FORCE"]` |
| `codex` | `"0.156.1"` | `["OPENAI_API_KEY"]` |
| `grok` | `"1.0.50"` | `["GROK_SANDBOX"]` |
| `agy` | `"1.2.9"` | `[]` |

### [aliases]

An alias is a short model name that a Family lists in `models`. Maestro replaces it with the full model name. Each key is the alias and each value is the model name.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `<alias>` | string | see below | Maps a short model name to the full model name that Maestro sends to the Harness. |

Default entries:

| Alias | Model |
|---|---|
| `haiku` | `"claude-haiku-5-5"` |
| `sonnet` | `"claude-sonnet-5-5"` |
| `opus` | `"claude-opus-5-5"` |
| `fable` | `"claude-fable-5-1"` |

### [families]

A Family is one Harness × model pair, the unit that the [Router](reference.md#router) chooses between. Each Family is a table named `[families.<id>]`.

Keys of `families.<id>`:

| Key | Type | Default | Meaning |
|---|---|---|---|
| `harness` | string | none | Binds the Family to the Harness that runs it. |
| `models` | array of strings | none | Lists the models of the Family, in order of preference; Maestro uses the first one that the Harness catalog has. |
| `levels` | table | none | Maps a Judgment level (`none`, `some`, `significant`, `expert`) to an effort; the levels in the map set the Tiers that the Family serves. |
| `account` | string | none | Names the Account of the Family; Maestro replaces it with the one Account of the Harness. |
| `good_for` | string | none | Tells the Router what the Family does well. |
| `not_for` | string | none | Tells the Router what the Family must not do. |
| `request_only` | boolean | `false` | Keeps the Family out of automatic Routes; a Lane of the Family runs only when you request it. |
| `frontier` | boolean | `false` | Puts the Family in the Frontier Tier, whatever its levels are. |
| `enabled` | boolean | `true` | Takes the Family out of every Route when it is `false`. |
| `council` | string | none | Sets the effort of an automatic Council Seat of the Family, whatever the question's level; a requested Seat (`--lane family:effort`) keeps its own effort. The default Roster sets `council = "medium"` for `grok`, whose Seats at `high` took two to three times as long as the other Seats. |

If your file names a Family that has a default entry, Maestro changes only the keys you write. If it names a new Family, you must write `harness`, or Maestro ignores the entry with a warning.

Default entries:

| Family | `harness` | `models` | `levels` | `frontier` |
|---|---|---|---|---|
| `claude-haiku` | `"claude"` | `["haiku"]` | `{ none = "" }` | `false` |
| `claude-sonnet` | `"claude"` | `["sonnet"]` | `{ some = "high" }` | `false` |
| `claude-opus` | `"claude"` | `["opus"]` | `{ significant = "high", expert = "xhigh" }` | `false` |
| `claude-fable` | `"claude"` | `["fable"]` | `{ expert = "high" }` | `true` |
| `codex-luna` | `"codex"` | `["gpt-6-luna"]` | `{ none = "medium" }` | `false` |
| `codex-sol` | `"codex"` | `["gpt-6.1-sol", "gpt-6-sol"]` | `{ some = "medium", significant = "high", expert = "xhigh" }` | `false` |
| `codex-astra` | `"codex"` | `["gpt-6-astra"]` | `{ expert = "high" }` | `true` |
| `grok` | `"grok"` | `["grok-4.7"]` | `{ none = "low", some = "medium", significant = "high", expert = "xhigh" }` | `false` |
| `agy-flash` | `"agy"` | `["gemini-3.8-flash"]` | `{ none = "low", some = "high" }` | `false` |
| `agy-pro` | `"agy"` | `["gemini-3.1-pro"]` | `{ significant = "high" }` | `false` |

Text defaults of `good_for` and `not_for`:

| Family | `good_for` | `not_for` |
|---|---|---|
| `claude-haiku` | `"docs, release notes, one-file lookups, formatting"` | `"writing code, anything with judgment"` |
| `claude-sonnet` | `"prose, small scoped edits, documentation with a known shape"` | `"multi-file approach decisions, unreproducible failures"` |
| `claude-opus` | `"the one pattern-setting lane per feature, UI polish, design and review, specs"` | `"mechanical follow-ons that a smaller lane can do"` |
| `claude-fable` | `"expert judgment, design and review, specs"` | `"day-to-day work"` |
| `codex-luna` | `"mechanical edits with every change named, renames, boilerplate"` | `"open questions, prose"` |
| `codex-sol` | `"spec-slice implementation, mechanical follow-ons, test work, fixes with a known cause"` | `"open design questions, prose"` |
| `codex-astra` | `"expert judgment after Heavy failed: concurrency, security, architecture"` | `"day-to-day work"` |
| `grok` | `"issue triage, contrarian checks, quick facts, second opinions"` | `"security-sensitive edits, multi-file research"` |
| `agy-flash` | `"very large inputs up to 1M tokens, cited web research"` | `"edits"` |
| `agy-pro` | `"very large inputs that need judgment, cited web research"` | `"edits"` |

A Family id is written `<harness>-<short model>`, for example `codex-sol`; the default `grok` Family omits the model. A Lane is a Family plus one effort. A fan-out effort is an effort setting that starts sub-agents on its own, such as Codex `ultra` or Claude `ultracode`.

`account`, `request_only`, and `enabled` have no default entry. A default Family has `request_only = false` and `enabled = true`. Setting `[router] orchestrator` to a Family id makes that Family `request_only`.

### [rules]

The rules table is the deterministic [Router](reference.md#router). It has one table for each Tier: `[rules.<tier>]` with `light`, `daily`, `heavy`, or `frontier`. Each key in a Tier table is a category, and each value is an ordered chain of Family ids.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `default` | array of strings | see below | Sets the chain that Maestro uses for a category that has no chain of its own. |
| `<category>` | array of strings | see below | Sets the chain for one category, for example `implement`; Maestro tries the Families in order. |

When your file names a category, that chain replaces the default chain for that category only. Other categories keep their defaults. A category name with a space must be in quotes in TOML.

Default entries:

| Tier | Category | Chain |
|---|---|---|
| `light` | `default` | `["codex-luna", "claude-haiku", "grok", "agy-flash"]` |
| `light` | `triage` | `["grok", "codex-luna", "claude-haiku", "agy-flash"]` |
| `light` | `"large input"` | `["agy-flash", "codex-luna", "claude-haiku", "grok"]` |
| `daily` | `default` | `["codex-sol", "claude-sonnet", "grok", "agy-flash"]` |
| `daily` | `research` | `["grok", "codex-sol", "claude-sonnet", "agy-flash"]` |
| `daily` | `triage` | `["grok", "codex-sol", "claude-sonnet", "agy-flash"]` |
| `daily` | `prose` | `["claude-sonnet", "codex-sol", "grok", "agy-flash"]` |
| `daily` | `"second opinion"` | `["grok", "codex-sol", "claude-sonnet", "agy-flash"]` |
| `heavy` | `default` | `["claude-opus", "codex-sol", "grok", "agy-pro"]` |
| `heavy` | `implement` | `["codex-sol", "claude-opus", "grok", "agy-pro"]` |
| `heavy` | `fix` | `["codex-sol", "claude-opus", "grok", "agy-pro"]` |
| `heavy` | `refactor` | `["codex-sol", "claude-opus", "grok", "agy-pro"]` |
| `heavy` | `review` | `["claude-opus", "codex-sol", "grok", "agy-pro"]` |
| `heavy` | `spec` | `["claude-opus", "codex-sol", "grok", "agy-pro"]` |
| `heavy` | `"large input"` | `["agy-pro", "claude-opus", "codex-sol", "grok"]` |
| `frontier` | `default` | `["codex-astra", "claude-fable"]` |

### [prices]

Maestro uses prices to estimate the cost of a Dispatch. Each model has a table named `[prices."<model>"]`. The key is the full model name, not the alias. Prices are USD per 1M tokens, standard tier only.

Keys of `prices.<model>`:

| Key | Type | Default | Meaning |
|---|---|---|---|
| `input` | float | none | Sets the price of 1M input tokens. |
| `cached_input` | float | none | Sets the price of 1M cached input tokens. |
| `output` | float | none | Sets the price of 1M output tokens. |

Default entries:

| Model | `input` | `cached_input` | `output` |
|---|---|---|---|
| `claude-fable-5-1` | `10.00` | `0.25` | `50.00` |
| `claude-opus-5-5` | `4.00` | `0.20` | `20.00` |
| `claude-sonnet-5-5` | `2.00` | `0.10` | `10.00` |
| `claude-sonnet-5` | `2.00` | `0.20` | `10.00` |
| `claude-haiku-5-5` | `0.10` | `0.01` | `0.50` |
| `claude-haiku-4-5` | `1.00` | `0.10` | `5.00` |
| `gpt-6-astra` | `10.00` | `1.00` | `50.00` |
| `gpt-6.1-sol` | `2.00` | `0.10` | `10.00` |
| `gpt-6-sol` | `2.00` | `0.20` | `10.00` |
| `gpt-6-luna` | `0.10` | `0.01` | `0.50` |
| `grok-4.7` | `2.00` | `0.50` | `6.00` |
| `gemini-3.8-flash` | `0.75` | `0.075` | `3.75` |
| `gemini-3.1-pro` | `2.00` | `0.20` | `12.00` |

### [hook]

The hook is the set of `maestro hook` entries that each Adapter installs in a Harness. For what it holds, the Reminder, and the stale Adapter line, see [The hook](reference.md#the-hook).

| Key | Type | Default | Meaning |
|---|---|---|---|
| `scope` | string | `"projects"` | Sets where the hook applies, for both the deny rule and the Reminder: `projects` applies them only inside a project that holds `.agents/maestro.toml` (any folder under it), and `all` applies them everywhere. The Grok rules file states the condition in words instead. |

The read-only command list is hook behavior, and no key changes it; see [Read-only command judgment](reference.md#read-only-command-judgment). A project adds commands for edit Lanes with [`checks` and `allow`](#agentsmaestrotoml).

### [router]

The Router makes the Routes; see [Router](reference.md#router), [Jev](reference.md#jev), and [Reason line](reference.md#reason-line).

| Key | Type | Default | Meaning |
|---|---|---|---|
| `orchestrator` | string | none | Names a Family that the Orchestrator runs on; Maestro marks it `request_only`, so it never spends that model automatically. |
| `backend` | string | `"jev"` | Chooses the Router: `jev` asks Jev first, and `rules` uses only the rules table. |
| `base_url` | string | `"https://api.typesafe.ai/v1/systemone"` | Sets the address of the Jev service. |
| `model` | string | `"jev-1.13.0"` | Names the Jev model that Maestro calls. |
| `thresholds` | table | see below | Holds one table of numbers for each backend. |

### [router.thresholds.jev]

Maestro compares these numbers with the scores that Jev returns. Only the table of the active `backend` is used.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `dispatch` | float | `0.70` | Sets the confidence at or above which Maestro takes the first pick of Jev. |
| `step_up` | float | `0.40` | Sets the confidence at or above which Maestro may take a higher-Tier second option; below it, the rules table decides. |
| `second_option` | float | `0.20` | Sets the least probability that a second option needs before Maestro steps up to it. |
| `vague_brief` | float | `0.30` | Sets the score of "the Brief is concrete" under which Maestro asks the Orchestrator instead of routing. |
| `heavy_override` | float | `0.70` | Sets the confidence at which a Heavy or higher pick skips the vague-Brief check. |
| `council_view` | float | `0.80` | Sets the score of "wants an independent view" above which Maestro suggests a Council. |
| `council_write` | float | `0.30` | Sets the score of "needs to write" under which Maestro suggests a Council. |
| `ask` | float | `0.50` | Sets the confidence at which an "ask the Orchestrator" pick stops the Route. |
| `timeout_s` | float | `1.5` | Sets the number of seconds that Maestro waits for Jev before it uses the rules table. |
| `satisfies` | float | `0.65` | Sets the score at or above which the gate closes a Task as satisfying its Brief; see [`satisfies`](reference.md#satisfies) and [Nudge](reference.md#nudge). Tune the score from `maestro router stats` once the Ledger has rows. |

### [budget]

These keys are the global Budget rules. An Account can override `block_at`. A window is one usage period that a vendor counts for an Account, such as Claude's five-hour and seven-day windows or Codex's weekly window.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `stale_after` | duration | `"30m"` | Sets the age after which a stored Reading is old, and Maestro runs a Probe for a new one. |
| `block_at` | float | `0.90` | Sets the used share of a window at which a Lane leaves the Route list. |
| `warn_at` | float | `0.75` | Sets the used share of a window at which Maestro shows a warning. |
| `assumed_headroom` | float | `0.50` | Sets the Headroom that Maestro assumes for an Account when it has no Reading. |

### [failure]

These keys set when Maestro stops a Dispatch and when it stops a Run. A duration is a string such as `"10m"` or `"1h"`. For Failure classes, Causes, and re-Routes, see [Checks and failure](reference.md#checks-and-failure).

| Key | Type | Default | Meaning |
|---|---|---|---|
| `idle_limit` | duration | `"10m"` | Sets the longest time that a Dispatch or a check may run with no new output before Maestro stops it. It is one value for every Tier. A Lane tool call in progress pauses the Lane's Idle limit; the Wall limit still applies. For checks, output on stdout or stderr resets the Idle limit; see [Check limits](reference.md#check-limits). |
| `wall_limit` | table of durations | see below | Sets the longest total time of a Dispatch for each Tier. The checks share a separate budget of the same length, counted from the start of the first check. |
| `max_dispatches` | integer | `6` | Sets how many Dispatches a Task may use before Maestro fails it instead of re-routing it; see [Re-Route](reference.md#re-route). |
| `keep_awake` | boolean | `true` | Keeps the Mac awake during every Lane, Run, Council, and verb that blocks on live work, and during the Orchestrator's turn in Claude Code, Codex, and Antigravity; see [Keep-awake](reference.md#keep-awake). |
| `run_pause_after_escalations` | integer | `3` | Sets the number of consecutive Task hand-offs to the Orchestrator that pauses a Run; a completed Task resets the count. Maestro prints the count and Task number; running Dispatches finish, and no new Dispatch starts. |
| `run_pause_after_same_error` | integer | `2` | Sets the number of Task hand-offs with the same first error line that pauses a Run; the count accumulates until the Run resumes. Maestro prints the count, Task number, and error; running Dispatches finish, and no new Dispatch starts. |

Default `wall_limit`:

| Tier | Duration |
|---|---|
| `light` | `"15m"` |
| `daily` | `"30m"` |
| `heavy` | `"60m"` |
| `frontier` | `"90m"` |

A Brief can raise the Wall limit for one Task. It cannot lower it.

### [run]

These keys control Runs. The [approval hold](reference.md#approval-holds) rules have no key.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `council_tasks` | integer | `10` | Starts a [plan Council](reference.md#plan-council) when a held Plan has at least this many Tasks. `0` disables the size rule. |
| `plan_check` | boolean | `true` | Starts a [Plan check](reference.md#plan-check) before a Run with two or more edit Tasks. `maestro run --no-plan-check` skips it for one Run. |
| `worktrees` | boolean | `true` | Permits Worktree mode; see [Mode choice](reference.md#mode-choice). |

Worktrees are on by default. To turn them off for every project, set `[run] worktrees = false` in `~/.maestro/config.toml`. To turn them off for one project, set `worktrees = false` in `.agents/maestro.toml`. The project key cannot turn Worktrees on when the global key is off.

### [concurrency]

These keys set how many Lanes run at the same time.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `max_parallel` | integer | `4` | Sets how many Council Seats or Run Tasks run at the same time. |
| `max_per_account` | integer | none | Caps concurrent Run Dispatches on one Account across the machine when set to a positive number. An absent key or `0` gives the Account no cap of its own; `max_total` still applies across Runs. A Task at a set Account cap waits with `account cap <id>` until a slot is free. |
| `max_total` | integer | `6` | Caps concurrent Run Dispatches across the machine. A Task waits with `machine cap` until a slot is free. |
| `close_tabs` | string | — | Accepted for old user files. Has no effect; `maestro doctor` reports the key. |

### [commit]

This table sets the message of each commit that Maestro makes; see [Commits](reference.md#commits). A project `commit_message` in `.agents/maestro.toml` replaces it.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `message` | string | see below | Sets the commit message template. |

The default is:

```toml
[commit]
message = "{title}\n\nCo-Authored-By: {lane}"
```

| Placeholder | Value |
|---|---|
| `{title}` | The Task title. |
| `{run}` | The Run id. |
| `{task}` | The Task id. |
| `{lane}` | The Lane identity, including its email address. |
| `{family}` | The Family id. |
| `{model}` | The model name. |
| `{summary}` | The Summary section of the Task's Report. |

For example, this template adds a prefix and the Report summary, with no trailer:

```toml
[commit]
message = "feat: {title}\n\n{summary}"
```

After Maestro fills the template, it removes each line that a value made empty. It keeps one blank line where the template has blank lines, and it removes blank lines at the end. Thus, an empty Summary leaves no extra blank line and no trailing newline.

### [commit.identities]

This table replaces `{lane}` by Family prefix. Each key is a Family prefix, and each value is the full identity text.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `<family prefix>` | string | see below | Sets the identity that `{lane}` gives for Families with this prefix. |

With no override, Maestro uses these identities:

| Family prefix | Default identity |
|---|---|
| `codex` | `Codex <noreply@openai.com>` |
| `claude` | `Claude <model name and version> <noreply@anthropic.com>`, for example `Claude Opus 5.5 <noreply@anthropic.com>`. |
| `grok` | `Grok <noreply@x.ai>` |
| `agy` | `Gemini <noreply@google.com>` |

```toml
[commit.identities]
codex = "Build assistant <assistant@example.com>"
```

## .agents/maestro.toml

This file is in the root of a project, and it applies to that project only. [`maestro init`](reference.md#init) writes the file. A missing file is empty.

The keys `forbid`, `checks`, `router`, `redact`, `councils`, and `worktrees` tighten what the user file allows. The keys `allow`, `dirs`, and `claude_auto` widen it: they give edit Dispatches more permission rules, more writable directories, or the Claude Code auto permission mode. `commit_message` replaces the commit message template for this project.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `forbid` | array of strings | none | Lists the Families that Maestro never routes to in this project. |
| `worktrees` | boolean | none | Turns Worktrees off with `false`; `true` cannot override a global `false`. See [`[run]`](#run). |
| `worktree_links` | array of strings | none | Adds project paths, relative to the project root, to the built-in Worktree links. Each path must exist and be ignored. A `touch` that names it or a path inside it forces tree mode. See [Links](reference.md#links). |
| `commit_message` | string | none | Replaces the global commit message template for this project. See [`[commit]`](#commit). |
| `checks` | array of strings | none | Lists the commands that Maestro runs after every edit Dispatch, and that the Lane may run. See [Checks and failure](reference.md#checks-and-failure). |
| `allow` | array of strings | none | Lists extra permission rules that Maestro gives to every edit Dispatch in this project. |
| `router` | string | none | When it is `rules`, Maestro never calls Jev for this project, so no text of the project leaves the machine. |
| `redact` | array of strings | none | Lists strings that Maestro replaces before it sends text to Jev. |
| `councils` | string | none | When it is `always`, every Run in this project ends with a Council, except a Run in which every Task is read-only and no file changed; that Run ends without a Council. |
| `claude_auto` | boolean | `false` | Uses the Claude Code auto permission mode for edit Lanes in this project; see [Claude Code](reference.md#claude-code). |
| `dirs` | array of strings | none | Lists extra writable directories for every edit Dispatch in this project, for example a toolchain cache; a leading `~` is the home directory, and a Brief's `dirs:` adds to the list. The key exists because a Harness sandbox can deny a build cache outside the project, which fails every check until the cache folder is writable. |

For example, `worktree_links = ["storage", "config/license.key"]` links a `storage` folder and a license file into each Worktree.

An `allow` rule uses the hook's `Bash(<prefix> *)` form. A trailing ` *` permits arguments; a bare rule permits only that command. For how the hook matches a command against `allow` and `checks`, including the leading `env` words and assignments that it removes, see [Read-only command judgment](reference.md#read-only-command-judgment).
