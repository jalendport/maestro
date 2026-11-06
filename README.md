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
- **Solo integration** — inside [Solo](https://soloterm.com), Tasks are todos, results are comments, and your session wakes when a Run ends
- **Keeps the Mac awake** — `caffeinate -i -s` protects every Lane, Run, Council, and verb that waits on live work, on battery and AC power. In Claude Code, Codex, and Antigravity, the Orchestrator's turn holds a 30-minute lease that ends on `Stop` or when its process ends. Grok has no turn protection. Set `[failure] keep_awake = false` to turn it off
- **Single static binary** — Go, no runtime dependencies, and each Harness needs only a skill file, hook entries, and for Grok one rules file

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

`maestro doctor` shows the stored Canary result for each installed Harness version and starts no Dispatch. Run `maestro doctor --canary` to check every installed Harness, or `maestro doctor --canary codex` to check one. Each Canary uses your Account for one small read Dispatch with the cheapest enabled Family. It runs on the one-shot path in a scratch folder. Maestro removes that folder after a pass. After a fail, it keeps the files under `~/.maestro/cache/maestro-canary-<harness>`, and the doctor line gives the path. The next Canary for the same Harness removes those files first. A pass needs a process that starts and ends with no error, a Lane tool call judged by the Maestro hook, and a Report with a valid Status line. For Codex, the hook part does not apply: the Codex sandbox holds Access, and a pass needs only the process and Report parts. Maestro stores the version, time, and all three parts; an older version's pass does not count for an update. The hook result names the control folder or the Run folder where it found the record. The Lane can write a record in the Run folder, so that record checks for a broken Harness and does not prove the Lane is safe. The Canary uses only its Wall limit. Canary Dispatches have `canary: true` in the Ledger and do not count in Route statistics. A Canary runs only when you request it.

`maestro adapter` prints the steps for each harness and writes nothing. `maestro adapter install` does the steps. Maestro touches nothing else in the configuration of a harness. To connect one harness only, give its name: `maestro adapter install claude`.

After a rebuild, the Reminder in Claude Code, Codex, and Antigravity reports a stale Adapter when the installed skill or Grok rules differ from the text in the binary. It names each stale Harness on one line, for example `Adapter stale (claude, grok): run maestro adapter install`. Run `maestro adapter install` to update the files and clear the line. Grok has no Reminder event; use `maestro doctor` there.

`maestro init` writes the Project file `.agents/maestro.toml`. That file puts the project in hook scope. It detects Go, Node (npm, pnpm, or yarn), PHP, Python, Rust, and Docker from files at the project root, and adds active cache `dirs` and command `allow` rules for each match. It writes `checks` as commented suggestions for you to review. If the Project file exists, it shows the proposed additions. Use `maestro init --write` to merge them; it keeps existing values and comments, skips values that are present in comments, and adds nothing on a second call. See [Project file configuration](docs/configuration.md#agentsmaestrotoml) for the detection rules. `maestro doctor` warns when the Project file has no active checks, or a Docker check needs `~/.docker` in `dirs`.

An install from before this version reports `partial` in `maestro doctor` until `maestro adapter install` runs again.

### Connect Claude Code

```sh
maestro adapter install claude
```

It writes two paths:

- `~/.claude/skills/maestro` is a link to the shared skill file in `~/.maestro/skills/maestro`.
- `~/.claude/settings.json` gets `PreToolUse`, `UserPromptSubmit`, and `Stop` hook entries for `maestro hook`; Maestro adds or replaces only its own entries and keeps all other settings.

Manual step: start a new Claude Code session. It loads the hook.

Every Claude Lane ignores the user, project, and local settings files. `CLAUDE.md` still loads. Maestro supplies the Lane hook and sandbox settings at launch. A Claude edit Lane runs shell commands in the Claude sandbox: writes stay in the project, the Run folder, the project's `dirs`, and the per-user temp directory. Shell commands have no outbound network, so a check that must download something fails inside a Claude Lane. Caches already on disk are readable; a check that writes a cache needs that cache in `dirs`. A shell command that names a network host is denied without a prompt in every Claude Lane (`sandbox.network.strictAllowlist`, Claude Code 2.1.219 or later); a check that needs a module download must find it in the caches first. `claude_auto = true` makes Claude edit Lanes `best-effort`.

With `claude_auto = true`, Claude's own classifier decides whether to allow a command outside the allow list.

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
- `~/.codex/hooks.json` gets `PreToolUse`, `UserPromptSubmit`, and `Stop` hook entries for `maestro hook codex`; Maestro adds or replaces only its own entries and keeps all other hooks.

Manual step: start Codex, open `/hooks`, and trust the Maestro `PreToolUse`, `UserPromptSubmit`, and `Stop` hooks. This review is a security boundary. Only you cross it. An agent must not trust the hooks for you.

Verify: `maestro doctor` shows `adapter codex` as `ok`.

Probe the hook. A sub-agent that can write code must get a `deny`:

```sh
echo '{"tool_name":"Agent","tool_input":{"subagent_type":"general-purpose"}}' | maestro hook
```

The probe denies only where the hook is in scope: with the default `[hook] scope = "projects"` that is inside a project that has `.agents/maestro.toml`, and `scope = "all"` in `~/.maestro/config.toml` makes it global.

### Connect Grok

Grok takes read and edit work. Headless Grok runs with full tool access. A Grok Lane runs in always-approve mode and never prompts. The Maestro hook is the policy: it runs before every tool call and denies what the Lane may not do, and the workspace sandbox backs it at the kernel level:

- A read Lane runs the built-in read-only commands and writes only its Report in the Run folder
- An edit Lane also runs the checks and the project `allow` list, and writes inside the project, the Run folder, and the extra `dirs`; the workspace sandbox also permits writes to temp directories and `~/.grok`
- Grok never relays: a command outside the allow list is denied; add it to `checks` or the project `allow` list

The hook's [read-only command list](docs/configuration.md#read-only-commands) is the baseline for every project. It includes text and file commands, version control reads, and version and listing forms for Go, Node, PHP, Python, and Docker. Tests, builds, formatters, and installs need project `checks` or `allow` entries.

The hook judges a command as written. It denies shell expansion with `$`, a backquote, or an unquoted `(`, `{`, or `}` in a read-only command. A glob is allowed after a fixed prefix, as in `grep -r --include=*.go x .` and `ls ./*_test.go`; a glob that can match a file name that starts with `-`, such as a bare `*`, is denied. It removes leading `env` words and assignments to a [short list of harmless environment names](docs/configuration.md#agentsmaestrotoml) before it matches a command. Other assignments are denied in a read Lane. An edit Lane needs a check or `allow` entry for the full command, or uses Relay where its Harness supports Relay. `composer show` requires `--no-plugins`; npm, pnpm, Yarn, cargo, and rustc forms that can select a project toolchain are outside the baseline list. The sandbox is the boundary that holds a shell; the hook is the second line that holds paths, tool names, and the command list.

Grok Lanes run under `--sandbox workspace` when every writable directory is inside the project. For edit Lanes, this includes `dirs`. If a directory lies outside the project, as this repository's Go caches do, the Grok edit Lane runs without the sandbox and its Route is `best-effort`. A read Lane does not use the extra edit `dirs`.

The hook first tries to record its judgment in the control folder under `~/.maestro`. The Grok workspace sandbox permits this record only inside the project, so the hook uses the Task's Run folder when the control write fails. The Canary reads both places and names the place in its hook result.

Maestro cannot detect three limits. Grok warns and continues without the kernel sandbox when the OS refuses a built-in profile. Maestro finds a Grok Lane through the `transcriptPath` key in the hook payload; a Grok release that drops the key leaves the Lane under the Orchestrator rule. A check that runs Lane-written code under the Grok workspace sandbox can write `~/.grok`, Grok's own home. Grok's `hashline` file toolset is not supported in a Lane: its write tools are denied as unknown.

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

- A read Lane runs read-only commands and returns its Report in its final message; Maestro saves the `response` text as `result.md` in the Run folder
- An edit Lane also runs commands on the checks and `allow` lists, and writes inside the project, the Run folder, and the extra `dirs`
- An edit Lane relays other commands; a read Lane denies them

Antigravity has no kernel sandbox, so the hook is the only boundary of an Antigravity Lane. It judges a command as written: a read-only command with `$`, a backquote, or an unquoted `(`, `{`, or `}` is denied; a glob needs a fixed prefix, as in `ls ./*_test.go`.

The hook entry changed, so an existing install must run `maestro adapter install agy` again.

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

The probe denies only where the hook is in scope: with the default `[hook] scope = "projects"` that is inside a project that has `.agents/maestro.toml`, and `scope = "all"` in `~/.maestro/config.toml` makes it global.

## Usage

You install once and plan with your agent. The agent hands finished plans to Maestro on its own. You answer only when Maestro needs you.

1. Install Maestro and connect the harnesses once. See [Installation](#installation).
2. Plan with your agent as you do now: a grilling session, a spec, a plan file. The Maestro skill tells the agent what a finished plan looks like.
3. When you approve the plan, the agent writes the Plan file and runs `maestro run` itself. Nobody asks you to type anything.
4. Inside Solo, each task is a todo and each result is a comment, and a finished run wakes the agent. Outside Solo, the agent reports from `maestro wait`.
5. Maestro needs you when a verb exits with code 2: an open Relay, a held breakdown, a paused Run, a blocked or failed Task, or a single-opinion Council. The agent reads the item or report and either answers or asks you one question.

Inside hook scope, the Orchestrator gets a Reminder on every prompt in Claude Code and Codex, and before every model call in Antigravity. Its fixed text comes first, then the stale Adapter line when needed. Each live item of the project follows on its own line: an open Relay with its Task, tool, input, and answer command; a paused Run with its reason and resume command; a blocked or failed Task of a live Run; or a running Lane with its Task, Family, effort, and elapsed time. Relay input is limited to 300 characters. The hook reads local Run and control folders, with no Solo or network call. It uses the same project root as the CLI, including from a nested git folder. It leaves out a live Run or running Dispatch whose process is gone; `maestro status` marks it `stale (process gone)`. With no live items or stale Adapter, it sends only the fixed text.

In Grok, the rules file loads at the start of each session and states the scope condition in words, so there the scope is best-effort. A Grok Orchestrator does not get the live items through the Reminder; use `maestro status` to read them.

For example, you end a grilling session with a plan for a new probe feature. The agent writes `.agents/plans/probe-feature.md` and starts the run. Some minutes later it tells you that the three tasks are done and the checks pass. If a lane needs an answer, the agent asks you one question and passes your answer on.

### Solo

Every Lane runs as a headless child process. Inside Solo, Maestro creates todos, adds Report and Merge comments, and sends wake-ups.

Maestro wakes the Orchestrator once for each item that needs it:

- A Relay opens: `maestro answer <task> allow|deny --relay <id>`.
- A Run pauses: `maestro resume <run> --task <n> (--lane <family[:effort]> | --brief <file> | --skip)`.
- A single Dispatch ends failed, blocked, timed out, or with an unshaped Report: `maestro wait <task>`.
- A Run ends: `maestro wait <run>`.

Each wake-up gives the same next command as `maestro wait`. An item resolved during the wake-up delay sends no wake-up. A Task of a Run uses the Run's pause or end wake-up. A single Dispatch that ends verified, answered, or unverified with a shaped Report sends no wake-up.

With a Solo link, Maestro passes Solo's MCP server to a Claude Lane through `--mcp-config`. Read and edit Lanes get the same read-only set, which also permits a todo comment:

- `mcp__solo__list_processes`
- `mcp__solo__get_process_status`
- `mcp__solo__get_process_output`
- `mcp__solo__search_output`
- `mcp__solo__get_process_ports`
- `mcp__solo__todo_comment_create`

The Brief gives the Solo project id. Each Solo tool call needs this id in `project_id`. A Claude Lane can read dev server or test watcher output and add a todo comment. Other Solo tools are denied without a Relay. A Lane has no Solo identity, so Solo could treat its action as an action of the Orchestrator. All `SOLO_` variables stay removed. With no Solo link, a Claude Lane gets no MCP config. Other Harnesses and the Canary get no Solo MCP tools.

### Verbs

These are the commands the agent runs. A person can run them by hand too.

`maestro run` uses a separate Worktree for each edit Task when Worktrees are on, the repository has a commit, HEAD is on a branch, and no Task's `touch` names a dependency folder. Uncommitted changes in your tree do not stop it: the Tasks start from HEAD and the Run says so. A saved Run keeps its mode on resume. At landing, a user branch that gained commits is rebased onto; uncommitted changes block the landing only when their paths overlap the Run's, and `maestro resume <run> --finish` retries after you commit or stash them. The mode line says `worktrees: on` or gives the reason for tree mode. Tasks with files that overlap run in Plan order. Maestro owns the commits; Lanes never commit. At the end, the Plan's `commit: true` keeps the Task commits on your branch, and `commit: false` leaves the changes uncommitted. A merge conflict gets one attempt in the Worktree, then blocks with its path. After a hand fix, use `maestro resume <run> --task <n> --merge`; if landing the Run branch fails, use `maestro resume <run> --finish` after you correct the reported condition.

Worktrees link ignored dependency folders and exclude the links from git status. Only a `touch` pattern that names a dependency folder or a path inside it refuses the link; no `touch` permits it. A refused folder requires tree mode for the Run, with the reason `task <n> touches <folder>`. Finish retries continue from the step that is left. Cleanup deletes branches only when the Run branch is an ancestor of your branch or the Run state is `finished`; otherwise, it keeps all branches and reports their names. It also keeps a Task branch with work that did not land on your branch. A blocked Task or `finish_pending` does not stop the cleanup of an expired Run; Maestro keeps its unlanded branches. See [Worktrees](docs/configuration.md#worktrees).

Each read Task of a Worktree-mode Run uses a detached Worktree at the Run branch head, with dependency links. A review that waits for edit Tasks sees their merged work. Maestro removes the read Worktree when the Task ends. Grok Lanes keep the workspace sandbox in a Worktree and return the Report by message; extra writable directories outside the Worktree still make the launch `best-effort`.

A standalone edit Dispatch uses a Worktree when Worktrees are on, the repository has a commit, HEAD is on a branch, and the Brief's `touch` names no refused dependency folder; otherwise `maestro dispatch` prints `worktrees: off (<reason>)` and uses the project tree. After the gate, Maestro commits and lands the Task with the Brief's `commit` setting. A blocked landing keeps the Worktree and Run branch; `maestro wait <task>` returns exit 2 with `maestro dispatch --finish <task>`, which retries the landing without a new Lane. `maestro clean` removes old standalone Worktrees and keeps branches with commits that did not land.

Before a Worktree-mode Run or a standalone edit Dispatch lands, Maestro runs the project's `checks` once on the merged Run branch, after any landing rebase. A failed landing check keeps the Run branch and every Task commit: for a Run, add a fix Task with `maestro add <brief>` and then `maestro resume <run> --finish`; for a Dispatch, fix the Run branch and then `maestro dispatch --finish <task>`.

Runs and standalone edit Dispatches use tree mode when a check starts with `ddev`, `docker`, `docker-compose`, `lando`, `sail`, or a path that ends in `/sail`, after environment words, because such a check tests the project folder and not the Worktree. Worktrees link existing ignored `.env` and `.env.local` files; add other paths with `worktree_links` in `.agents/maestro.toml`, and a Task whose `touch` names a link requires tree mode. `maestro init` proposes links for Craft and Laravel, and `maestro doctor` shows the mode reason, the links, and ignored environment files that are not linked.

Before a Run with two or more edit Tasks starts, Maestro runs a Plan check against the project tree: one quick read Lane checks paths, symbols, scope, and order; `maestro wait <run>` waits for it, findings hold the Run for a corrected Plan and `maestro approve <run>`, a failed check never blocks, and `--no-plan-check` or `[run] plan_check = false` skips it.

For a Bad result, only Cause `capability` can re-Route. Causes `brief`, `environment`, and `none` block for the Orchestrator. `maestro wait <task>` follows only that Task in its Run and gives its Cause, Blocked items, and resume command. After an answer, it waits for that Task and does not return the old result. Approval reuses held Task ids and removes folders for Tasks that the Plan no longer has. If a commit fails after verification, the Dispatch record and Ledger say `decision: blocked` with the error; the Run report names the Worktree for blocked and failed Tasks.

Jev's Judgment level sets the Task's Tier. The Brief's `level` is the Orchestrator's estimate. `maestro dispatch --level` and `maestro run --level` replace that estimate when set. Jev sees it as context; it does not raise Jev's score or limit its pick. The rules table uses the estimate when Jev is absent or below the confidence floor. Set `floor:` and `ceiling:` in a Brief, or `maestro run --floor` and `--ceiling`, for hard Tier bounds. An edit never goes Light. The Reason line names both levels when Jev's score is below the estimate, for example `jev some, orchestrator estimated expert`.

The Orchestrator writes the `level` it believes and does not raise it or name a Lane to reach a stronger Lane, because Maestro uses a Frontier Lane when the work calls for it.
The `touch:` list names the paths the Task may change. Test files are always in scope; list all other required paths, including other fixtures, docs, and `CHANGELOG.md` when the Done asks for an entry. A Lane that must change another file outside `touch` stops blocked, and Maestro sends the Task back to the Orchestrator with Cause `brief`. A test file a Lane changed outside its own `touch` is recorded as `tests outside touch` and shown to the gate and the end-of-Run Council; a deleted test file outside `touch` is a violation.

Write a brief file (`maestro help brief` prints the format), then dispatch it:

```sh
maestro dispatch .agents/briefs/add-probe.md        # returns at once with a task id
maestro wait 7f3a                                   # prints the status, changes, checks, and report
maestro dispatch .agents/briefs/add-probe.md --wait # or block
```

Several ordered Tasks go in a Plan file (`maestro help plan`). Text after the front matter or optional `# title` and before the first `## <task title>` is the Plan's introduction. Maestro adds it to the start of every Task's Context under `Shared context of the Plan:`, before the Task's own `### Context` text. An empty introduction adds nothing.

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

`maestro wait <task|run>` returns with exit code `2` as soon as a Relay opens, a Run pauses, or a Task of a Run is blocked or failed and waits for the Orchestrator. It prints the item, the reason, and the next `maestro answer` or `maestro resume` command. Relay input in the reason is limited to 300 characters. Answer, then use `maestro wait` with the same id to continue. Answered or resolved items do not stop it again. At the end, `wait` prints the report and returns the outcome's exit code. For an early return, `--json` prints one object with `kind`, `run` or `task`, and `next_command`. A failed Task has the kind `task_failed`. A paused Run also gives the path to `report.md`, in text and in the JSON field `report_path`.

Exit codes: `0` ok, `1` failed, `2` needs the Orchestrator (an open Relay, a held breakdown, a paused Run, a blocked or failed Task, or a single-opinion Council). `--json` prints one object per verb.

Claude and Antigravity edit Lanes relay commands outside the allow list. Read Lanes, Grok Lanes, and Codex Lanes do not relay. An open Relay pauses the Idle limit and the Wall limit; an unanswered Relay is denied after 10 minutes and the Lane continues. Antigravity edit Lanes use Maestro's limits with no vendor print timeout.

Checks use the same Idle limit as the Lane and share a separate Wall limit budget, counted from the first check. A silent check or an expired budget fails with Failure class Stall. Before the first check that starts with `docker`, Maestro runs `docker info` once per Task; a failure or no answer within 10 seconds fails the check with `Docker does not respond` and Failure class Bad result. These failures skip the remaining checks with the same reason. A check that fails on its own exit code does not stop the next check. The Ledger and `maestro wait` show the reason and Failure class. See [configuration](docs/configuration.md#failure) for the limits.

Every Bad result has a Cause: `brief`, `environment`, `capability`, or `none`. A touch violation is `brief`; otherwise Jev classifies the Cause with the gate, or alone when the Report is blocked or the checks fail. In a Run, Lane error, Transient error, Limit hit, Stall, and Bad result with Cause `capability` re-Route one Tier up, within the Dispatch cap and Tier ceiling. Bad result with Cause `brief` or `environment` blocks the Task for the Orchestrator. A blocked Report stays blocked. If Jev is absent, a touch violation or blocked Report is `brief`, a partial Report with Blocked items or a failed Docker pre-flight is `environment`, and other failures are `capability`. A partial Report with Blocked items keeps its Bad result and Cause after a check Stall. The same error twice still blocks the Task. The Ledger records the Cause, its source, and the decision. The Record and Ledger keep zero numbers and use `scored` to identify scores. A Gate with only a Cause has `scored: false`; `maestro wait` and `maestro status` show `cause <value> (<source>)` for it. A scored Gate has `scored: true` and shows `satisfies <n> verdict <v> risk <r>`. The Run report, `maestro wait`, the Reminder, and the Solo wake-up show the Cause and the Lane's Blocked items.

When checks pass and the gate returns `pass` with risk at most `medium` but `satisfies` is under the close threshold, Maestro gives the same Lane one nudge for Done items scored under `0.5`. For this rule, the checks pass when no checks are set. The nudge Brief tells the Lane to keep the previous work and complete the listed Done items. With no such items, the Task ends `unverified` at once. Each Task gets at most one nudge, with one extra attempt in the Dispatch and Run caps. The result follows the normal checks and gate rules. The Ledger marks that the nudge ran, and `maestro wait` prints the last attempt's Report. See [the `satisfies` rule](docs/configuration.md#routerthresholdsjev).

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

Worktrees are on by default. Set `[run] worktrees = false` in `~/.maestro/config.toml` for every project, or `worktrees = false` in `.agents/maestro.toml` for one project. A project cannot turn Worktrees on when the global key is off. Set `[commit] message` globally or `commit_message` in a project to change the commit message template. See [Worktrees](docs/configuration.md#worktrees) for placeholders and Lane identities.

`.agents/maestro.toml` in a project changes things for that repository only; `forbid`, `checks`, `router`, `redact`, `councils`, and `worktrees` tighten, and `allow`, `dirs`, and `claude_auto` widen:

```toml
checks   = ["go build ./...", "go vet ./...", "go test ./..."]   # run after every edit
forbid   = ["grok"]                                              # families this project never uses
dirs     = ["~/.cache/go-build"]                                 # extra writable directories for edits: your Go build cache, `go env GOCACHE`
router   = "rules"                                               # keep this project's text off the network
redact   = ["ACME"]                                              # strings replaced before any Jev call
councils = "always"                                              # an end-of-run council on every run
```

Generated state lives in `~/.maestro/`: `ledger.jsonl` (append only), `budget.json`, `cache/discovery.json`, and the live-run index. Maestro rejects a Ledger row with no `kind`. It drops Accounts absent from the Roster from the Budget before the next save. Tests must set `MAESTRO_HOME` to a temporary folder; a test that calls `config.Home()` without it stops with a panic.

Per-project Run folders live under `.agents/maestro/runs/` and Council folders under `.agents/maestro/councils/`. `maestro clean --older-than 30d` removes old Run and Council folders in the current project. Across all project keys in `~/.maestro/control`, it also removes control folders whose Dispatch or Run ended before the threshold, or whose record is gone. It keeps live Run controls. It removes old folders under `~/.maestro/cache`, but keeps the latest Canary files per Harness at the path shown by `maestro doctor`. It never touches the Ledger or `budget.json`. Maestro writes a `.gitignore` into each Run and Council folder it creates, so nothing under them reaches git.

Keep-awake and the end-of-run notice are macOS only; on Linux they do nothing.

## Support

Found a bug or need help? Open an [issue](https://github.com/jalendport/maestro/issues).

<hr>

<p align="center">Made by <a href="https://jalendport.com">Jalen Davenport</a></p>
