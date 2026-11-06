# Maestro — ubiquitous language

The glossary for Maestro: the words for its parts and what they mean, and nothing else. Code, docs, Briefs, and the skill use these words and no others for these things; rules, limits, and settings live in the configuration reference.

## Actors

**Maestro**:
A local tool that owns the work after the thinking is done. It turns a plan into Tasks, routes, dispatches, checks, and runs Councils.
_Avoid_: Orchestrator (Maestro is the magic box; the Orchestrator is the session that directs it)

**Orchestrator**:
The model session the user talks to, in any Harness. It plans and hands work to Maestro. It is a role, not a program.
_Avoid_: Planner

**Harness**:
A CLI agent program that can run a model on a task: Claude Code, Codex, Grok, Antigravity.
_Avoid_: Provider (say Harness for the program and vendor for the company)

**Adapter**:
The small per-Harness piece that connects an Orchestrator to Maestro: a skill, a hook, and the Carrier of the Reminder. Maestro itself works without any Adapter.

## Lanes and the Roster

**Lane**:
One runnable combination of harness + model + effort, for example `codex gpt-6.1-sol high`. Maestro routes Tasks to Lanes and starts each Lane as a headless child process.
_Avoid_: Tool (the word already names a function a model calls)

**Family**:
One Harness × model pair, the unit the Router chooses between. A Lane is a Family plus one effort.

**Roster**:
The list of all Lanes with their facts: cost, speed, strengths, limits, billing kind.

**Catalog**:
The list of models a Harness can run today, as its own list command reports it.

**Embedded defaults**:
Roster data compiled into the Maestro binary: families, levels, text, chains, prices.

**Discovery cache**:
The cache Maestro generates from the catalogs and rewrites freely.

**Level map**:
A Family's map from Judgment level to effort. The Tier is derived from the levels a Family serves.

**Fan-out effort**:
An effort setting that starts sub-agents on its own.

## Work

**Task**:
One unit of work with a brief and a definition of done.

**Run**:
One invocation of Maestro on a plan: its Tasks, their Routes and Dispatches, the checks, and the report back to the Orchestrator.

**Dispatch**:
Starting a Lane on a Task and collecting its result.

**Brief**:
The text a Lane receives for one Task: the goal, the definition of done, the files, the checks, and the Access.

**Brief file**:
The Markdown file the Orchestrator writes for one Task.

**Plan file**:
The Markdown file with every Task of a Run, each Task a section shaped like a Brief file.

**Project file**:
The per-project configuration file at the root of a project.

**Pointer**:
The one-line prompt a Lane receives in every launch form: read the Brief at this path, write the Report in the same folder.

**Report**:
The file a Lane writes when it finishes: fixed headings by Access, ending with the Status line.

**Status line**:
The fixed last line of a Report: `done`, `partial`, or `blocked`. Maestro writes `unshaped` when the Lane left no usable Report.

**Run folder**:
The per-Dispatch folder with the rendered Brief, the Report, the raw stream, and the Dispatch record.

**Run state**:
The record the per-Run supervisor rewrites while a Run is live.

**Run report**:
The short text the Orchestrator gets when a Run ends or pauses: a header, one line per Task, then the items that need the Orchestrator.

**Ledger**:
The one global record of every finished Dispatch, Council, and Run.

## Routing

**Route**:
The decision "this Task goes to this Lane".

**Router**:
The part of Maestro that makes Routes: Jev, with a rules fallback.

**Rules table**:
The deterministic Router: per Tier and category, an ordered chain of Lane families.

**Reason line**:
The one line Maestro prints for each Route: the Lane family, its Tier, the Judgment level, and the tie-break used.

**Tier**:
A rung on the strength ladder that Lane families sit on: Light, Daily, Heavy, Frontier. "One Tier up" means the next rung.

**Judgment level**:
How much a Lane must decide on its own to finish a Task: none, some, significant, or expert. It sets the Tier.

**Allocation weight**:
What breaks a near-tie between equally fit subscription Lanes: the Account's plan price weighted by its Headroom.

## Budget

**Account**:
One login that Lanes bill against, with its billing kind and plan price. Several Lanes can share one Account.

**Billing kind**:
Whether a Lane draws on a vendor subscription or pays per token through an API key. Drives how often the Lane is used.

**Budget**:
What is left for a Lane's billing source: percent of a subscription window and its reset time, or money for a token-billed Lane.

**Window**:
One usage period a vendor counts for an Account.

**Reading**:
One stored Budget value for a window, with the time it was read and where it came from.

**Headroom**:
The part of an Account's usage window that is still free.

**Block point**:
The used share of a window at which a Lane leaves the Route list.

**Reserve**:
The part of an Account's usage that automatic Dispatches leave free for the Orchestrator.

**Probe**:
A cheap call Maestro makes only to read a Budget.

**Harvest**:
Reading a Budget from the output of a Dispatch that ran anyway.

## Checks and Access

**Access**:
The part of the Brief that says whether a Lane may change files: `read` or `edit`.
_Avoid_: Permission mode (Access is the Brief field)

**Enforcement**:
How firmly a Harness holds an Access value: `enforced` when something other than the model holds it, `best-effort` when only an instruction or a model decision does.

**Relay**:
Maestro passing a Lane's permission prompt to the Orchestrator and typing the answer back.

**Scope guard**:
The check that limits an edit Task's changed paths to the ones its Brief allows.

**Plan check**:
A read Dispatch before a Run that checks the Plan against the project tree for defects in paths, symbols, scope, and order.

**Landing check**:
The project's checks on the merged Run branch just before a Run or standalone Dispatch lands.

**Container-bound check**:
A check whose command runs in a container bound to the project folder.

**Canary**:
A small read Dispatch that checks the launch and Report of an installed Harness, and the Maestro hook where it holds the Lane's Access.

## Worktrees

**Worktree**:
A separate git checkout for one Task, outside the project tree.

**Run branch**:
The git branch that holds the commits of a Run, made from the user's HEAD when the Run starts.

**Task branch**:
The git branch that holds one Task's commits, made from the Run branch when the Task starts.

**Worktree mode**:
A Run or standalone edit Dispatch mode in which edit Tasks use separate Worktrees and Maestro merges their commits into the Run branch.

**Tree mode**:
A Run or standalone edit Dispatch mode that uses the project tree.

## Failure and limits

**Failure class**:
The name Maestro gives to one kind of Dispatch failure: Lane error, Transient error, Limit hit, Stall, or Bad result.

**Cause**:
Why a Task went wrong: `brief` (the Brief excludes required work or contradicts itself), `environment` (a tool or service was denied, down, or missing), `capability` (the Lane's work is wrong or incomplete with no blocker), or `none` (nothing went wrong).

**Idle limit**:
The longest time a Dispatch or a check may run with no new output.

**Wall limit**:
The longest total time a Dispatch may run.

## Councils

**Seat**:
One Lane's place in a Council.

**Round**:
One pass where every Seat answers the same question.

**Challenge round**:
A Round after the first, where each Seat sees the other answers and may confirm, refine, or withdraw.

**Position**:
The fixed last line of a Seat's report: `confirmed`, `changed`, or `withdrawn`.

**Council**:
Several Lanes answering the same question in parallel. The Orchestrator synthesizes; Maestro returns a Merge.

**Merge**:
What Maestro returns from a Council: a status table, the findings per Seat, and the report paths.

## Reminders

**Reminder**:
The fixed text that tells the Orchestrator, on every prompt, that Maestro owns the work after the thinking is done, followed by the project's live items: open Relays, paused Runs, blocked and failed Tasks of live Runs, and running Lanes.

**Reminder event**:
A Harness hook event that fires before the model sees a prompt; the hook answers it with the Reminder.

**Carrier**:
The per-Harness path that delivers the Reminder: a hook entry for a Reminder event, or a rules file where hooks cannot add context.
