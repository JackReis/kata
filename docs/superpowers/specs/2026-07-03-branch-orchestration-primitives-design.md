# Branch-orchestration primitives for kata — exploratory sketch

Status: **direction confirmed 2026-07-03 — coordination substrate, metadata-first.**
This sketch records a feature survey of an adjacent per-branch agent
orchestrator (reviewed 2026-07-03) and proposes which of its ideas kata should
grow. Maintainer constraints, confirmed the same day:

- Kata stays a tracker; orchestration (worktrees, terminals, launch) lives in
  external consumers.
- **Nothing may add complexity for existing kata users.** New capability
  arrives as additional fields/metadata on issues and additive read paths,
  not as new required concepts.

The reference consumer is
[middleman](https://github.com/kenn-io/middleman), which already ships a kata
mode (talking to external kata daemons via `$KATA_HOME/config.toml`
discovery) and a kata-task-workspace-launch design that maps a kata task to
one tracked repository and creates a worktree keyed by the task's UID.
Middleman supplies the launch/dashboard half; kata supplies the durable board.

## Context

The surveyed orchestrator pairs a tracker CLI with a daemon that creates git
worktrees, launches coding agents into managed terminals, and follows each
branch. Its tracker-side ideas map cleanly onto kata; its orchestration side
does not. Four ideas stand out:

1. **Branch as the unit of work.** Every launched agent session gets a branch
   plus a *tracking issue* claimed by that branch; the issue is the handle for
   following the session.
2. **Two-axis status.** Mechanical lifecycle (running/orphaned/done, owned by
   the orchestrator) is kept separate from **attention** — the agent's own
   signal of whether it needs a human: `ok`, `attention`, or `blocked`, plus a
   one-line current-state message. The dashboard filters on attention, not on
   guessed activity.
3. **Blocking waits.** `issue wait <n>` blocks until a delegated issue closes
   or its branch raises attention, so agents can fan work out into parallel
   sub-sessions and join on them.
4. **Delegation attribution.** Issues created from inside a session record the
   originating branch, so a branch's delegated sub-tree is queryable.

## What kata already has

The substrate is nearly complete on the daemon side:

- **A metadata patch endpoint.** `POST
  /api/v1/projects/{project_id}/issues/{ref}/metadata` applies a per-key
  merge patch to the issue's free-form `metadata` object: supplied keys are
  set, `null` clears a key, omitted keys are untouched. It supports optional
  `If-Match` revision preconditions (412 on conflict), returns the new
  revision as an ETag, and emits an event carrying per-key before/after
  diffs (`internal/metadata/diff.go`). Projects have the same endpoint.
- **A reserved-key registry with typed validators.**
  `internal/metadata/registry.go` reserves keys that carry daemon-side
  semantics (`scheduled_on`, `deadline_on`, `someday`, `checklist`,
  `timezone` on issues; `area` on projects) and validates their values by
  type. **All other keys are accepted opaquely by design** — the package doc
  states the goal: consumers can carry their own metadata without
  coordinating a schema release. SQLite expression indexes are the
  documented path when a reserved key needs query performance.
- **Federation already wired.** The metadata path enforces federated issue
  claim gates and read-only replicas, and metadata patches are events, so
  they fold through federation like any other mutation.
- **Event-following already correct.** SSE plus polling parity and the
  purge-cursor reset rule give consumers a sound way to track metadata
  changes.

Gaps, all client-side or additive:

- `CreateIssueRequestBody` does not accept `metadata`
  (`additionalProperties: false`), so a launcher must create-then-patch.
- The kata CLI has no metadata surface at all: no way to set, read (beyond
  raw `--json`), or filter by metadata. The existing reserved keys are
  daemon-only features serving external consumers.
- `list` cannot filter on metadata keys.

So the plan is layered: expose what exists, document conventions over it, and
promote conventions into the registry only when the daemon takes on semantic
load for them.

## Layer 0 — generic metadata plumbing (small, code)

No orchestration concepts in kata core; just finish exposing the existing
capability:

- `kata meta set <ref> <key> <value>` / `kata meta unset <ref> <key>` /
  `kata meta get <ref> [key]` — thin CLI over the existing patch endpoint.
  `set` writes JSON strings by default with an explicit flag for raw JSON
  values; `unset` sends `null`. (Alternative: `--meta k=v` flags on `edit`;
  the dedicated verb keeps `edit` small.)
- Accept `metadata` in the create request body (validated through the same
  registry path) so a launcher can bind at creation; until then
  create-then-patch works and is safe to retry.
- `kata list --meta key[=value]` filter, and metadata rendered in `show`
  (already present in `--json` today).

Invisible to anyone who doesn't use it; useful beyond orchestration — it also
gives the CLI access to the existing scheduling keys.

## Layer 1 — orchestration conventions (docs only)

A documented contract, not code. Proposed keys, prefixed to avoid colliding
with user data and future reserved keys:

- `work.branch` — git branch doing the work (string). Coordination metadata,
  never validated against a repository: kata does not learn git.
- `work.attention` — `ok | attention | blocked`, asserted by the working
  agent. Distinct from blocks/blocked-by dependency links, which model issue
  ordering; documentation must disambiguate the two senses of "blocked".
- `work.attention_msg` — one-line current-state message.

Semantics the contract must state explicitly:

- **Scope.** Attention is meaningful only while the issue is open. Closing
  does not touch metadata, so consumers must ignore `work.*` on closed
  issues rather than expect a reset.
- **Concurrency.** Per-key merge means writers of different keys never
  conflict. For attention updates, unconditional last-write-wins is the
  intended behavior; `If-Match` is available when a caller genuinely needs
  read-modify-write.
- **Ownership.** One writer per key by convention: the launcher owns
  `work.branch`; the working agent owns the attention pair. The mechanical
  lifecycle axis (running/orphaned/done) stays in the orchestrator's process
  supervision and is deliberately absent here.

Plus the tracking-issue recipe as an operations-guide chapter: launcher
creates the issue with an idempotency key and sets `work.branch`; the working
agent keeps `work.attention` current; a coordinator follows via events or
polling; merge automation closes with evidence via service token (existing
issue `3r3e`). Placeholder names only (`spoke-project`, `agent-a`).

## Layer 2 — promotion and sugar (later, only if usage proves out)

- **Reserve the `work.*` keys.** When the daemon takes semantic load (e.g. a
  `list` attention filter or TUI badge), add them to `IssueRegistry` with
  validators — `work.branch` and `work.attention_msg` as strings,
  `work.attention` needing a small enum validator type. This is exactly the
  registry's documented promotion path and still adds no schema columns.
- `kata wait <ref> [--until closed|attention|blocked] [--timeout] [--any|--all]`
  — additive read-only command looping over the existing SSE stream/polling;
  the fan-out/join primitive for delegating agents.
- TUI: attention badge and filter driven by the `work.*` keys.
- Typed columns only if expression indexes and the registry prove
  insufficient (e.g. federation-fold or hot-path join needs).

## Consumer split (middleman)

Middleman's existing kata workspace-launch resolver already handles
task→repository mapping and worktree ownership by task UID. On top of layers
0–1 it can:

- set `work.branch` when it creates a workspace for a kata task;
- badge kata tasks in its UI with `work.attention` and offer a
  "needs a human" filter;
- warn on workspace teardown when the bound issue is still open.

Nothing in kata's layers depends on middleman specifics; it is the first
consumer, not a coupling.

## Acceptance sketch

The design is proven when this loop works end-to-end with only Layer 0 code
in kata: an orchestrator creates a tracking issue and sets `work.branch`; an
agent in the worktree runs `kata meta set <ref> work.attention blocked` plus
a message; a dashboard following the event stream (or polling `list --meta`)
surfaces the issue as needing a human within one poll interval; the agent
clears it; merge automation closes the issue; the closed issue's `work.*`
metadata is ignored everywhere.

## Out of scope

- Worktree/terminal/session management of any kind in kata.
- Lifecycle status (running/orphaned/done) in kata.
- A `delegated` link type: existing `--parent` plus event actors already
  attribute delegation; add nothing until a real consumer needs more.
- New required workflow for existing users: every layer is opt-in and
  invisible when unused.

## Open questions for the maintainer

1. Key naming: existing reserved keys are flat (`scheduled_on`), so the
   dotted `work.` prefix introduces a new style. Keep the prefix (clearer
   namespacing for convention keys), go flat (`work_branch`), or pick a
   different prefix?
2. Should Layer 0 include the `list --meta` filter from the start, or ship
   write/read first and add filtering when a consumer needs it?
3. Is `kata meta` the right CLI shape, or fold into `edit --meta k=v`?
4. Should `work.attention` be registry-reserved (validated enum) from day
   one rather than at promotion time? Cheap to do, prevents typo'd levels,
   but commits the key names earlier.
