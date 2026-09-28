# Simplifications that help agents

Ranked for information ergonomics. Each item is a candidate or a rule for future edits. This harvest does not perform the deletions. Drive-by rewrites, schema changes, and branch deletion are out of scope.

Evidence is the 2026-09-27 tree: file modes, `wc`, package file counts, and the branch inventory in `BRANCHES.md`.

## 1. Load the agent contract once

`CLAUDE.md` is already a symlink to `AGENTS.md` (`git ls-files -s` mode `120000`, target `AGENTS.md`, 354 lines). Commit `c5e9cc7` ("Consolidate agent guidance into AGENTS.md, symlink CLAUDE.md") is the landed form.

Some sessions still inject both paths and pay for the contract twice. Read `AGENTS.md` only. Do not replace the symlink with a second copy.

## 2. Stop treating `dev` as integration

`origin/dev` is 3 commits ahead and 65 behind `main`, with a different tree (`2eceee23…` vs `main` `74d16631…`) and a tip subject that duplicates `main`. Agents that branch from `dev` or cherry-pick its beads commits will fight `ad85e09` and `b37aa01`.

Candidate, for a human with remote delete rights: delete `dev` after confirming no one tracks it. Do not do that from a docs change.

## 3. Retire stale heads instead of replaying them

Safe to forget, because `main` already contains the outcome or a later replacement (see `BRANCHES.md`):

- `claude-hooks-design`
- `feat/issue-58`
- `port-env-bind`
- `windows-port`
- `fix/beads-import-aegis-20260810`
- `pi-tasks-kata-plugin` (only if the plugin is still unwanted)
- `wesm/branch-orchestration-spec`
- `feat/issue-59` (only after someone checks the five paths are not a missing release workflow)

Hold `wesm/branch-orchestration-fixes` until a file-level diff shows the wait/meta fixes are present or still needed. Hold `docs-assets`; it is generated storage with no merge base, not a stale feature.

## 4. Pair the two stores; do not merge them

`internal/db/sqlitestore` and `internal/db/pgstore` are the hot persistence paths (86 and 82 Go files). Shared basenames are few: `store.go`, `schema.go`, `schema.sql`, `imports.go`, `export.go`, `issue_sync.go`, `tokens.go`, `ui.go`, `conformance.go`, federation ingest, idempotency, transaction fence. SQLite keeps `queries_*.go`. Postgres keeps `migrations.go`, `schema_manifest.go`, `issue_lifecycle.go`, and a wider `federation_*.go` set.

That split is the product (SQLite default, Postgres via `KATA_DSN`, identical scoping). Collapsing to one engine would be a new backend, not a cleanup.

Agent rule: a ledger behavior change names both stores in the same change, or states why one backend cannot express it. Do not copy a `queries_*.go` edit into a guessed Postgres file.

## 5. Regenerate the client OpenAPI; do not hand-merge it

`api/openapi.yaml` is 176667 bytes. `pkg/client/openapi.yaml` is 171509 bytes. They are not identical. `pkg/client` is the generated daemon API client (`package client`, oapi-codegen runtime). Editing either file by hand to "sync" them will drift the client from the Huma contract.

## 6. Keep design notes and operations docs as two layers

Largest paired texts:

| Topic | Maintainer note | Operator / user doc |
| --- | --- | --- |
| Federation | `docs/design/federation.md` (749 lines) | `docs/operations/federation.md` (913) |
| Semantic search | `docs/design/semantic-search.md` (571) | `docs/guide/semantic-search.md` (198) |
| GitHub sync | `docs/design/github-sync.md` (156) | `docs/operations/github-sync.md` (304) |
| Hosted mode | `docs/design/hosted-mode.md` (39) | `docs/operations/hosted-mode.md` (78) |

`docs/design/index.md` already says these notes are not the quick path, and that finished plans should be folded in and drafts removed. Do not concatenate the pairs. When context is scarce, read the operations or guide page for steps and the design page only for the invariant you are about to touch.

`README.md` (230 lines) and `docs/index.md` overlap on purpose: GitHub landing page versus the docs site. Leave both.

## 7. Leave the root `kata` package colocated

Module-root `service.go` (645 lines) plus `service_projects.go`, `service_federation_enrollments.go`, `service_credentials.go`, and their tests are the embeddable HTTP service (`docs/development/embedding.md`). Go requires them in one package. Moving them for tidiness splits the public embed API from its tests and does not shorten the path an agent should take (`service.go` and `docs/development/embedding.md`).

## 8. Do not finish the connector inside a drive-by

`docs/reference/connector-protocol.md` (last edited 2026-08-23) states the SDK and conformance kit exist, and that daemon configuration, durable bindings, synchronization, and API/CLI administration do not. Adding a config section fails closed because unknown keys are rejected. The simplification is to keep connector work unstarted until an issue asks for the daemon half.

## 9. Use MCP section loaders instead of a bigger prompt

`internal/mcp/sections.go` registers 13 loaders: activity, federation, import, issue discovery, issue lifecycle, issue mutation, leases, projects, recurrence, storage (only if enabled), sync, system, tokens (only if token admin and full scope). About 89 `toolHandlers` methods exist behind those loaders. Agents that paste the full catalog into context are fighting the design. Load one section.

## 10. Keep this harvest off the public nav

`docs/scripts/public_markdown_sources.py` publishes only markdown listed in `docs/zensical.toml`. The files under `docs/agent-memory/` and `docs/DUNE-CI.md` are intentionally unlisted. Adding them to the nav, or retargeting `repo_url` from `github.com/kenn-io/kata` to this checkout's origin, is a product-docs change, not a memory cleanup.
