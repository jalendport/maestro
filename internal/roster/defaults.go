package roster

// Defaults is the Roster data compiled into the binary: families, level maps,
// text, accounts, rules table, prices, Claude aliases, per-Harness minimum
// versions, and the global blocks with their settled values. A release
// updates it; the user file holds only deltas.
const Defaults = `
version = 1

[accounts.claude]
harness       = "claude"
billing       = "subscription"
block_at      = 0.90

[accounts.codex]
harness       = "codex"
billing       = "subscription"
block_at      = 0.90

[accounts.grok]
harness       = "grok"
billing       = "subscription"
block_at      = 0.90

[accounts.agy]
harness       = "agy"
billing       = "subscription"
block_at      = 0.90

[harness.claude]
min_version = "2.1.283"
strip_env   = ["ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_EFFORT_LEVEL", "CLAUDE_CODE_SUBAGENT_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL_FORCE"]

[harness.codex]
min_version = "0.156.1"
strip_env   = ["OPENAI_API_KEY"]

[harness.grok]
min_version = "1.0.50"
strip_env   = ["GROK_SANDBOX"]

[harness.agy]
min_version = "1.2.9"
strip_env   = []

[aliases]
haiku  = "claude-haiku-5-5"
sonnet = "claude-sonnet-5-5"
opus   = "claude-opus-5-5"
fable  = "claude-fable-5-1"

[families.claude-haiku]
harness  = "claude"
models   = ["haiku"]
levels   = { none = "" }
good_for = "docs, release notes, one-file lookups, formatting"
not_for  = "writing code, anything with judgment"

[families.claude-sonnet]
harness  = "claude"
models   = ["sonnet"]
levels   = { some = "high" }
good_for = "prose, small scoped edits, documentation with a known shape"
not_for  = "multi-file approach decisions, unreproducible failures"

[families.claude-opus]
harness  = "claude"
models   = ["opus"]
levels   = { significant = "high", expert = "xhigh" }
good_for = "the one pattern-setting lane per feature, UI polish, design and review, specs"
not_for  = "mechanical follow-ons that a smaller lane can do"

[families.claude-fable]
harness      = "claude"
models       = ["fable"]
levels       = { expert = "high" }
frontier     = true
good_for     = "expert judgment, design and review, specs"
not_for      = "day-to-day work"

[families.codex-luna]
harness  = "codex"
models   = ["gpt-6-luna"]
levels   = { none = "medium" }
good_for = "mechanical edits with every change named, renames, boilerplate"
not_for  = "open questions, prose"

[families.codex-sol]
harness  = "codex"
models   = ["gpt-6.1-sol", "gpt-6-sol"]
levels   = { some = "medium", significant = "high", expert = "xhigh" }
good_for = "spec-slice implementation, mechanical follow-ons, test work, fixes with a known cause"
not_for  = "open design questions, prose"

[families.codex-astra]
harness  = "codex"
models   = ["gpt-6-astra"]
levels   = { expert = "high" }
frontier = true
good_for = "expert judgment after Heavy failed: concurrency, security, architecture"
not_for  = "day-to-day work"

[families.grok]
harness  = "grok"
models   = ["grok-4.7"]
levels   = { none = "low", some = "medium", significant = "high", expert = "xhigh" }
good_for = "issue triage, contrarian checks, quick facts, second opinions"
not_for  = "security-sensitive edits, multi-file research"

[families.agy-flash]
harness  = "agy"
models   = ["gemini-3.8-flash"]
levels   = { none = "low", some = "high" }
good_for = "very large inputs up to 1M tokens, cited web research"
not_for  = "edits"

[families.agy-pro]
harness  = "agy"
models   = ["gemini-3.1-pro"]
levels   = { significant = "high" }
good_for = "very large inputs that need judgment, cited web research"
not_for  = "edits"

[rules.light]
default       = ["codex-luna", "claude-haiku", "grok", "agy-flash"]
triage        = ["grok", "codex-luna", "claude-haiku", "agy-flash"]
"large input" = ["agy-flash", "codex-luna", "claude-haiku", "grok"]

[rules.daily]
default          = ["codex-sol", "claude-sonnet", "grok", "agy-flash"]
research         = ["grok", "codex-sol", "claude-sonnet", "agy-flash"]
triage           = ["grok", "codex-sol", "claude-sonnet", "agy-flash"]
prose            = ["claude-sonnet", "codex-sol", "grok", "agy-flash"]
"second opinion" = ["grok", "codex-sol", "claude-sonnet", "agy-flash"]

[rules.heavy]
default       = ["claude-opus", "codex-sol", "grok", "agy-pro"]
implement     = ["codex-sol", "claude-opus", "grok", "agy-pro"]
fix           = ["codex-sol", "claude-opus", "grok", "agy-pro"]
refactor      = ["codex-sol", "claude-opus", "grok", "agy-pro"]
review        = ["claude-opus", "codex-sol", "grok", "agy-pro"]
spec          = ["claude-opus", "codex-sol", "grok", "agy-pro"]
"large input" = ["agy-pro", "claude-opus", "codex-sol", "grok"]

[rules.frontier]
default = ["codex-astra"]

[prices."claude-fable-5-1"]
input = 10.00
cached_input = 0.25
output = 50.00

[prices."claude-opus-5-5"]
input = 4.00
cached_input = 0.20
output = 20.00

[prices."claude-sonnet-5-5"]
input = 2.00
cached_input = 0.10
output = 10.00
[prices."claude-sonnet-5"]
input = 2.00
cached_input = 0.20
output = 10.00

[prices."claude-haiku-5-5"]
input = 0.10
cached_input = 0.01
output = 0.50
[prices."claude-haiku-4-5"]
input = 1.00
cached_input = 0.10
output = 5.00

[prices."gpt-6-astra"]
input = 10.00
cached_input = 1.00
output = 50.00

[prices."gpt-6.1-sol"]
input = 2.00
cached_input = 0.10
output = 10.00

[prices."gpt-6-sol"]
input = 2.00
cached_input = 0.20
output = 10.00

[prices."gpt-6-luna"]
input = 0.10
cached_input = 0.01
output = 0.50

[prices."grok-4.7"]
input = 2.00
cached_input = 0.50
output = 6.00

[prices."gemini-3.8-flash"]
input = 0.75
cached_input = 0.075
output = 3.75

[prices."gemini-3.1-pro"]
input = 2.00
cached_input = 0.20
output = 12.00

[hook]
scope = "projects"

[router]
backend = "jev"
base_url = "https://api.typesafe.ai/v1/systemone"
model = "jev-1.13.0"

[router.thresholds.jev]
dispatch = 0.70
step_up = 0.40
second_option = 0.20
vague_brief = 0.30
heavy_override = 0.70
council_view = 0.80
council_write = 0.30
ask = 0.50
timeout_s = 1.5
satisfies = 0.65

[budget]
stale_after = "30m"
block_at = 0.90
warn_at = 0.75
assumed_headroom = 0.50

[failure]
idle_limit = "10m"
wall_limit = { light = "15m", daily = "30m", heavy = "60m", frontier = "90m" }
max_dispatches = 6
keep_awake = true
run_pause_after_escalations = 3
run_pause_after_same_error = 2

[run]
council_tasks = 10

[concurrency]
max_parallel = 4
max_per_account = 2
max_total = 6
close_tabs = "on-success"
`
