# Decisions

## Maestro is written in Go, not TypeScript

Every peer project in this space (counselors, routr, oh-my-subscriptions, jev-router) is TypeScript, and Jev ships only Python and JavaScript SDKs, so TypeScript compiled with Bun was the obvious choice. We chose Go anyway (decided 2026-09-24 in the form-factor grilling): Jalen already maintains a Go CLI (`spark-cli`) with a cobra layout, goreleaser, a Homebrew tap, and a curl installer that Maestro copies as is; a single static binary is the whole product, since every Harness Adapter is only a skill file plus a hook file that calls it; and Go starts fastest inside a PreToolUse hook that runs on every tool call, which the research flagged as the one place TypeScript start-up could hurt. The Jev SDK gap does not matter: Jev is one REST call with a Bearer key.

- **Status:** accepted
- **Considered options:** TypeScript + `bun build --compile` (peer ecosystem, Jev JS SDK); Bash like jamsession (zero build, hard to keep correct at this size); PHP (Jalen's daily language, but no single-binary story); Go.
- **Consequences:** ideas from the TypeScript peers are ported, never vendored. The Claude Code plugin cannot bundle a JS runtime dependency and only points at the binary on PATH, which was decided independently anyway.

## The public name and the binary stay "Maestro"

The name is taken on Homebrew (the RunMaestro cask), npm (`maestro`, `maestro-cli`), PyPI, and GitHub (mobile.dev's Maestro, Netflix's, Doriandarko's), so the research flagged it as a blocker before the first build. Jalen kept it anyway (decided 2026-10-09 in the naming ticket): the mobile.dev tool is mobile end-to-end testing, a space with no overlap in users or purpose; the binary ships only through `jalendport/homebrew-tap` and the curl installer, installed by its full tap path (`brew install jalendport/tap/maestro`) so the cask never resolves; nothing is published on npm. The GitHub repo `jalendport/maestro` already exists under Jalen's account.

- **Status:** accepted
- **Considered options:** `baton` (free on Homebrew, abandoned npm and PyPI packages, clear on GitHub in this space); `kapell` (free on every registry, obscure); `podium`, `downbeat`, `upbeat`, `rehearsal` (weak conflicts); `tempo`, `tutti`, `cue` (strong conflicts: Temporal, the tutti-os agent platform, the CUE language).
- **Consequences:** the README's install line always carries the tap path; a user with mobile.dev's `maestro` on PATH gets a name clash that the README names; no npm wrapper package, ever.

## A passing gate verdict under the close threshold yields `unverified`, not failed

The verification gate closes a task only at `satisfies` 0.90 or more, yet on the first dogfood tasks Jev marked correct diffs `pass` with `satisfies` 0.73 to 0.89. Rather than fail good work or lower a settled threshold before the ledger has data, a `pass` verdict under the threshold yields the outcome `unverified` (exit 0, not closed, gate numbers in the record) so the Orchestrator reads the diff before accepting it (decided 2026-10-09 during milestone 2). Tune `[router.thresholds.jev] satisfies` from `maestro router stats` once enough rows exist.

- **Status:** accepted
- **Considered options:** fail the task (loses correct work and triggers retries with no findings); lower the threshold to 0.70 (changes a settled number on four samples); `unverified` with the numbers kept.
- **Consequences:** `unverified` counts as done for a Run's blockers and the report marks it `unverif.`; the end-of-Run Council sees these tasks like any other.

## The project file carries `dirs`, a sixth key

The roster decision fixed five tighten-only keys for `.agents/maestro.toml`. The Codex sandbox denies writes to the Go build cache under `~/Library/Caches`, which made every Go check inside a Lane fail until the Lane found a temporary cache. A per-project `dirs` list (extra writable directories passed as `--add-dir` on every edit dispatch) fixed it in one line, where the Brief-only `dirs:` field would have to be repeated on every brief (decided 2026-10-09 during milestone 2, flagged on todo 218 for Jalen).

- **Status:** proposed
- **Considered options:** repeat `dirs:` on every Brief; special-case Go in the adapter; a project-wide list.
- **Consequences:** `dirs` loosens, not tightens, the sandbox for the project; the user file never carries it.
