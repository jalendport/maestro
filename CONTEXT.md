# Maestro — ubiquitous language

The glossary for Maestro. Terms only; no implementation detail. Code, docs, and briefs use these words and no others for these things.

## Terms

- **Maestro** — a local tool that owns the work after the thinking is done. It turns a plan into Tasks, routes, dispatches, checks, and runs Councils.
- **Orchestrator** — the model session the user talks to, in any Harness. It plans and hands work to Maestro. It is a role, not a program.
- **Harness** — a CLI agent program that can run a model on a task: Claude Code, Codex, Grok, Antigravity.
- **Lane** — one runnable combination of harness + model + effort, for example `codex gpt-5.6-sol high`. Maestro routes Tasks to Lanes.
- **Roster** — the list of all Lanes with their facts: cost, speed, strengths, limits, billing kind.
- **Billing kind** — whether a Lane draws on a vendor subscription or pays per token through an API key. Drives how often the Lane is used.
- **Budget** — what is left for a Lane's billing source: percent of a subscription window and its reset time, or money for a token-billed Lane.
- **Task** — one unit of work with a brief and a definition of done.
- **Route** — the decision "this Task goes to this Lane".
- **Router** — the part of Maestro that makes Routes: Jev, with a rules fallback.
- **Dispatch** — starting a Lane on a Task and collecting its result.
- **Council** — several Lanes answering the same question in parallel, then synthesized.
- **Adapter** — the small per-Harness piece that connects an Orchestrator to Maestro: one shared skill file plus one hook file. Maestro itself works without any Adapter. `maestro adapter install` writes it.
- **Run** — one invocation of Maestro on a plan: its Tasks, their Routes and Dispatches, the checks, and the report back to the Orchestrator.
- **Brief** — the text a Lane receives for one Task: the goal, the definition of done, the files, the checks, and the permission mode.
- **Tier** — a rung on the strength ladder that Lane families sit on: Light, Daily, Heavy, Frontier. "One Tier up" means the next rung.
- **Judgment level** — how much a Lane must decide on its own to finish a Task: none, some, significant, or expert. It sets the Tier a Task needs.
- **Account** — one login that Lanes bill against, with its billing kind and plan price. Several Lanes can share one Account.
- **Headroom** — the part of an Account's usage window that is still free.
- **Allocation weight** — what breaks a near-tie between equally fit subscription Lanes: the Account's plan price times its Headroom.
- **Fan-out effort** — an effort setting that starts sub-agents on its own, such as Codex `ultra` or Claude `ultracode`.
- **Rules table** — the deterministic Router: per Tier and category, an ordered chain of Lane families. It observes every Route and decides only when Jev is absent.
- **Reason line** — the one line Maestro prints for each Route: the Lane family, its Tier, the Judgment level, and the tie-break used.
- **Window** — one usage period a vendor counts for an Account, such as Claude's five-hour and seven-day windows or Codex's weekly window.
- **Reading** — one stored Budget value for a window, with the time it was read and where it came from.
- **Block point** — the used share of a window at which a Lane leaves the Route list.
- **Reserve** — the part of the Claude weekly window that automatic Dispatches may not use. It keeps room for the Orchestrator.
- **Probe** — a cheap call Maestro makes only to read a Budget.
- **Harvest** — reading a Budget from the output of a Dispatch that ran anyway.
- **Family** — one Harness × model pair, the unit the Router chooses between. A Lane is a Family plus one effort. Written `<harness>-<short model>`, for example `codex-sol`.
- **Catalog** — the list of models a Harness can run today, as its own list command reports it.
- **Embedded defaults** — Roster data compiled into the `maestro` binary: families, levels, text, chains, prices. A release updates them; the user file holds only deltas.
- **Discovery cache** — the file Maestro generates from the catalogs and rewrites freely. Never edited by hand.
- **Level map** — a Family's map from Judgment level to effort. The Tier is derived from the levels a Family serves.
- **Access** — the part of the Brief that says whether a Lane may change files. Two values: `read` and `edit`. There is no third value; Maestro never passes a bypass flag to a Lane.
- **Enforcement** — a measured fact per Harness and Access value: `enforced` when a sandbox or a tool allowlist holds the Access with no help from the model, `best-effort` when only an instruction or a weak flag does. A `read` Task goes only to `enforced` Lanes.
- **Relay** — Maestro passing a Lane's permission prompt to the Orchestrator and typing the answer back. The Orchestrator may ask the user. An unanswered Relay is denied after 10 minutes.
- **Failure class** — the name Maestro gives to one kind of Dispatch failure. Five values: Lane error, Transient error, Limit hit, Stall, Bad result. Every ledger row and failure message carries exactly one.
- **Idle limit** — the longest time a Dispatch may run with no new output. One value for every Tier. An open Relay pauses it.
- **Wall limit** — the longest total time a Dispatch may run. Grows with the Tier; a Brief may raise it for one Task.
- **Seat** — one Lane's place in a Council. The Router picks one Seat per vendor at the Council's Tier.
- **Round** — one pass where every Seat answers the same question. Round 1 is independent.
- **Challenge round** — a Round after the first, where each Seat sees the other answers with the names removed and may confirm, refine, or withdraw.
- **Position** — the fixed last line of a Seat's report: `confirmed`, `changed`, or `withdrawn`. A Council stops early when every Seat says `confirmed`.
- **Merge** — what Maestro returns from a Council: a status table, the findings per Seat (Jev marks weak ones, never deletes them), and the report paths. The Orchestrator does the synthesis.
- **Brief file** — the Markdown file the Orchestrator writes for one Task: a front matter of short fields and fixed headings for the Goal and the Done.
- **Plan file** — the Markdown file with every Task of a Run, each a section with the same fields as a Brief file plus `blocked_by`.
- **Pointer** — the one-line prompt a Lane receives in every launch form: read the Brief at this path, write the Report in the same folder.
- **Report** — the file a Lane writes when it finishes (`result.md`): fixed headings by Access, ending with the Status line.
- **Status line** — the fixed last line of a Report: `done`, `partial`, or `blocked`. Maestro writes `unshaped` when the Lane left no usable Report.
- **Run folder** — the per-Dispatch folder `.agents/maestro/runs/<run>/<task>/` with the rendered Brief, the Report, the raw stream, and the Dispatch record.
- **Ledger** — the one global file (`~/.maestro/ledger.jsonl`) where Maestro records every finished Dispatch, Council, and Run. Append only; rows are never updated.
- **Run state** — the file (`run.json` in the Run folder) the per-Run supervisor rewrites while a Run is live. `maestro status` reads it.
- **Run report** — the short text (`report.md` in the Run folder) the Orchestrator gets when a Run ends or pauses: a header, one line per Task, then the items that need the Orchestrator.

## Words to avoid

- **Tool** for a Lane. Claude Code and MCP already use "tool" for functions such as Read, Bash, or `spawn_agent`.
- **Planner** for the Orchestrator.
- **Orchestrator** for Maestro itself. Maestro is the magic box; the Orchestrator is the session that directs it.
- **Provider** for a Lane or a Harness. Say Harness for the program and vendor for the company.
- **Permission mode** for Access. Claude and Grok use "permission mode" for their own flag; Maestro says Access for the Brief field.
