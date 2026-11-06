# Configuration

Maestro runs with no configuration file. It reads three layers, in this order: the defaults compiled into the binary, then `~/.maestro/config.toml` (your deltas), then `.agents/maestro.toml` in the project root. `MAESTRO_HOME` moves the `~/.maestro` folder. A delta is a change on top of the defaults: a family or a chain that you name in your file replaces that one entry, and every entry you do not name keeps its default. Maestro never rewrites your files.

A key that Maestro does not know gives a warning. It does not stop a Route.

Every Lane runs as a headless child process on the one-shot path, inside Solo and outside it. This includes Run Tasks, single Dispatches, and Council Seats. Solo supplies todos, Report and Merge comments, and wake-ups.

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
| `billing` | string | `"subscription"` | Sets the billing kind: `subscription` reads a usage window, `metered` counts money against a cap. An automatic Route gives a `metered` Account read work only, and only when no subscription Lane serves the level; a requested Lane on a `metered` Account takes edit work with a warning. |
| `plan_price` | float | none | Gives the plan price in USD, which Maestro multiplies by Headroom to break a tie between equally fit subscription Lanes. |
| `block_at` | float | `0.90` | Sets the used share of a window at which the Account's Lanes leave the Route list. |
| `reserve` | float | none | Keeps this share of the weekly window for the Orchestrator, so automatic Dispatches cannot use it. |
| `budget` | table | none | Sets the spend cap of a `metered` Account. |
| `budget.usd` | float | none | Sets the cap in USD. |
| `budget.period` | string | none | Sets the period of the cap, for example `30d`. |

A new Account in your file starts from `billing = "subscription"` and `block_at = 0.90`. For an Account that has a default entry, Maestro changes only the keys you write.

Default entries:

| Account | `harness` | `billing` | `block_at` |
|---|---|---|---|
| `claude` | `"claude"` | `"subscription"` | `0.90` |
| `codex` | `"codex"` | `"subscription"` | `0.90` |
| `grok` | `"grok"` | `"subscription"` | `0.90` |
| `agy` | `"agy"` | `"subscription"` | `0.90` |

### [harness]

A table named `[harness.<name>]` holds the settings for one Harness: `claude`, `codex`, `grok`, or `agy`. A key that you write replaces the default for that key only.

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

The Maestro hook installed by `maestro adapter install agy` or `maestro adapter install grok` holds the Access of Antigravity and Grok Lanes; Grok edit Lanes also need the workspace sandbox for `enforced` Access. A Grok Lane in a Worktree keeps `--sandbox workspace` when no extra writable directory is outside its root. It reads the Brief by absolute path and returns the Report in its final message, so the Run folder needs no write access. Extra writable directories outside the Worktree still make the launch `best-effort`; the Route records the launch's Enforcement.

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
| `council` | string | none | Sets the effort of an automatic Council Seat of the Family, whatever the question's level; a requested Seat (`--lane family:effort`) keeps its own effort. The default Roster sets `council = "medium"` for `grok`, whose Seats at `high` took two to three times as long as the other Seats. |

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
| `frontier` | `default` | `["codex-astra", "claude-fable"]` |

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

The hook is the set of `maestro hook` entries that each Adapter installs in a Harness: a `PreToolUse` entry, and for Claude Code, Codex, and Antigravity a second entry for the Reminder event (Antigravity's runs `maestro hook agy`). For Antigravity, Grok, and Claude Code the `PreToolUse` entry also holds each Lane's permissions, because those harnesses' own permission rules cannot hold the Access on their own; for Claude Code the hook can only deny, and the Claude sandbox holds the rest. The hook judges a command as written and denies a read-only command that carries a shell expansion.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `scope` | string | `"projects"` | Sets where the hook applies, for both the deny rule and the Reminder: `projects` applies them only inside a project that holds `.agents/maestro.toml` (any folder under it), and `all` applies them everywhere. The Grok rules file states the condition in words instead. |

For a Reminder in scope, the hook compares the installed skill and Grok rules with the text in the binary. If either differs, it adds `Adapter stale (<harnesses>): run maestro adapter install` with each stale Harness's name. Claude Code, Codex, and Antigravity all receive this line, including when only Grok's rules are stale. The check reads local files and makes no network call. Run `maestro adapter install` to update the files and clear the line. Grok has no Reminder event; use `maestro doctor` there. Missing or unreadable files remain checks for `maestro doctor`.

#### Read-only commands

The hook's read-only command list is the baseline for every project. It serves Go, Node, PHP, Python, Rust, and Docker projects. These commands need no project `allow` entry. A command's write or execution forms are denied even when a check or `allow` entry names the command.

A read-only command cannot change the project tree, the user's files outside the tool's own folders, or the system. Writes to a tool's own cache, log, telemetry, or module download folders under the user's home, such as `~/.npm/_logs`, `~/Library/Caches/go-build`, `~/go/pkg/mod`, Go telemetry counters, `~/.composer/cache`, pip's cache, and Docker's own state, do not count as changes under this rule.

| Group | Commands |
|---|---|
| Text and files | `cat`, `ls`, `head`, `tail`, `grep`, `rg`, `find`, `sed`, `wc`, `diff`, `stat`, `file`, `which`, `tree`, `du`, `df`, `basename`, `dirname`, `realpath`, `pwd`, `cut`, `sort`, `uniq`, `tr`, `jq`, `yq`, `test`, `[` |
| Version control | `git log`, `git diff`, `git show`, `git status`, `git blame`, `git branch` in its current list forms, `git ls-files`, `git describe`, `git rev-parse`, `git tag` in list forms |
| Toolchains: versions | `go version`, `node --version`, `php --version`, `composer --version`, `python3 --version`, `pip --version`, `docker --version` |
| Toolchains: environment, packages, and docs | `go env`, `go list`, `go doc`, `composer show --no-plugins`, `pip list`, `pip show` |
| Containers | `docker version`, `docker info`, `docker ps`, `docker images`, `docker compose ps`, `docker compose config` |
| Other | `echo`, `date` |

The following limits apply:

- `sed` denies `-i`, BSD `-I`, `--in-place`, script files (`-f`, `--file`), and scripts that the hook cannot judge. The hook accepts simple numeric or regular expression addresses, groups, read commands, and substitutions. It denies `w`, `W`, `e`, and substitution flags that write or execute. This also applies with `-s`. Bracket expressions, control escapes, and other unsupported script syntax are denied.
- `file` denies `-C`, `--compile`, timestamp changes (`-p`, `--preserve-date`), and decompression forms that can run an external program (`-z`, `-Z`, `--uncompress`, `--uncompress-noreport`).
- `tree` denies `-o`, `-R`, and `-H`. `sort` denies `-o`, `--output`, and `--compress-program`. `uniq` denies two operands because its second operand is an output file.
- `rg` denies `--pre`, `--pre-glob`, and `--hostname-bin`. `find` denies `-exec`, `-execdir`, `-delete`, `-fprint`, `-fprint0`, `-fprintf`, `-fls`, `-ok`, and `-okdir`.
- `jq` has no file write form. `yq` denies `-i`, `--inplace`, `--in-place`, `-s`, `--split-exp`, and `--split-exp-file`.
- `git log`, `git diff`, and `git show` deny `--output`, `--ext-diff`, and `--textconv`. `git describe` denies `--dirty` and `--broken` because they can update the index. `git branch` keeps its current list forms. `git tag` accepts a bare command or list forms with `-l`, `--list`, `-n`, `--contains`, `--points-at`, and `--sort`. A tag name without a list form is denied. Creation, annotation, deletion, signing, force, and message options are denied.
- `go env` denies `-w` and `-u`. `go list` denies module changes (`-mod=mod`, `-modfile`), output files (`-o`, `-debug-actiongraph`, `-pkgdir`), program execution (`-toolexec`), and flags passed to compilers or linkers (`-asmflags`, `-gccgoflags`, `-gcflags`, `-ldflags`). `go doc` denies the HTTP server form (`-http`). Go's double dash forms have the same limits.
- `composer show` requires `--no-plugins` before `--`. `composer --no-plugins show` is also accepted. Without this option, Composer can load project plugins from `vendor/`. `--no-scripts` alone does not disable plugins. `composer --version` does not load those plugins.
- `npm --version`, `npm ls`, `npm view`, `pnpm --version`, `pnpm ls`, and `yarn --version` are outside the list. Corepack can run a package manager that the project's `packageManager` field names. Yarn can also use `yarnPath`; pnpm can load `.pnpmfile.cjs` or `.pnpmfile.mjs` and select a package manager version from project settings. `cargo --version` and `rustc --version` are outside the list because rustup can run a local toolchain that `rust-toolchain.toml` names.
- A `--version` form accepts no extra arguments. `pip list` and `pip show` deny `--log` and `--python`; `--python` can run a project file as an interpreter. `docker compose config` denies `-o`, `--output`, and `--lock-image-digests`.

`node --version`, `python3 --version`, `php --version`, and `go version` stay on the list under version managers (asdf, mise, pyenv) because these shims select an installed interpreter by a version string from the project and do not run project files; a `path:` version in `.tool-versions` is the user's own machine configuration, not repository content.

These checks use the command's documented forms: [sed](https://www.gnu.org/software/sed/manual/sed.html), [file](https://man.freebsd.org/cgi/man.cgi?query=file&sektion=1&format=html), [yq](https://mikefarah.gitbook.io/yq/usage/split-into-multiple-files), [Git tag](https://git-scm.com/docs/git-tag), [Go](https://pkg.go.dev/cmd/go), and [pip](https://pip.pypa.io/en/stable/cli/pip/).

The toolchain limits follow the documented behavior of [Composer plugins](https://getcomposer.org/doc/articles/plugins.md), [Corepack's package manager selection](https://github.com/nodejs/corepack#when-authoring-packages), [pnpm hooks](https://pnpm.io/pnpmfile), and [rustup overrides](https://rust-lang.github.io/rustup/overrides.html).

Tests, builds, formatters, and package installs stay outside this list. This includes `go test`, `npm test`, `composer test`, `pytest`, `cargo test`, and `cargo metadata`, which can write `Cargo.lock`. An edit Lane needs a project `checks` or `allow` entry to run these commands. A read Lane cannot run them. The deny reason names every accepted read-only command.

This list does not grant shell loops, command substitution, redirects, or a pipe to a write command. The existing shell and path checks still apply. The hook denies `$`, backquotes, and unquoted `(`, `{`, or `}` in read-only commands, including zsh glob qualifiers that can run code and brace lists that can add flags. A glob word that starts with an unquoted `*`, `?`, or `[` is denied: `glob can match a file name that starts with -; write ./ before it`. A glob after a fixed prefix is permitted, such as `grep -r --include=*.go x .`, `ls ./*_test.go`, and `cat src/**/*.php`.

### [router]

The Router makes the Routes. The backend is Jev or the rules table, and Maestro uses the rules table when Jev is absent or sure of nothing.

Jev's Judgment level sets the Task's Tier. The Brief's `level` is the Orchestrator's estimate. `maestro dispatch --level` and `maestro run --level` replace that estimate when set. The estimate goes to Jev as context and never raises Jev's score or limits its pick. The rules table uses the estimate when Jev is absent or below the confidence floor. Only a Brief's `floor:` and `ceiling:`, or `maestro run --floor` and `--ceiling`, set hard Tier bounds. An edit never goes Light. When Jev's score is below the estimate, the Reason line says, for example, `jev some, orchestrator estimated expert`. The Route record keeps both values.

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
| `satisfies` | float | `0.65` | Sets the score at or above which the gate closes a Task as satisfying its Brief. A `pass` verdict under this score can get one nudge before the Task ends `unverified`; see the rule below. Tune the score from `maestro router stats` once the Ledger has rows. |

The gate asks one `noul` question for each Done item, keyed as `done_1`, `done_2`, and so on, with the `cause`, `satisfies`, `verdict`, and `risk` questions. It records the item scores under `verification.gate.done_items`. If the checks pass, the verdict is `pass`, the risk is `none`, `low`, or `medium`, and `satisfies` is under the close threshold, Maestro gives the same Lane one nudge. For this rule, the checks pass when no checks are set. The retry Brief tells the Lane to keep the previous work, which passed the checks and the gate's verdict, and lists each Done item with a score under `0.5`. These scores are not confirmed defects. An absent item score does not count as a missing item. With no missing items, the Task ends `unverified` at once.

Each Task gets at most one nudge, including across re-Routes. The nudge adds one attempt to the Dispatch cap and the Run's Dispatch cap, so the existing retry after a Lane error or Transient error still fits. The nudge attempt uses the normal checks and gate: a passing result at the close threshold is verified; another result follows the normal outcome rules. There is no second nudge. The Ledger marks the nudge attempt and later attempts with `nudged: true`. Earlier Reports stay under `attempts/<n>/`; `maestro wait` prints the last attempt's Report.

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

For a Task of a Run, Lane error, Transient error, Limit hit, and Stall re-Route one Tier up. A Bad result re-Routes only when its Cause is `capability`: the Lane's work is wrong or incomplete with no blocker. Cause `brief` means the Brief excludes a required file, forbids required work, or contradicts itself. Cause `environment` means a tool or service was denied, down, or missing. Cause `none` means nothing went wrong; on a Bad result, it needs the Orchestrator's judgment. These three Causes block the Task for the Orchestrator, with the Cause and the Blocked items. A blocked Report stays blocked. The Dispatch cap, Tier ceiling, and same-error-twice rule still apply.

A touch violation has Cause `brief` without a Jev call. Otherwise Jev answers the `cause` choice question with the gate's `satisfies`, `verdict`, and `risk` questions, or answers `cause` alone for a blocked or partial Report or failed checks. The choices are `brief`, `environment`, `capability`, and `none` (nothing went wrong). The state includes the Brief's title, Goal, and Done, the Report's Blocked section and Status, failed check output, and the diff when present. Each text field is limited to 60 KiB. If `router = "rules"`, no key is available, or Jev fails, a rule supplies the Cause: a touch violation or blocked Report is `brief`; a partial Report with Blocked items or a failed Docker pre-flight is `environment`; other failures are `capability`. The Record and Ledger store `scored: true` when Jev answers the full gate questions. They keep `satisfies`, `verdict_confidence`, and `risk_confidence` even when the value is zero. A Gate with only a Cause has `scored: false`; its zero numbers are not scores. `maestro wait` and `maestro status` use `scored` to show either `satisfies <n> verdict <v> risk <r>` or `cause <value> (<source>)`.

A partial Report with Blocked items keeps its Bad result and Cause when a later check reaches its Idle limit or Wall limit. The check still records its Stall. Without Blocked items, the Stall replaces the Report's outcome. This rule applies only to a Stall. A failed Docker pre-flight still sets the outcome's error text to the check failure and keeps Cause `environment` under the rule. A path from an earlier attempt leaves the changed list and the touch guard when it returns to its earliest baseline; it stays while it still differs.

The Ledger stores `cause` and `cause_source` (`jev` or `rule`) under `verification.gate`, and `decision` under `outcome`. A re-Route names the Cause in its Reason line. A Task blocked by its Cause reaches the Orchestrator through the Run report, `maestro wait`, the Reminder, and the Solo wake-up with the Cause and the Lane's Blocked items.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `idle_limit` | duration | `"10m"` | Sets the longest time that a Dispatch or a check may run with no new output before Maestro stops it. A Lane tool call in progress pauses the Lane's Idle limit; the Wall limit still applies. For checks, output on stdout or stderr resets the Idle limit. |
| `wall_limit` | table of durations | see below | Sets the longest total time of a Dispatch for each Tier. The checks share a separate budget of the same length, counted from the start of the first check. |
| `max_dispatches` | integer | `6` | Sets how many Dispatches a Task may use before Maestro fails it instead of re-routing it. |
| `keep_awake` | boolean | `true` | Keeps the Mac awake during every Lane, Run, Council, and verb that blocks on live work, and during the Orchestrator's turn in Claude Code, Codex, and Antigravity. See below. |
| `run_pause_after_escalations` | integer | `3` | Sets the number of consecutive Task hand-offs to the Orchestrator that pauses a Run; a completed Task resets the count. Maestro prints the count and Task number; running Dispatches finish, and no new Dispatch starts. |
| `run_pause_after_same_error` | integer | `2` | Sets the number of Task hand-offs with the same first error line that pauses a Run; the count accumulates until the Run resumes. Maestro prints the count, Task number, and error; running Dispatches finish, and no new Dispatch starts. |

With `keep_awake = true`, each Maestro process shares one `caffeinate -i -s -w <pid>` while it works or waits on live work. This includes `maestro wait` for a Dispatch, Run, or Council, the `--wait` paths, the Relay verb, and a hook that waits for a Relay answer. `-i` prevents idle sleep on battery and AC power; `-s` also prevents system sleep on AC power. The process releases its assertion when the work or wait ends. This does not prevent display sleep.

In hook scope, a Reminder event also starts a detached `caffeinate -i -s -w <orchestrator pid> -t 1800`. The hook writes the Reminder and passes its parent pid to a detached worker. The worker finds the nearest ancestor whose executable matches a configured Harness binary and writes the lease record. It does nothing when no ancestor matches. Reminder and Stop events do no sleep-control work when `keep_awake = false`, outside hook scope, or in a Lane. Each later Reminder event replaces the same process's caffeinate and refreshes its 30-minute lease. The record is `~/.maestro/control/awake/<pid>`. Each worker removes records for Orchestrator processes that have ended.

| Harness | Starts or refreshes the lease | Ends the lease |
|---|---|---|
| Claude Code | `UserPromptSubmit` | `Stop`, process exit, or 30 minutes after the last Reminder event, whichever comes first |
| Codex | `UserPromptSubmit` | `Stop`, process exit, or 30 minutes after the last Reminder event, whichever comes first |
| Antigravity | `PreInvocation` | `Stop`, process exit, or 30 minutes after the last Reminder event, whichever comes first |
| Grok | None: its Carrier is a rules file, with no Reminder event | No Orchestrator turn protection; its Lanes and blocking Maestro verbs still have protection |

The lease protects a turn, not an idle Harness session. If `Stop` does not reach Maestro, the lease releases the Mac within 30 minutes of the last Reminder event. Run `maestro adapter install` again to add the `Stop` entries; `maestro doctor` reports a missing entry as a partial Adapter. In Codex, review and trust the new `Stop` hook in `/hooks`. The current [Codex hooks](https://learn.chatgpt.com/docs/hooks) and [Antigravity hooks](https://antigravity.google/docs/hooks) both provide `Stop`.

Default `wall_limit`:

| Tier | Duration |
|---|---|
| `light` | `"15m"` |
| `daily` | `"30m"` |
| `heavy` | `"60m"` |
| `frontier` | `"90m"` |

A Brief can raise the Wall limit for one Task. It cannot lower it.

A check has no fixed time cap. When stdout and stderr stay silent for the Idle limit, Maestro kills the process group and fails the check with `no output for <idle limit>` and Failure class Stall. When the checks' budget ends, Maestro kills the process group and fails the check with `wall limit` and Failure class Stall.

Before the first check whose command starts with `docker` (including `docker compose`), Maestro runs `docker info` once per Task. If it fails or does not answer within 10 seconds, the check fails with `Docker does not respond` and Failure class Bad result. The checks' budget also applies to this pre-flight.

After an Idle limit, a Wall limit, or a failed Docker pre-flight, Maestro skips the remaining checks and records the same reason. A check that fails on its own exit code does not stop the next check. The Ledger and `maestro wait` show the reason and Failure class.

### [run]

In a Plan file, text after the front matter or optional `# title` and before the first `## <task title>` is the introduction. Maestro adds it to the start of every Task's Context under `Shared context of the Plan:`, before the Task's own `### Context` text. An empty introduction adds nothing. `maestro help plan` prints the format.

These keys control Runs. The approval hold rules are separate: more than four Tasks, a medium or fallback Route, a Judgment level two steps from the Router's, a Router question, or a Router Council signal.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `council_tasks` | integer | `10` | Starts a plan Council when a held Plan has at least this many Tasks. `0` disables the size rule. |
| `worktrees` | boolean | `true` | Permits Worktree mode. A project can turn it off, but cannot turn it on when this key is `false`. See [Worktrees](#worktrees). |

`maestro run --council <plan>` requests a plan Council for a held Run. A Router question or Council signal for any Task also starts it. `--no-council` stops the plan Council in every case, including when `--council` is set. A hold alone does not start a Council.

Fold any Council findings into the Plan file, then use `maestro approve <run>`. Approval reads the file at the recorded path again, parses it, prints new provisional Routes, and starts the current Tasks under the same Run id. It reuses the held Task ids in Plan order and removes folders for Tasks that the Plan no longer has. It starts no second plan Council. A parse error leaves the Run held.

## Worktrees

A Run uses Worktree mode when Worktrees are on, the repository has a commit, the tree is clean, and the Plan has more than one edit Task. The mode is recorded once for the Run. Other Runs use tree mode, where an edit Task waits with `tree busy`. In Worktree mode, Tasks with files that overlap wait in Plan order with `files overlap with <n>`; Routes and status show `after <n> (files overlap)`. If an edit Task's `touch` names a shared dependency folder or a path inside it, the Run starts in tree mode with the reason `task <n> touches <folder>`. A detached HEAD requires tree mode with the reason `HEAD is not on a branch`. Maestro does not check out a Task branch in the project tree.

Lanes never commit. Maestro commits a verified edit Task after the gate, or an unverified edit Task with a Report and changed files. In Worktree mode, Maestro merges each Task into the Run branch. The Plan's `commit: true` keeps those commits when the Run lands on the user's branch; `commit: false` leaves the changes uncommitted. In tree mode, Maestro commits only a Task with `commit: true`.

Every read Task of a Worktree-mode Run starts in a detached Worktree at the current Run branch head. A read Task with `blocked_by` the edit Tasks sees their merged changes. It gets the dependency links and keeps read Access. Maestro removes the read Worktree when the Task ends, including after failure or cancellation. Read Tasks have no Task branch.

If a commit fails after verification, the Dispatch record and Ledger keep `outcome.decision: blocked` and the error text. The Run report gives the Worktree path for both blocked and failed Tasks. `maestro wait <task>` follows the Task's Run, with the same Cause, Blocked items, and resume command as `maestro wait <run>`. After an answer, it waits for the Run and does not return the old Dispatch result.

Worktrees live under `~/.maestro/worktrees/<project-key>/<run>/<task>/`. The Run branch is `maestro/<run>/run`; each edit Task branch is `maestro/<run>/<task>`. Maestro links existing ignored dependency folders from the project: `node_modules`, `vendor`, `.venv`, `.bundle`, and `storage/framework`. Before it makes each link, Maestro adds the folder name to the exclusion file that `git rev-parse --git-path info/exclude` returns for the Worktree. This keeps a link ignored when the project rule has a trailing slash, such as `node_modules/`. A folder stays unlinked only when `touch` names the folder or a path inside it, such as `node_modules/**`. No `touch`, an empty list, or a general pattern such as `**` permits the link.

Each commit keeps the user as its git author. The commit message comes from `[commit] message` in `~/.maestro/config.toml`. A project `commit_message` in `.agents/maestro.toml` replaces it. The default is:

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

`[commit.identities]` in `~/.maestro/config.toml` replaces `{lane}` by Family prefix. Each value is the full identity text. With no override, Maestro uses these identities:

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

A merge conflict gets one more attempt in the same Worktree. Maestro starts the rebase onto the Run branch, the Lane edits the conflicted files, and Maestro continues the rebase after the checks and the gate. A second conflict, a conflict marker that stays in a file, or a failed attempt blocks the Task with `merge conflict in <paths>` and keeps its Worktree. To fix it by hand, rebase the Task branch onto the Run branch in that Worktree, resolve the conflicts, and complete the rebase; then `maestro resume <run> --task <n> --merge` retries the merge with no new Lane. After a successful rebase, Maestro removes the Worktree with `--force`, deletes the Task branch, and updates the Run branch only if its old head still matches. A cleanup failure keeps the Task branch for a retry.

If landing fails, the Run pauses with the failed Finish step in the error, and `maestro resume <run> --finish` retries it. Finish is idempotent: it skips a merge that already landed and continues cleanup. After a squash, it continues the reset or removal step. It requires a clean user tree only before the merge step. When the user's branch moved, rebase the Run branch onto the user's branch, or reset the user's branch to the base, before you retry. A cancelled Run removes its Worktrees. Removal deletes the Run and Task branches only when the Run branch is an ancestor of the user's branch or the Run state is `finished`. When only the Run branch is an ancestor, removal also keeps each Task branch that is not an ancestor of the user's branch, because that Task branch has work that did not land. Otherwise, it keeps every branch. Removal returns the names of the kept branches for the Run report. A failed or paused Run keeps its Worktrees and branches until resume or cleanup.

Worktrees are on by default. To turn them off for every project, set `[run] worktrees = false` in `~/.maestro/config.toml`. To turn them off for one project, set `worktrees = false` in `.agents/maestro.toml`. The project key cannot turn Worktrees on when the global key is off.

`maestro doctor` prints `worktrees on` or `worktrees off (<where>)`, the dependency folders it would link, and a warning when the tree has uncommitted changes: `Runs use the tree one Task at a time until you commit or stash`. `maestro clean --older-than 30d` removes old Worktrees and Run folders, and applies the branch removal rule above to finished, cancelled, and paused Runs. It prints each kept branch and returns them as `kept_branches` in JSON. It keeps live Runs, Runs with `finish_pending`, and Runs with a blocked Task.

### [concurrency]

These keys set how many Lanes run at the same time.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `max_parallel` | integer | `4` | Sets how many Council Seats or Run Tasks run at the same time. |
| `max_per_account` | integer | none | Caps concurrent Run Dispatches on one Account across the machine when set to a positive number. An absent key or `0` gives the Account no cap of its own; `max_total` still applies across Runs. A Task at a set Account cap waits with `account cap <id>` until a slot is free. |
| `max_total` | integer | `6` | Caps concurrent Run Dispatches across the machine. A Task waits with `machine cap` until a slot is free. |
| `close_tabs` | string | — | Accepted for old user files. Has no effect; `maestro doctor` reports the key. |

## .agents/maestro.toml

This file is in the root of a project, and it applies to that project only. `maestro init` writes the file. A missing file is empty. The keys `forbid`, `checks`, `router`, `redact`, `councils`, and `worktrees` tighten what the user file allows. The keys `allow`, `dirs`, and `claude_auto` widen it: they give edit Dispatches more permission rules, more writable directories, or the Claude Code auto permission mode. `commit_message` replaces the commit message template for this project.

`maestro init` detects each stack from files at the project root. A project can match more than one stack. It writes active `dirs` and `allow` arrays for all matches. It writes `checks` as commented suggestions, such as `# checks = ["go test ./..."]`. Review the suggestions before you activate them: an incorrect check fails every edit Task. With no match, all keys stay commented.

| Stack | Root files | Cache directories |
|---|---|---|
| Go | `go.mod` | Uses `go env GOCACHE GOMODCACHE`; falls back to `~/Library/Caches/go-build` on macOS or `~/.cache/go-build` on Linux, plus `~/go/pkg/mod`. |
| npm | `package.json`, with `package-lock.json` or no runner lockfile | Uses `npm config get cache`; falls back to `~/.npm`. |
| pnpm | `package.json` and `pnpm-lock.yaml` | Uses `pnpm store path`; falls back to `~/.cache/pnpm`. |
| yarn | `package.json` and `yarn.lock` | Uses `yarn cache dir` and `~/.yarn`; falls back to `~/.yarn` and `~/.cache/yarn`. |
| PHP | `composer.json` | Uses `composer config --global cache-dir`; falls back to `~/.composer/cache` and `~/.cache/composer`. |
| Python | `pyproject.toml` or `requirements.txt` | Uses `pip cache dir`; falls back to `~/.cache/pip`. |
| Rust | `Cargo.toml` | Uses `~/.cargo`. |
| Docker | `Dockerfile`, `compose.yaml`, `compose.yml`, or `docker-compose.yml` | Uses `~/.docker`, plus `~/.orbstack/run` when that folder exists. |

When several Node lockfiles exist, npm takes priority, then pnpm, then yarn. Cache queries run from the project root when the command is on `PATH`. If a query fails, times out, or returns an invalid path, Maestro uses the fallback paths.

The generated allow rules use the hook's `Bash(<prefix> *)` form. A trailing ` *` permits arguments; a bare rule permits only that command. The rules cover Go test, vet, build, and gofmt; the Node runner's test and run commands; Composer test, PHPUnit, Pint, and PHPStan; pytest and ruff; Cargo test, build, clippy, and fmt; and Docker compose and build. Node test, Composer test, and pytest also get a bare rule. `Bash(docker run *)` stays a commented suggestion because it permits host path mounts.

For matching against `allow`, `checks`, and the read-only list, the hook removes leading `env` words, including `env -i` and `env -u NAME`. It removes a leading `NAME=value` assignment only for these harmless names: `CI`, `NO_COLOR`, `FORCE_COLOR`, `TERM`, `COLUMNS`, `LINES`, `LANG`, `LC_ALL`, `TZ`, `CGO_ENABLED`, `GOOS`, `GOARCH`, and `MAESTRO_HOME`. These can be combined. Thus `env -u MAESTRO_HOME go test ./...` matches `Bash(go test *)`, and `CGO_ENABLED=0 go build ./...` matches `Bash(go build *)`. If any other assignment is present, the hook keeps the full command for matching. A read Lane is denied; an edit Lane uses Relay where its Harness supports Relay, or is denied, unless a check or `allow` entry permits that full command. The reason names the variable. This includes `GIT_EXTERNAL_DIFF`, `GIT_CONFIG_COUNT`, and `GOFLAGS`, whose values can make a read command run code. The command runs as written. Write-flag and shell checks still apply to matched read-only commands. The hook does not remove other prefixes, such as `sudo`, `nohup`, or `time`.

If the Project file exists, `maestro init` shows the proposed file and changes nothing. `maestro init --write` merges the additions. It keeps every existing key, value, and comment, and appends only missing values to `dirs` and `allow`. A value in a comment also counts as present, so a value that you comment out stays inactive. It adds a commented `checks` line only when no active or commented `checks` key exists. A second `--write` with the same stack and cache paths adds nothing.

`maestro doctor` warns when a Project file has no active checks. It also warns when a project check starts with `docker` and `dirs` does not include `~/.docker` or its absolute path. These warnings do not fail the doctor command.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `forbid` | array of strings | none | Lists the Families that Maestro never routes to in this project. |
| `worktrees` | boolean | none | Turns Worktrees off with `false`; `true` cannot override a global `false`. |
| `commit_message` | string | none | Replaces the global commit message template for this project. See [Worktrees](#worktrees). |
| `checks` | array of strings | none | Lists the commands that Maestro runs after every edit Dispatch, and that the Lane may run. |
| `allow` | array of strings | none | Lists extra permission rules that Maestro gives to every edit Dispatch in this project. |
| `router` | string | none | When it is `rules`, Maestro never calls Jev for this project, so no text of the project leaves the machine. |
| `redact` | array of strings | none | Lists strings that Maestro replaces before it sends text to Jev. |
| `councils` | string | none | When it is `always`, every Run in this project ends with a Council, except a Run in which every Task is read-only and no file changed; that Run ends without a Council. |
| `claude_auto` | boolean | `false` | Uses the Claude Code auto permission mode for edit Lanes in this project; these Lanes are `best-effort` because a model can approve commands. |
| `dirs` | array of strings | none | Lists extra writable directories for every edit Dispatch in this project, for example a toolchain cache; a leading `~` is the home directory, and a Brief's `dirs:` adds to the list. The key exists because a Harness sandbox can deny a build cache outside the project, which fails every check until the cache folder is writable. |

## Generated state

Maestro writes these paths itself. Do not edit them by hand. `maestro clean --older-than 30d` removes old Run and Council folders in the current project. It removes control folders across all project keys when their Dispatch or Run ended before the threshold, or their record is gone. It keeps live Run controls. It also removes old cache folders, but keeps the latest Canary files per Harness at the path shown by `maestro doctor`. It never touches the Ledger or `budget.json`.

Maestro rejects a Ledger row with no `kind`. It drops Budget Accounts that are absent from the loaded Roster before the next save. Tests must set `MAESTRO_HOME` to a temporary folder. A call to `config.Home()` in a test without that variable stops with a panic.

| Path | Written by |
|---|---|
| `~/.maestro/env` | `maestro key set` writes the Jev key. |
| `~/.maestro/ledger.jsonl` | Every finished Dispatch, Council, and Run appends one row. |
| `~/.maestro/budget.json` | Dispatches (Harvest) and `maestro budget` (Probe) store Readings. |
| `~/.maestro/cache/discovery.json` | `maestro roster refresh` rewrites it, and any verb rewrites it when it is stale. |
| `~/.maestro/control/<project-key>/` | Dispatches store Lane policy and Relay files. |
| `~/.maestro/canaries/` | `maestro doctor --canary` stores the latest check per Harness. |
| `~/.maestro/cache/maestro-canary-<harness>/` | A failed Canary keeps its check files until the next Canary for that Harness. |
| `~/.maestro/runs/` | `maestro run` writes one index file for each live Run. |
| `.agents/maestro/runs/` | `maestro dispatch` and `maestro run` write one Run folder for each Run. |
| `.agents/maestro/councils/` | `maestro council` writes one folder for each Council. |
