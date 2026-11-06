# Reference

This file tells how Maestro behaves. Each key, its type, and its default are in the [configuration reference](configuration.md).

## Lanes and Access

### Access

Access has two values, `read` and `edit`; there is no third value. Claude and Grok use "permission mode" for their own flag, and Maestro says Access for the Brief field. Maestro passes no bypass flag to a Claude, Codex, or Antigravity Lane.

### Enforcement

Enforcement is a measured fact for each Harness and Access value. It is `enforced` when a sandbox, a tool allowlist, or the Maestro hook holds the Access with no help from the model. It is `best-effort` when only an instruction, a weak flag, or a model decision holds it. A `read` Task goes only to `enforced` Lanes. The Route records the launch's Enforcement.

Two settings make an edit Lane `best-effort`: `claude_auto` for Claude edit Lanes (see [Claude Code](#claude-code)), and writable `dirs` outside the project for Grok edit Lanes (see [Grok](#grok)).

### The hook

The hook is the set of `maestro hook` entries that each Adapter installs in a Harness: a `PreToolUse` entry, and for Claude Code, Codex, and Antigravity a second entry for the Reminder event (Antigravity's runs `maestro hook agy`). For Antigravity, Grok, and Claude Code the `PreToolUse` entry also holds each Lane's permissions, because those harnesses' own permission rules cannot hold the Access on their own. For Claude Code the hook can only deny, and the Claude sandbox holds the rest.

The hook applies only inside its [`scope`](configuration.md#hook), for both the deny rule and the Reminder. The hook probe in the README denies only where the hook is in scope. In Grok, the rules file loads at the start of each session, so there the scope is best-effort.

Inside hook scope, the Orchestrator gets a Reminder on every prompt in Claude Code and Codex, and before every model call in Antigravity. Its fixed text comes first, then the stale Adapter line when needed. Each live item of the project follows on its own line: an open Relay with its Task, tool, input, and answer command; a paused Run with its reason and resume command; a blocked or failed Task of a live Run; or a running Lane with its Task, Family, effort, and elapsed time.

The hook reads local Run and control folders for the Reminder, with no Solo or network call. It uses the same project root as the CLI, including from a nested git folder. It leaves out a live Run or running Dispatch whose process is gone; `maestro status` marks it `stale (process gone)`. With no live items or stale Adapter, it sends only the fixed text. A Grok Orchestrator does not get the live items through the Reminder; use `maestro status` to read them.

For a Reminder in scope, the hook compares the installed skill and Grok rules with the text in the binary. If either differs, it adds `Adapter stale (<harnesses>): run maestro adapter install` with each stale Harness's name. Claude Code, Codex, and Antigravity all receive this line, including when only Grok's rules are stale. The check reads local files and makes no network call.

Run `maestro adapter install` to update the files and clear the line. Grok has no Reminder event; use `maestro doctor` there. Missing or unreadable files remain checks for `maestro doctor`.

### Read-only command judgment

The hook judges a command as written. Its read-only command list is the baseline for every project. It serves Go, Node, PHP, Python, Rust, and Docker projects. These commands need no project `allow` entry. A command's write or execution forms are denied even when a check or `allow` entry names the command. No configuration key changes this list.

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

This list does not grant shell loops, command substitution, redirects, or a pipe to a write command. The existing shell and path checks still apply. The hook denies `$`, backquotes, and unquoted `(`, `{`, or `}` in read-only commands, including zsh glob qualifiers that can run code and brace lists that can add flags. For a brace, the deny reason says `quote the brace, for example '@{u}'`. Quote git references as in `git log '@{u}'..` and `git show 'stash@{0}'`.

A glob word that starts with an unquoted `*`, `?`, or `[` is denied: `glob can match a file name that starts with -; write ./ before it`. A glob after a fixed prefix is permitted, such as `ls ./*_test.go` and `cat src/**/*.php`. In a word that starts with `-`, a glob is permitted only after an `=` in that word, such as `grep -r --include=*.go x .`. The hook denies `sort -*`, `rg --pr* x`, and `ls -?` with the same reason as a bare glob.

For matching against `allow`, `checks`, and the read-only list, the hook removes leading `env` words, including `env -i` and `env -u NAME`. It removes a leading `NAME=value` assignment only for these harmless names: `CI`, `NO_COLOR`, `FORCE_COLOR`, `TERM`, `COLUMNS`, `LINES`, `LANG`, `LC_ALL`, `TZ`, `CGO_ENABLED`, `GOOS`, `GOARCH`, and `MAESTRO_HOME`. These can be combined. Thus `env -u MAESTRO_HOME go test ./...` matches `Bash(go test *)`, and `CGO_ENABLED=0 go build ./...` matches `Bash(go build *)`.

If any other assignment is present, the hook keeps the full command for matching. A read Lane is denied. An edit Lane uses Relay where its Harness supports Relay, or is denied, unless a check or `allow` entry permits that full command. The reason names the variable. This includes `GIT_EXTERNAL_DIFF`, `GIT_CONFIG_COUNT`, and `GOFLAGS`, whose values can make a read command run code.

The command runs as written. Write-flag and shell checks still apply to matched read-only commands. The hook does not remove other prefixes, such as `sudo`, `nohup`, or `time`.

### Relay

A Relay passes a Lane's permission prompt to the Orchestrator, which types the answer back. The Orchestrator may ask the user. To answer one, see [Verbs: Relay](#relay-1).

Claude and Antigravity edit Lanes relay commands outside the allow list. Read Lanes, Grok Lanes, and Codex Lanes do not relay. An open Relay pauses the Idle limit and the Wall limit; an unanswered Relay is denied after 10 minutes and the Lane continues. Antigravity edit Lanes use Maestro's limits with no vendor print timeout.

## Adapters

`maestro adapter` prints the steps for each Harness and writes nothing. `maestro adapter install` does the steps. Maestro touches nothing else in the configuration of a Harness. To connect one Harness only, give its name: `maestro adapter install claude`. The names are `claude`, `codex`, `grok`, and `agy`. For the stale Adapter line in the Reminder, see [the hook](#the-hook).

An Adapter is one shared skill file, one hook file, and the Carrier of the Reminder. Maestro itself works without any Adapter. Maestro and its docs say Lane, not "tool", because Claude Code and MCP already use "tool" for functions such as Read, Bash, or `spawn_agent`.

### Claude Code

Every Claude Lane ignores the user, project, and local settings files. `CLAUDE.md` still loads. Maestro supplies the Lane hook and sandbox settings at launch. A Claude edit Lane runs shell commands in the Claude sandbox: writes stay in the project, the Run folder, the project's `dirs`, and the per-user temp directory.

Shell commands have no outbound network, so a check that must download something fails inside a Claude Lane. Caches already on disk are readable; a check that writes a cache needs that cache in `dirs`. A shell command that names a network host is denied without a prompt in every Claude Lane (`sandbox.network.strictAllowlist`, Claude Code 2.1.219 or later); a check that needs a module download must find it in the caches first.

`claude_auto = true` makes Claude edit Lanes `best-effort`: Claude's own classifier decides whether to allow a command outside the allow list. For the Solo tools of a Claude Lane, see [Solo](#solo).

### Codex

The Codex sandbox holds the Access of a Codex Lane. For which Lanes relay, see [Relay](#relay).

### Grok

Grok takes read and edit work. Headless Grok runs with full tool access. A Grok Lane runs in always-approve mode and never prompts, because Grok has no mode that is prompt-free and deny-by-default. The Maestro hook that `maestro adapter install grok` installs is the policy: it runs before every tool call and denies what the Lane may not do. The workspace sandbox backs it at the kernel level, and Grok edit Lanes need that sandbox for `enforced` Access:

- A read Lane runs the built-in read-only commands and writes only its Report in the Run folder
- An edit Lane also runs the checks and the project `allow` list, and writes inside the project, the Run folder, and the extra `dirs`; the workspace sandbox also permits writes to temp directories and `~/.grok`
- A command outside the allow list is denied; add it to `checks` or the project `allow` list

The sandbox is the boundary that holds a shell; the hook is the second line that holds paths, tool names, and the command list. The hook judges each command by the [read-only command judgment](#read-only-command-judgment).

Grok Lanes run under `--sandbox workspace` when every writable directory is inside the project. For edit Lanes, this includes `dirs`; a read Lane does not use the extra edit `dirs`. If a directory lies outside the project, as this repository's Go caches do, the Grok edit Lane runs without the sandbox and its Route is `best-effort`.

A Grok Lane in a Worktree keeps `--sandbox workspace` when no extra writable directory is outside its root. It reads the Brief by absolute path and returns the Report in its final message, so the Run folder needs no write access. Extra writable directories outside the Worktree still make the launch `best-effort`.

The hook first tries to record its judgment in the control folder under `~/.maestro`. The Grok workspace sandbox permits this record only inside the project, so the hook uses the Task's Run folder when the control write fails. The Canary reads both places and names the place in its hook result.

Maestro cannot detect three limits. Grok warns and continues without the kernel sandbox when the OS refuses a built-in profile. Maestro finds a Grok Lane through the `transcriptPath` key in the hook payload; a Grok release that drops the key leaves the Lane under the Orchestrator rule. A check that runs Lane-written code under the Grok workspace sandbox can write `~/.grok`, Grok's own home.

Grok's `hashline` file toolset is not supported in a Lane: its write tools are denied as unknown.

### Antigravity

Antigravity takes read and edit work, like the other Harnesses. The Maestro hook entry that `maestro adapter install agy` installs carries each Lane's permissions:

- A read Lane runs read-only commands and returns its Report in its final message; Maestro saves the `response` text as `result.md` in the Run folder
- An edit Lane also runs commands on the checks and `allow` lists, and writes inside the project, the Run folder, and the extra `dirs`
- An edit Lane relays other commands; a read Lane denies them

Antigravity has no kernel sandbox, so the hook is the only boundary of an Antigravity Lane. The hook judges each command by the [read-only command judgment](#read-only-command-judgment).

## Routing and the gate

### Router

The Router makes the Routes. The backend is Jev or the rules table, and Maestro uses the rules table when Jev is absent or sure of nothing. The rules table observes every Route and decides only when Jev is absent. For a Task's Tier and category, it tries the Families of the chain in order. Without a Jev key the rules table routes every Task, and edit Tasks end `unverified`; the Orchestrator reads the diff.

### Jev

Jev's Judgment level sets the Task's Tier. The Brief's `level` is the Orchestrator's estimate. `maestro dispatch --level` and `maestro run --level` replace that estimate when set. The estimate goes to Jev as context and never raises Jev's score or limits its pick. The rules table uses the estimate when Jev is absent or below the confidence floor.

Only a Brief's `floor:` and `ceiling:`, or `maestro run --floor` and `--ceiling`, set hard Tier bounds. An edit never goes Light. The Orchestrator writes the `level` it believes and does not raise it or name a Lane to reach a stronger Lane, because Maestro uses a Frontier Lane when the work calls for it.

### The gate

The gate asks one `noul` question for each Done item, keyed as `done_1`, `done_2`, and so on, with the `cause`, `satisfies`, `verdict`, and `risk` questions. It records the item scores under `verification.gate.done_items`. For the Cause, see [Causes](#causes).

The Record and Ledger store `scored: true` when Jev answers the full gate questions. They keep `satisfies`, `verdict_confidence`, and `risk_confidence` even when the value is zero. A Gate with only a Cause has `scored: false`; its zero numbers are not scores. `maestro wait` and `maestro status` use `scored` to show either `satisfies <n> verdict <v> risk <r>` or `cause <value> (<source>)`. For an old Gate record without `scored`, a nonempty `verdict` means the Gate has a score.

### `satisfies`

The gate closes a Task as satisfying its Brief when its `satisfies` score is at or above the [`satisfies`](configuration.md#routerthresholdsjev) threshold, the close threshold. A `pass` verdict under the close threshold can get one [nudge](#nudge) before the Task ends `unverified`.

### Nudge

If the checks pass, the verdict is `pass`, the risk is `none`, `low`, or `medium`, and `satisfies` is under the close threshold, Maestro gives the same Lane one nudge. For this rule, the checks pass when no checks are set. The retry Brief tells the Lane to keep the previous work, which passed the checks and the gate's verdict, and lists each Done item with a score under `0.5`. These scores are not confirmed defects.

An absent item score does not count as a missing item. With no missing items, the Task ends `unverified` at once. Each Task gets at most one nudge, including across re-Routes. There is no second nudge.

The nudge adds one attempt to the Dispatch cap and the Run's Dispatch cap, so the existing retry after a Lane error or Transient error still fits. The nudge attempt uses the normal checks and gate: a passing result at the close threshold is verified; another result follows the normal outcome rules.

The Ledger marks the nudge attempt and later attempts with `nudged: true`. Earlier Reports stay under `attempts/<n>/`; `maestro wait` prints the last attempt's Report.

### Reason line

When Jev's score is below the Orchestrator's estimate, the Reason line says, for example, `jev some, orchestrator estimated expert`. The Route record keeps both values. A re-Route names the Cause in its Reason line.

## Runs

### Plan file and Brief file format

A Brief file has a front matter of short fields and fixed headings for the Goal and the Done. A Plan file holds every Task of a Run, each as a section with the same fields as a Brief file plus `blocked_by`. `maestro help brief` and `maestro help plan` print the formats.

### Plan introduction

In a Plan file, text after the front matter or optional `# title` and before the first `## <task title>` is the introduction. Maestro adds it to the start of every Task's Context under `Shared context of the Plan:`, before the Task's own `### Context` text. An empty introduction adds nothing.

### Approval holds

A Run holds for approval when it has more than four Tasks, a medium or fallback Route, a Judgment level two steps from the Router's, a Router question, or a Router Council signal. These rules have no configuration key. A held Run exits with `2`.

Fold any Council or Plan check findings into the Plan file, then use `maestro approve <run>`. Approval reads the file at the recorded path again, parses it, prints new provisional Routes, and starts the current Tasks under the same Run id. It reuses the held Task ids in Plan order and removes folders for Tasks that the Plan no longer has. A parse error leaves the Run held.

### Plan Council

The plan Council is a separate, deeper review for a big Plan. It starts when a held Plan has at least [`council_tasks`](configuration.md#run) Tasks, when `maestro run --council <plan>` requests it for a held Run, or when any Task has a Router question or Council signal. A hold alone does not start a Council. `--no-council` stops the plan Council in every case, including when `--council` is set. Approval starts no second plan Council.

When the plan Council and the [Plan check](#plan-check) both apply, both run. `--no-plan-check` does not disable the Council, and `--no-council` does not disable the Plan check.

### Plan check

The Plan check is one detached read Dispatch in `<run>/plan-check/`, with category `review`, Judgment level `some`, a Daily Tier ceiling, and a five-minute Wall limit. [`plan_check`](configuration.md#run) turns it on. Its Route ignores Council and approval triggers, including `wants_independent_view` and `needs_write`, and records `plan check: triggers ignored`. It checks these six questions against the project tree by reading and searching only:

1. Does a Task name a path in Files, touch, or text that does not exist, without saying it creates that path?
2. Does a Task name an existing function, type, flag, or config key that a search cannot find?
3. Does a Task remove or rename an exported symbol with a caller outside its touch?
4. Does a Task say it changes a path outside its touch, or do shared rules require a doc outside its touch to agree with the code?
5. Do two Tasks with no blocked_by between them change the same file in their text, without showing the overlap in Files?
6. Does a Task have a Done item that its touch or the shared rules make impossible?

While the Plan check runs, the Run is held with `plan check running`; `maestro wait <run>` waits for the result. When `plan check running` is the only hold, `maestro run` gives `maestro wait <run>` as the next command. No findings and no other approval trigger starts the Run without approval, and wait continues into the Run.

Each finding becomes an approval line with the prefix `plan check: task <n>:`. Findings lines can start with `- ` or `* `. Wait exits 2 with the findings and `maestro approve <run>`. Approval reads the corrected Plan and starts the Run without a second check.

A failed or timed-out check, or an unshaped Report, permits the Run to continue without findings; status shows `plan check: no result (<reason>)`.

During the check, `maestro cancel <run>` writes a cancel marker and reports `cancel requested`; the check supervisor stops the Lane and cancels the Run. Maestro gives a new check supervisor five seconds to start. If the check supervisor is gone, `maestro wait` and `maestro approve` record `plan check: no result (supervisor gone)` and permit approval. If a cancel marker exists when the check supervisor is gone, wait reports `cancel pending`; approval cancels the Run without reading or routing the Plan again.

## Worktrees and landing

Worktrees live under `~/.maestro/worktrees/<project-key>/<run>/<task>/`. The Run branch is `maestro/<run>/run`; each edit Task branch is `maestro/<run>/<task>`. A standalone Dispatch has its own [names](#standalone-dispatches).

### Mode choice

A Run or a standalone edit Dispatch uses Worktree mode when all of these are true:

- Worktrees are on; see [`worktrees`](configuration.md#run).
- The repository has a commit.
- HEAD is on a branch. A new Run with a detached HEAD uses tree mode with the reason `HEAD is not on a branch`.
- No Task's `touch` names a refused [link](#links).
- No check is [container-bound](#container-bound-checks).

Otherwise it uses tree mode. `maestro dispatch` prints `worktrees: off (<reason>)`. The mode line of `maestro run` says `worktrees: on` or gives the reason for tree mode.

A Run with one edit Task or a dirty tree also uses Worktree mode. Tasks start from the HEAD commit. For a dirty tree, `maestro run` prints `tree has uncommitted changes: Tasks start from <short hash>; your uncommitted work is not in them`. The main checkout is never changed except at landing.

Maestro chooses the mode when it creates the Run. For a held Run, `maestro approve` chooses the mode again from the current Plan, including new Tasks and changes to `touch`. The mode stays the same on resume. A saved Run keeps its user branch even when HEAD is detached.

Each live Run has its own Run branch and base. Worktree-mode Runs can share a project; `maestro run` prints `another Run is live: <id>`. If the new Run or any live Run of the project uses tree mode, `maestro run` and `maestro approve` refuse with `Run <id> is live in tree mode; wait for it or use maestro add`.

### Container-bound checks

A container-bound check has `ddev`, `docker`, `docker-compose`, `lando`, `sail`, or a path ending in `/sail` as its first word after environment words. Maestro removes leading assignments and `env` words, including `env -i` and `env -u NAME`, for this check. It checks the Project file's `checks` and each Task's own checks.

Such a check uses the project folder mounted in the container, so a separate checkout cannot supply the Task's files. The Run and standalone Dispatch use tree mode with `checks run in a container bound to the project folder (<first word>)`. `go test ./...` permits Worktree mode.

### Links

Maestro links existing ignored paths from the project into each Worktree: `node_modules`, `vendor`, `.venv`, `.bundle`, `storage/framework`, `.env`, and `.env.local`. [`worktree_links`](configuration.md#agentsmaestrotoml) adds project paths. A file or folder is linked only when it exists and git ignores it.

Before it makes each link, Maestro adds the path to the exclusion file that `git rev-parse --git-path info/exclude` returns for the Worktree. This keeps a link ignored when the project rule has a trailing slash, such as `node_modules/`.

A path stays unlinked when `touch` names it or a path inside it, such as `node_modules/**`. The Run or standalone Dispatch then uses tree mode with `task <n> touches <path>`. No `touch`, an empty list, or a general pattern such as `**` permits the link.

### Overlap order

In Worktree mode, Tasks with files that overlap wait in Plan order with `files overlap with <n>`; Routes and status show `after <n> (files overlap)`. Overlap uses the Brief's declared `Files` and `touch` patterns. The [scope guard](#scope-guard)'s test-file rule does not add overlap between Tasks.

In tree mode, an edit Task waits with `tree busy` while another edit Task runs.

### Commits

Lanes never commit. Maestro commits a verified edit Task after the gate, or an unverified edit Task with a Report and changed files. In Worktree mode, Maestro merges each Task into the Run branch. The Plan's `commit: true` keeps those commits when the Run lands on the user's branch; `commit: false` leaves the changes uncommitted. In tree mode, Maestro commits only a Task with `commit: true`.

Each commit keeps the user as its git author. The message comes from the [commit message template](configuration.md#commit).

If a commit fails after verification, the Dispatch record and Ledger keep `outcome.decision: blocked` and the error text. The Run report gives the Worktree path for both blocked and failed Tasks.

### Read Worktrees

Every read Task of a Worktree-mode Run starts in a detached Worktree at the current Run branch head. A read Task with `blocked_by` the edit Tasks sees their merged changes. It gets the dependency links and keeps read Access. On resume, Maestro reuses a read Worktree from this repository and resets it to the Run branch head. Maestro removes a read Worktree when the Task ends, including after failure or cancellation, whatever the saved mode. Read Tasks have no Task branch.

### Merge conflicts

A merge conflict gets one more attempt in the same Worktree. Maestro starts the rebase onto the Run branch, the Lane edits the conflicted files, and Maestro continues the rebase after the checks and the gate. A second conflict, a conflict marker that stays in a file, or a failed attempt blocks the Task with `merge conflict in <paths>` and keeps its Worktree.

To fix it by hand, rebase the Task branch onto the Run branch in that Worktree, resolve the conflicts, and complete the rebase; then `maestro resume <run> --task <n> --merge` retries the merge with no new Lane. After a successful rebase, Maestro removes the Task's Worktree and Task branch. A cleanup failure keeps the Task branch for a retry.

### Landing rebase

When the user's branch gained commits, Finish rebases only the Run branch's own commits onto the user's branch head before landing. A conflict pauses the Run with `landing conflict in <paths>; Run branch <name>`. Maestro aborts the rebase and keeps the Run branch. Resolve the conflict on the Run branch, complete the rebase onto the user's branch, return to the user's branch, then use `maestro resume <run> --finish`.

### Landing check

Before a Worktree-mode Run or standalone edit Dispatch lands, the landing check runs the Project file's `checks` on the merged Run branch head. It uses a detached Worktree at `<run dir>/landing`, with the dependency links. When Finish rebases the Run branch, the landing check tests the rebased head. Output resets the Idle limit. The checks share one Wall limit budget, with the longest Wall limit of the Run's completed edit Tasks, or the standalone Task's Wall limit.

The landing check is skipped when the project has no `checks`, or when exactly one merged edit Task passed those checks on the same base and its checked commit is still the Run branch head. A moved user branch requires the check again. `maestro status` and the Run report show `landing check: skipped (<reason>)`, `landing check: passed (<n> checks, <duration>)`, or `landing check: failed`. The result is in `run.json` as `landing_check` and in the Run's Ledger row.

A failed landing check pauses the Run with `landing check failed: <command>: <first failing line>` in the Run report, `maestro wait`, and the Solo wake-up. It keeps the Run branch and every Task commit. Use `maestro add <brief>` to add a fix Task that starts from the merged head, then `maestro resume <run> --finish` to run the fix and try the landing check again.

A standalone Dispatch ends `blocked` with the same failure text. Put its fix in the kept Worktree that `maestro wait` names; `maestro dispatch --finish <task>` commits and merges the fix, then runs the landing check again without a new Lane.

### Dirty-tree landing

A dirty tree permits landing when its dirty paths do not overlap the Run's changed paths. With `commit: true`, Finish uses `git merge --ff-only` and keeps every Task commit. With `commit: false`, Finish applies the Run's changes with `git merge --squash` and unstages only the Run's changed paths with `git reset -q -- <paths>`, so the user's own staged work stays staged.

Overlap pauses the Run with `landing blocked: uncommitted changes in <paths> overlap the Run; commit or stash them, then maestro resume <run> --finish`. Maestro never stashes, resets, or checks out the user's work to clear a landing block.

### Finish

If landing fails, `maestro resume <run> --finish` retries it. Finish skips a merge that already landed and continues cleanup. A retry continues from the step that is left, also when the Run's added files are already untracked after a crash. After landing, Finish removes the Run's Worktrees and branches by the [cleanup](#cleanup) rules.

When landing needs the user's branch and it is not checked out, the Run pauses with `user branch <name> is not checked out`; check out that branch, then use `maestro resume <run> --finish`.

### Cleanup

At the start of `maestro run`, `maestro dispatch`, and `maestro status`, and when `maestro wait` returns, Maestro checks this project's saved Worktree identities. It removes an identity only when all its branches and uncommitted files have landed on its saved user branch (or the checked-out branch if none was saved); file content committed on that branch after the identity's base also counts as landed. It keeps live, held, and paused Runs, Runs with a Task that needs an Orchestrator answer or `finish_pending`, running Dispatches, and Dispatches that wait for `maestro dispatch --finish`. The sweep never runs from a Lane's hook and never changes the main checkout. It prints `cleaned <n> landed Worktrees` on stderr when it removes identities; errors print warnings and do not fail the verb. Text status and `--json` list each kept non-live identity with its id, reason, and next command: `maestro dispatch --finish <task>`, `maestro resume <run> --finish`, or `maestro clean --older-than 30d`.

A cancelled Run removes its Worktrees. Removal deletes the Run and Task branches only when the Run branch is an ancestor of the user's branch or the Run state is `finished`. When only the Run branch is an ancestor, removal also keeps each Task branch that is not an ancestor of the user's branch, because that Task branch has work that did not land. Otherwise, it keeps every branch. Removal returns the names of the kept branches for the Run report.

A failed or paused Run keeps its Worktrees and branches until resume or cleanup. `maestro clean --older-than 30d` removes old Worktrees and applies the branch removal rule above to finished, cancelled, and paused Runs. It prints each kept branch and returns them as `kept_branches` in JSON. It keeps live Runs. A blocked Task or `finish_pending` does not prevent cleanup of an expired Run. For the other folders that `maestro clean` removes, see [Generated state](#generated-state).

### Standalone Dispatches

A standalone edit Dispatch chooses Worktree mode or tree mode by the [mode choice](#mode-choice) rules. In Worktree mode, its supervisor opens the Worktree before it starts the Lane. The Task starts from HEAD, and the main checkout stays unchanged until landing.

After the checks and gate, a standalone edit Dispatch commits its changed paths with the commit message template when the outcome is `verified`, or `unverified` with a Report and changed files. It merges the Task branch into its Run branch and lands with the Brief's `commit` setting: `true` keeps the commit on the user's branch; `false` leaves the changes uncommitted and unstaged.

Its identity is `d-<run>-<task>`, which includes both its Run id and Task id. Its Run branch is `maestro/d-<run>-<task>/run`, its Task branch is `maestro/d-<run>-<task>/<task>`, and its Worktree is at `~/.maestro/worktrees/<project-key>/d-<run>-<task>/<task>/`. Dispatches with the same Task id in different Runs use separate Worktrees and branches.

The landing rules above also apply to a standalone edit Dispatch. Maestro rebases onto a user branch that gained commits. Overlapping uncommitted changes or a landing conflict block the Dispatch with the paths and Run branch name, and `maestro wait <task>` returns exit 2 with `maestro dispatch --finish <task>`. Clear any overlap in the project tree. Put a hand fix in the kept Worktree that `maestro wait` names. A landing conflict can also be resolved on the Run branch before the retry.

The finish command commits uncommitted fixes with the commit message template, merges the fix commits into the Run branch, and retries landing without a new Lane. After a new fix merge, the retry runs the landing check and lands the fix before cleanup, also when an earlier landing succeeded and only cleanup failed.

A failed, blocked, or cancelled Dispatch keeps its Worktree and branches, and `maestro wait` prints the Worktree path. When the Dispatch is older than the cleanup threshold, `maestro clean` uses the same Dispatch identity to remove its Worktree. It deletes branches whose commits landed and prints `kept branch <name>` for each branch that it keeps because its commits did not land.

### `maestro add`

`maestro add <brief>` selects the project's live Run when there is one. When no live Run matches, it accepts a paused Run with `finish_pending` or a blocked Task. With more than one matching Run, use `maestro add <brief> --run <id>`. The named Run must be in the current project. A Task added to a paused Run stays queued until resume starts the supervisor. `maestro cancel <task>` finds the Task's Run among all live Runs of the project.

## Checks and failure

### Scope guard

The scope guard checks changed paths against the Brief's `touch` patterns. An empty `touch` sets no path limit. With a nonempty `touch`, test files can be added or edited, even when a negation excludes them. Test files match `**/*_test.go`, `**/*_test.*`, `**/*.test.*`, `**/*.spec.*`, `**/*Test.php`, `**/test/**`, `**/tests/**`, `**/__tests__/**`, `**/spec/**`, or `**/testdata/**`. Other paths use the last matching `touch` pattern; a leading `!` excludes a path.

A deleted test file outside the Brief's own `touch` is a touch violation. A path from an earlier attempt leaves the changed list and the touch guard when it returns to its earliest baseline; it stays while it still differs.

### Tests outside touch

Maestro records test files admitted only by the test rule in `verification.tests_outside_touch`. `maestro wait`, `maestro status`, and the Run report list them after the changed files. The Reminder names them for a blocked or failed Task. The end-of-Run Council question names each completed Task that changed these tests and lists the paths for review.

The gate gets their paths and diff under `tests_outside_touch`, with this note: “These test files are outside the Task's scope. A weakened or removed assertion that the Brief does not ask for is a Bad result.”

### Failure classes

A Failure class is one of Lane error, Transient error, Limit hit, Stall, or Bad result. Every Ledger row and failure message carries exactly one.

### Causes

A Cause tells why a Task went wrong. `capability` means the Lane's work is wrong or incomplete with no blocker. `brief` means the Brief excludes a required file, forbids required work, or contradicts itself. `environment` means a tool or service was denied, down, or missing. `none` means nothing went wrong; on a Bad result, it needs the Orchestrator's judgment.

A touch violation has Cause `brief` without a Jev call. Otherwise Jev answers the `cause` choice question with the gate's `satisfies`, `verdict`, and `risk` questions, or answers `cause` alone for a blocked or partial Report or failed checks. The state includes the Brief's title, Goal, and Done, the Report's Blocked section and Status, failed check output, and the diff when present. Each text field is limited to 60 KiB.

If `router = "rules"`, no key is available, or Jev fails, a rule supplies the Cause: a touch violation or blocked Report is `brief`; a partial Report with Blocked items or a failed Docker pre-flight is `environment`; other failures are `capability`. The Ledger stores `cause` and `cause_source` (`jev` or `rule`) under `verification.gate`, and `decision` under `outcome`.

A partial Report with Blocked items keeps its Bad result and Cause when a later check reaches its Idle limit or Wall limit. The check still records its Stall. Without Blocked items, the Stall replaces the Report's outcome. This rule applies only to a Stall. A failed Docker pre-flight still sets the outcome's error text to the check failure and keeps Cause `environment` under the rule.

### Re-Route

For a Task of a Run, Lane error, Transient error, Limit hit, and Stall re-Route one Tier up. A Bad result re-Routes only when its Cause is `capability`. The Causes `brief`, `environment`, and `none` block the Task for the Orchestrator, with the Cause and the Blocked items. A blocked Report stays blocked. The Dispatch cap ([`max_dispatches`](configuration.md#failure)), the Tier ceiling, and the same-error-twice rule still apply.

A Task blocked by its Cause reaches the Orchestrator through the Run report, `maestro wait`, the Reminder, and the Solo wake-up with the Cause and the Lane's Blocked items.

### Check limits

A check has no fixed time cap. When stdout and stderr stay silent for the [Idle limit](configuration.md#failure), Maestro kills the process group and fails the check with `no output for <idle limit>` and Failure class Stall. When the checks' Wall limit budget ends, Maestro kills the process group and fails the check with `wall limit` and Failure class Stall.

After an Idle limit, a Wall limit, or a failed Docker pre-flight, Maestro skips the remaining checks and records the same reason. A check that fails on its own exit code does not stop the next check. The Ledger and `maestro wait` show the reason and Failure class.

### Docker pre-flight

Before the first check whose command starts with `docker` (including `docker compose`), Maestro runs `docker info` once per Task. If it fails or does not answer within 10 seconds, the check fails with `Docker does not respond` and Failure class Bad result. The checks' Wall limit budget also applies to this pre-flight.

### Check rerun

When a check fails with a normal exit, Maestro runs that command once more if the remaining Wall limit budget can hold the first run's duration. Both runs use the same shared Wall limit budget. A pass on the second run records `ok: true` and `flaky: true`, keeps the first run's failed output, and prints `ok (flaky: passed on rerun)` in `maestro wait`. A second failure records the second result. A Stall, a process stopped by a signal, or a failed Docker pre-flight gets no rerun.

If the rerun has a Stall, Maestro keeps the first failure and its output with Failure class Bad result. If the rerun uses the rest of the Wall limit budget, Maestro records each remaining check as `skipped` with the reason `wall limit after a rerun`. The Task keeps Failure class Bad result and the first check's command and output in its error text. The Ledger keeps these skipped checks and their reason. If budget remains, Maestro continues with the next check.

### Check output

Check output keeps its last 4 KB. When a check fails, it also keeps up to 2 KB of lines that contain `--- FAIL`, `FAIL\t` (with a tab), `panic:`, `Error:`, `error:`, `✗`, or `not ok`, before that tail. These lines keep the test name and failure message when the output is long.

## Keep-awake

With [`keep_awake`](configuration.md#failure) on, each Maestro process shares one `caffeinate -i -s -w <pid>` while it works or waits on live work. This includes every Lane, Run, and Council, `maestro wait` for a Dispatch, Run, or Council, the `--wait` paths, the Relay verb, and a hook that waits for a Relay answer. The process releases its assertion when the work or wait ends.

`-i` prevents idle sleep on battery and AC power; `-s` also prevents system sleep on AC power. This does not prevent display sleep. Keep-awake and the end-of-run notice are macOS only; on Linux they do nothing.

In hook scope, a Reminder event also protects the Orchestrator's turn. It starts a detached `caffeinate -i -s -w <orchestrator pid> -t 1800`, a lease of 30 minutes. Each later Reminder event replaces the same process's caffeinate and refreshes its lease. The lease record is `~/.maestro/control/awake/<pid>`, and Maestro removes the records of Orchestrator processes that have ended.

Maestro finds the Orchestrator as the nearest parent process whose executable matches a configured Harness binary; when none matches, it does nothing. If the Orchestrator process is gone or replaced before Maestro records the lease, Maestro writes one line to `~/.maestro/control/awake/worker.log`. Reminder and Stop events do no sleep-control work when `keep_awake = false`, outside hook scope, or in a Lane.

| Harness | Starts or refreshes the lease | Ends the lease |
|---|---|---|
| Claude Code | `UserPromptSubmit` | `Stop`, process exit, or 30 minutes after the last Reminder event, whichever comes first |
| Codex | `UserPromptSubmit` | `Stop`, process exit, or 30 minutes after the last Reminder event, whichever comes first |
| Antigravity | `PreInvocation` | `Stop`, process exit, or 30 minutes after the last Reminder event, whichever comes first |
| Grok | None: its Carrier is a rules file, with no Reminder event | No Orchestrator turn protection; its Lanes and blocking Maestro verbs still have protection |

The lease protects a turn, not an idle Harness session. If `Stop` does not reach Maestro, the lease releases the Mac within 30 minutes of the last Reminder event. `maestro doctor` reports a missing entry as a partial Adapter. The current [Codex hooks](https://learn.chatgpt.com/docs/hooks) and [Antigravity hooks](https://antigravity.google/docs/hooks) both provide `Stop`.

## Doctor and Canary

`maestro doctor` shows the stored Canary result for each installed Harness version and starts no Dispatch.

`maestro doctor` warns when a Project file has no active checks. It also warns when a project check starts with `docker` and `dirs` does not include `~/.docker` or its absolute path. These warnings do not fail the doctor command.

`maestro doctor` prints `worktrees on` or `worktrees off (<reason>)` for the project's checks and the paths it would link. When the tree has uncommitted changes, its project row says `tree has uncommitted changes: Tasks start from HEAD` as information. When a setting turns Worktrees off, the reason names that file. It warns `not linked into Worktrees: <names>` for other ignored `.env*` files in the root; add those files to `worktree_links` if the checks need them.

Run `maestro doctor --canary` to check every installed Harness, or `maestro doctor --canary codex` to check one. Each Canary uses your Account for one small read Dispatch with the cheapest enabled Family. It runs in a scratch folder. Maestro removes that folder after a pass. After a fail, it keeps the files under `~/.maestro/cache/maestro-canary-<harness>`, and the doctor line gives the path. The next Canary for the same Harness removes those files first.

A pass needs a process that starts and ends with no error, a Lane tool call judged by the Maestro hook, and a Report with a valid Status line. For Codex, the hook part does not apply: the Codex sandbox holds Access, and a pass needs only the process and Report parts. Maestro stores the version, time, and all three parts; an older version's pass does not count for an update.

The hook result names the control folder or the Run folder where it found the record. The Lane can write a record in the Run folder, so that record checks for a broken Harness and does not prove the Lane is safe. The Canary uses only its Wall limit. Canary Dispatches have `canary: true` in the Ledger and do not count in Route statistics. A Canary runs only when you request it.

## Solo

Every Lane runs as a headless child process, inside Solo and outside it. This includes Run Tasks, single Dispatches, and Council Seats. Inside Solo, Maestro creates todos, adds Report and Merge comments, and sends wake-ups.

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

The Brief gives the Solo project id. Each Solo tool call needs this id in `project_id`. A Claude Lane can read dev server or test watcher output and add a todo comment. Other Solo tools are denied without a Relay.

A Lane has no Solo identity, so Solo could treat its action as an action of the Orchestrator. All `SOLO_` variables stay removed. With no Solo link, a Claude Lane gets no MCP config. Other Harnesses and the Canary get no Solo MCP tools.

## Verbs

### Wait

`maestro wait <task|run>` returns with exit code `2` as soon as a Relay opens, a Run pauses, or a Task of a Run is blocked or failed and waits for the Orchestrator. It prints the item, the reason, and the next `maestro answer` or `maestro resume` command. Relay input in the reason and in the Reminder is limited to 300 characters. Answer, then use `maestro wait` with the same id to continue. Answered or resolved items do not stop it again.

At the end, `wait` prints the report and returns the outcome's exit code. For an early return, `--json` prints one object with `kind`, `run` or `task`, and `next_command`. A failed Task has the kind `task_failed`. A paused Run also gives the path to `report.md`, in text and in the JSON field `report_path`.

For a Task of a Run, `maestro wait <task>` follows only that Task's state in its Run. A blocked or failed Task with an answer pending returns exit 2 with its Cause, Blocked items, and resume command. After an answer, it waits while that Task is waiting or running. When the Task is done, skipped, or cancelled, it prints that Task's result, even while other Tasks run.

If the Task is waiting or blocked by a dependency, the Run is paused or held, and its supervisor is gone, wait returns the Run's `run_paused` or `run_held` item with exit 2 and the resume or approve command. While the supervisor is live, a pause for a different Task does not return that Task's item.

### Exit codes

`0` ok, `1` failed, `2` needs the Orchestrator (an open Relay, a held breakdown, a paused Run, a blocked or failed Task, or a single-opinion Council). A Run that holds for approval exits with `2`; the hold rules are under [Approval holds](#approval-holds). `--json` prints one object per verb.

### Council

A Council puts several Lanes on the same question in parallel. The Router picks one Seat per vendor at the Council's Tier. The Orchestrator does the synthesis; Maestro returns a Merge.

Round 1 is independent: every Seat answers the question alone. Each later Round is a Challenge round, where each Seat sees the other answers with the names removed and may confirm, refine, or withdraw. A Seat's report ends with a fixed last line, its Position: `confirmed`, `changed`, or `withdrawn`. A Council stops early when every Seat says `confirmed`.

The Merge holds a status table, the findings per Seat, and the report paths. Jev marks weak findings and never deletes them.

### Relay

Answer a Relay with `maestro answer <task> allow|deny --relay <id>`. Use the id from the `relay` event printed by `maestro wait` or `maestro status`. For which Lanes relay and the time limit, see [Relay](#relay).

### Init

`maestro init` writes the Project file. It detects each stack from files at the project root. A project can match more than one stack. It writes active `dirs` and `allow` arrays for all matches. It writes `checks` as commented suggestions, such as `# checks = ["go test ./..."]`. Review the suggestions before you activate them: an incorrect check fails every edit Task. With no match, all keys stay commented.

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

The generated allow rules cover Go test, vet, build, and gofmt; the Node runner's test and run commands; Composer test, PHPUnit, Pint, and PHPStan; pytest and ruff; Cargo test, build, clippy, and fmt; and Docker compose and build. Node test, Composer test, and pytest also get a bare rule. `Bash(docker run *)` stays a commented suggestion because it permits host path mounts.

For `craftcms/cms` in `composer.json`, `maestro init` proposes `worktree_links = ["storage", "web/cpresources", "config/license.key"]`. For `laravel/framework`, it proposes `worktree_links = ["storage", "bootstrap/cache"]`. The line stays commented when any proposed path is absent. It keeps an existing active or commented `worktree_links` key.

If the Project file exists, `maestro init` shows the proposed file and changes nothing. `maestro init --write` merges the additions. It keeps every existing key, value, and comment, and appends only missing values to `dirs` and `allow`. A value in a comment also counts as present, so a value that you comment out stays inactive. It adds a commented `checks` line only when no active or commented `checks` key exists. A second `--write` with the same stack and cache paths adds nothing.

## Generated state

Maestro writes these paths itself. Do not edit them by hand.

| Path | Written by |
|---|---|
| `~/.maestro/env` | `maestro key set` writes the Jev key. |
| `~/.maestro/ledger.jsonl` | Every finished Dispatch, Council, and Run appends one row. The Ledger is append only; rows are never updated. |
| `~/.maestro/budget.json` | Dispatches (Harvest) and `maestro budget` (Probe) store Readings. |
| `~/.maestro/cache/discovery.json` | `maestro roster refresh` rewrites it, and any verb rewrites it when it is stale. |
| `~/.maestro/control/<project-key>/` | Dispatches store Lane policy and Relay files. |
| `~/.maestro/canaries/` | `maestro doctor --canary` stores the latest check per Harness. |
| `~/.maestro/cache/maestro-canary-<harness>/` | A failed Canary keeps its check files until the next Canary for that Harness. |
| `~/.maestro/runs/` | `maestro run` writes one index file for each live Run. |
| `.agents/maestro/runs/` | `maestro dispatch` and `maestro run` write one Run folder for each Run. |
| `.agents/maestro/councils/` | `maestro council` writes one folder for each Council. |

The Run folder of a Dispatch is `.agents/maestro/runs/<run>/<task>/`. It holds the rendered Brief, the Report, the raw stream, and the Dispatch record. `run.json` in the Run folder is the Run state: the per-Run supervisor rewrites it while a Run is live, and `maestro status` reads it. `report.md` in the Run folder is the Run report: the short text that the Orchestrator gets when a Run ends or pauses, with a header, one line per Task, then the items that need the Orchestrator.

Maestro writes a `.gitignore` into each Run and Council folder it creates, so nothing under them reaches git.

`maestro clean --older-than 30d` removes old Run and Council folders in the current project. It removes control folders across all project keys when their Dispatch or Run ended before the threshold, or their record is gone. It keeps live Run controls. It also removes old cache folders, but keeps the latest Canary files per Harness at the path shown by `maestro doctor`. It never touches the Ledger or `budget.json`. For Worktrees and branches, see [Cleanup](#cleanup).

Maestro rejects a Ledger row with no `kind`. It drops Budget Accounts that are absent from the loaded Roster before the next save. Tests must set `MAESTRO_HOME` to a temporary folder. A call to `config.Home()` in a test without that variable stops with a panic.
