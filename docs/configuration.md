# Configuration

Maestro runs with no configuration file. It reads three layers, in this order: the defaults compiled into the binary, then `~/.maestro/config.toml` (your deltas), then `.agents/maestro.toml` in the project root. `MAESTRO_HOME` moves the `~/.maestro` folder. A delta is a change on top of the defaults: a family or a chain that you name in your file replaces that one entry, and every entry you do not name keeps its default. Maestro never rewrites your files.

A key that Maestro does not know gives a warning. It does not stop a Route.

## ~/.maestro/config.toml

This file holds only your deltas. A missing file is not an error.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `version` | integer | `1` | Names the version of the file format. |

### [accounts]

An Account is one login that Lanes bill against. Each Account is a table named `[accounts.<id>]`. When your file names an Account for a Harness, that Account replaces every default Account of the same Harness. Maestro binds each Family to the one Account of its Harness. If a Harness has more than one Account, Maestro stops with an error, so keep one Account for each Harness.

Keys of `accounts.<id>`:

| Key | Type | Default | Meaning |
|---|---|---|---|
| `harness` | string | none | Binds the Account to one Harness, and so to every Family of that Harness. |
| `billing` | string | `"subscription"` | Sets the billing kind: `subscription` reads a usage window, `metered` counts money against a cap. |
| `plan_price` | float | none | Gives the plan price in USD, which Maestro multiplies by Headroom to break a tie between equally fit subscription Lanes. |
| `block_at` | float | `0.90` | Sets the used share of a window at which the Account's Lanes leave the Route list. |
| `reserve` | float | none | Keeps this share of the weekly window for the Orchestrator, so automatic Dispatches cannot use it. |
| `paid_overflow` | boolean | `false` | Reserved: Maestro reads the value, and no verb uses it yet. |
| `budget_source` | string | none | Names where the Budget comes from: `harvest+probe`, `computed`, or `none`. |
| `budget` | table | none | Sets the spend cap of a `metered` Account. |
| `budget.usd` | float | none | Sets the cap in USD. |
| `budget.period` | string | none | Sets the period of the cap, for example `30d`. |
| `base_url` | string | none | Reserved: Maestro reads the value, and no verb uses it yet. |

A new Account in your file starts from `billing = "subscription"` and `block_at = 0.90`. For an Account that has a default entry, Maestro changes only the keys you write.

Default entries:

| Account | `harness` | `billing` | `block_at` | `paid_overflow` | `budget_source` |
|---|---|---|---|---|---|
| `claude` | `"claude"` | `"subscription"` | `0.90` | `false` | `"harvest+probe"` |
| `codex` | `"codex"` | `"subscription"` | `0.90` | `false` | `"harvest+probe"` |
| `grok` | `"grok"` | `"subscription"` | `0.90` | `false` | `"none"` |
| `agy` | `"agy"` | `"metered"` | `0.90` | none | `"computed"` |

### [harness]

A table named `[harness.<name>]` holds the settings for one Harness: `claude`, `codex`, `grok`, or `agy`. A key that you write replaces the default for that key only.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `bin` | string | none | Sets the path of the Harness program; when it is empty, Maestro finds the program itself. |
| `min_version` | string | see below | Sets the oldest Harness version that `maestro doctor` accepts. |
| `strip_env` | array of strings | see below | Lists the environment variables that Maestro removes before it starts a Lane on this Harness. |
| `solo_tool` | integer | none | Sets the Solo agent tool id that Maestro uses to open a tab for this Harness; `0` means use the discovered id. |

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
| `haiku` | `"claude-haiku-4-5"` |
| `sonnet` | `"claude-sonnet-5"` |
| `opus` | `"claude-opus-5-5"` |
| `fable` | `"claude-fable-5-1"` |

### [families]

A Family is one Harness × model pair, the unit that the Router chooses between. Each Family is a table named `[families.<id>]`. If your file names a Family that has a default entry, Maestro changes only the keys you write. If it names a new Family, you must write `harness`, or Maestro ignores the entry with a warning.

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

`account`, `request_only`, and `enabled` have no default entry. A default Family has `request_only = false` and `enabled = true`. Setting `[router] orchestrator` to a Family id makes that Family `request_only`.

### [rules]

The rules table is the deterministic Router. It has one table for each Tier: `[rules.<tier>]` with `light`, `daily`, `heavy`, or `frontier`. Each key in a Tier table is a category, and each value is an ordered chain of Family ids. The key `default` is the chain for a Task whose category has no chain of its own. When your file names a category, that chain replaces the default chain for that category only. Other categories keep their defaults.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `default` | array of strings | see below | Sets the chain that Maestro uses for a category that has no chain of its own. |
| `<category>` | array of strings | see below | Sets the chain for one category, for example `implement`; Maestro tries the Families in order. |

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
| `frontier` | `default` | `["codex-astra"]` |

A category name with a space must be in quotes in TOML.

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
| `claude-fable-5-1` | `10.00` | `1.00` | `50.00` |
| `claude-opus-5-5` | `4.00` | `0.40` | `20.00` |
| `claude-sonnet-5` | `2.00` | `0.20` | `10.00` |
| `claude-haiku-4-5` | `1.00` | `0.10` | `5.00` |
| `gpt-6-astra` | `10.00` | `1.00` | `50.00` |
| `gpt-6.1-sol` | `2.00` | `0.20` | `10.00` |
| `gpt-6-sol` | `2.00` | `0.20` | `10.00` |
| `gpt-6-luna` | `0.10` | `0.01` | `0.50` |
| `grok-4.7` | `2.00` | `0.50` | `6.00` |
| `gemini-3.8-flash` | `0.75` | `0.075` | `3.75` |
| `gemini-3.1-pro` | `2.00` | `0.20` | `12.00` |

### [hook]

The hook is the `maestro hook` entry that each Adapter installs in a Harness.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `scope` | string | `"projects"` | Sets where the hook applies: `projects` applies the rule only in a folder that holds `.agents/maestro.toml`, and `all` applies it everywhere. |

### [router]

The Router makes the Routes. The backend is Jev or the rules table, and Maestro uses the rules table when Jev is absent or sure of nothing.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `orchestrator` | string | none | Names a Family that the Orchestrator runs on; Maestro marks it `request_only`, so it never spends that model automatically. |
| `backend` | string | `"jev"` | Chooses the Router: `jev` asks Jev first, and `rules` uses only the rules table. |
| `base_url` | string | `"https://api.typesafe.ai/v1/systemone"` | Sets the address of the Jev service. |
| `model` | string | `"jev-1.13.0"` | Names the Jev model that Maestro calls. |
| `thresholds` | table | see below | Holds one table of numbers for each backend. |

#### [router.thresholds.jev]

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
| `satisfies` | float | `0.90` | Sets the score at or above which the gate closes a Task as satisfying its Brief. A `pass` verdict under this score ends the Task `unverified` rather than failed, so correct work is kept and the Orchestrator reads the diff; tune the score from `maestro router stats` once the ledger has rows. |

### [budget]

These keys are the global Budget rules. An Account can override `block_at`.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `stale_after` | duration | `"30m"` | Sets the age after which a stored Reading is old, and Maestro runs a Probe for a new one. |
| `block_at` | float | `0.90` | Sets the used share of a window at which a Lane leaves the Route list. |
| `warn_at` | float | `0.75` | Sets the used share of a window at which Maestro shows a warning. |
| `assumed_headroom` | float | `0.50` | Sets the Headroom that Maestro assumes for an Account when it has no Reading. |

### [failure]

These keys set when Maestro stops a Dispatch and when it stops a Run. A duration is a string such as `"10m"` or `"1h"`.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `idle_limit` | duration | `"10m"` | Sets the longest time that a Dispatch may run with no new output before Maestro stops it. |
| `wall_limit` | table of durations | see below | Sets the longest total time of a Dispatch for each Tier. |
| `max_dispatches` | integer | `6` | Sets how many Dispatches a Task may use before Maestro fails it instead of re-routing it. |
| `keep_awake` | boolean | `true` | Keeps the Mac awake with `caffeinate` while a Dispatch or a Run is live. |
| `cooling_period` | duration | `"1h"` | Reserved: Maestro reads the value, and no verb uses it yet. |
| `run_pause_after_escalations` | integer | `3` | Reserved: Maestro reads the value, and no verb uses it yet. |
| `run_pause_after_same_error` | integer | `2` | Reserved: Maestro reads the value, and no verb uses it yet. |

Default `wall_limit`:

| Tier | Duration |
|---|---|
| `light` | `"15m"` |
| `daily` | `"30m"` |
| `heavy` | `"60m"` |
| `frontier` | `"90m"` |

A Brief can raise the Wall limit for one Task. It cannot lower it.

### [concurrency]

These keys set how many Lanes run at the same time.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `max_parallel` | integer | `4` | Sets how many Council Seats or Run Tasks run at the same time. |
| `max_per_account` | integer | `2` | Reserved: Maestro reads the value, and no verb uses it yet. |
| `max_total` | integer | `6` | Reserved: Maestro reads the value, and no verb uses it yet. |
| `close_tabs` | string | `"on-success"` | Chooses when Maestro closes the Solo tab of a Lane: `on-success` closes it after a verified, answered, or unverified result, `always` closes it every time, and `never` leaves it open. |

## .agents/maestro.toml

This file is in the root of a project. It can only tighten what the user file allows, and it applies to that project only. A missing file is empty.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `forbid` | array of strings | none | Lists the Families that Maestro never routes to in this project. |
| `pattern_setting` | string | none | Reserved: Maestro reads the value, and no verb uses it yet. |
| `checks` | array of strings | none | Lists the commands that Maestro runs after every edit Dispatch, and that the Lane may run. |
| `allow` | array of strings | none | Lists extra permission rules that Maestro gives to every edit Dispatch in this project. |
| `router` | string | none | When it is `rules`, Maestro never calls Jev for this project, so no text of the project leaves the machine. |
| `redact` | array of strings | none | Lists strings that Maestro replaces before it sends text to Jev. |
| `councils` | string | none | When it is `always`, every Run in this project ends with a Council. |
| `claude_auto` | boolean | `false` | Lets Maestro use the Claude Code auto permission mode for Dispatches in this project. |
| `dirs` | array of strings | none | Lists extra writable directories for every edit Dispatch in this project, for example a toolchain cache; a leading `~` is the home directory, and a Brief's `dirs:` adds to the list. The key exists because a Harness sandbox can deny a build cache outside the project, which fails every check until the cache folder is writable. |

## Generated state

Maestro writes these paths itself. Do not edit them by hand. `maestro clean` removes old Run and Council folders. It never touches the ledger.

| Path | Written by |
|---|---|
| `~/.maestro/env` | `maestro key set` writes the Jev key. |
| `~/.maestro/ledger.jsonl` | Every finished Dispatch, Council, and Run appends one row. |
| `~/.maestro/budget.json` | Dispatches (Harvest) and `maestro budget` (Probe) store Readings. |
| `~/.maestro/cache/discovery.json` | `maestro roster refresh` rewrites it, and any verb rewrites it when it is stale. |
| `~/.maestro/runs/` | `maestro run` writes one index file for each live Run. |
| `.agents/maestro/runs/` | `maestro dispatch` and `maestro run` write one Run folder for each Run. |
| `.agents/councils/` | `maestro council` writes one folder for each Council. |
