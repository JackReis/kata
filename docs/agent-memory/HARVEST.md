# Kata harvest map

Generated 2026-09-27 from the working tree at `main` tip `b37aa01` (`fix(client): raise DefaultHTTPTimeout 5s -> 30s`, committer date 2026-08-24). This note is an agent memory aid. It is not on the public docs nav in `docs/zensical.toml`, so it is not part of the katatracker.com site.

## What this repository is

Kata (`go.kenn.io/kata`) is a local-first issue tracker for coding agents and the humans steering them. It is a durable task ledger: create, claim, relate, and close issues with evidence. It is not a model runtime, a git workflow engine, or an agent worker pool.

Default state lives in SQLite under `KATA_HOME`. The repo commits a small secret-free `.kata.toml` binding. A shared daemon can use Postgres via `KATA_DSN`. Humans supervise the same ledger with `kata tui` and `kata ui`. The published docs site is configured in `docs/zensical.toml` with `repo_url = "https://github.com/kenn-io/kata"`. This checkout's `origin` is `github.com/JackReis/kata`. Those are different remotes. Commit subjects cite high pull-request numbers (`#287` and similar); `gh pr list` on this origin shows only merged `#1` and `#2`. Treat those high numbers as upstream history carried in the commit log, not as open pulls on this remote.

Changelog tip in-tree is **0.15.1** (2026-08-20) plus an **Unreleased** idle-shutdown note. `internal/version` defaults to `dev` unless the build stamps VCS.

## Open Engine is not in this tree

No package in this checkout is Open Engine or a multi-provider model router. The fleet map in [Fleet trust guidelines](#fleet-trust-guidelines) names Open Engine as a protocol role outside this tree. Do not search for an engine package or invent one.

The closest in-tree "engine" is the federation **fold**: `docs/design/federation.md` describes mutable state as a CRDT and checks `direct_write_projection == Fold(project_events)`. That fold converges hub and spoke replicas. It does not call language models.

How Kata sits next to a multi-provider agent runtime:

| Surface | What the tree actually ships | What it is not |
| --- | --- | --- |
| Ledger | CLI, HTTP API, MCP tools, TUI, web UI, all through one daemon API | A place to store prompts, transcripts, or model choice |
| Issue sync | Provider-neutral `issue_sync_*` storage and `/issue-sync/{provider}/...` routes. **GitHub is the only adapter.** One project, one repo, one-way import | A write-back to GitHub, GitLab, or Linear |
| Embeddings | Opt-in OpenAI-compatible `POST /embeddings`. Local (Ollama, LM Studio, llama.cpp) or hosted (OpenAI, Voyage, others). Kata stores vectors; it does not bundle a model | A chat or completions provider |
| Connectors | `kata.connector.v1` SDK and conformance kit in `pkg/connector`. One JSON-RPC process per call. Credentials stay inside the connector | Daemon config, durable bindings, or sync. The daemon rejects unknown `config.toml` keys |
| MCP | `kata mcp serve` (stdio, or Streamable HTTP with an env bearer). **13 section loaders**; tools appear only after a loader runs | An MCP client that picks models |
| Federation | Opt-in hub/spoke replicas. Mutual trust. SQLite and Postgres pairs must interoperate | Multi-tenant authorization, or a hostile-hub defense |
| Storage | SQLite default; Postgres optional | A client opening the database itself |

Shortest correct path for an agent that needs durable work state: `kata quickstart`, then `kata search` before `kata create`, then comments and `work.*` metadata, then `kata close` only with evidence. Model-provider choice stays outside this repository.

## Colocation

Keep these facts in the same place you change behavior:

- **One access path.** CLI, TUI, web, MCP, and embedders call the HTTP service. No client opens SQLite or Postgres directly (`docs/design/architecture.md`).
- **Project binding is `.kata.toml`, not the current directory.** Outside a bound workspace, every command except `kata init` fails with `project_not_initialized` unless `--project` is set. Alias identity comes from the normalized git remote, not the checkout path. One git repository attaches to one project.
- **Hooks are daemon-host config, not workspace config.** `.kata.toml` carries only the project binding. A committed hook must not be able to execute on a shared daemon.
- **SQLite and Postgres are one product.** `internal/db/sqlitestore` (86 Go files) and `internal/db/pgstore` (82 Go files) do not share a file-for-file layout. A behavior change that lands in only one store breaks the documented parity (federation, snapshots, filters, project isolation).
- **Agent guidance is one file.** `CLAUDE.md` is a git symlink (`120000`) to `AGENTS.md` (354 lines). Edit `AGENTS.md` only.
- **Design notes vs operations.** `docs/design/` is rationale for maintainers. `docs/operations/` and `docs/guide/` are the procedures. `docs/design/index.md` says shipped plans should be folded into the design notes and the drafts removed.
- **Public docs nav is the publish list.** A markdown file under `docs/` is not on katatracker.com unless `docs/zensical.toml` lists it.

## Where agents must not shortcut

These are product contracts already written in `AGENTS.md`, `docs/design/architecture.md`, `docs/design/github-sync.md`, `docs/reference/connector-protocol.md`, and `docs/workflows/agents.md`.

1. **Do not infer the project from the working directory** when `.kata.toml` is missing. Do not auto-create a project on first write.
2. **Do not open the database.** There is no supported direct-SQLite fast path.
3. **Do not close from git.** Kata does not scan commits, branches, or pull requests. Closure is an explicit mutation with a reason, a substantive message, and typed evidence. Legacy numeric refs (`#12`, `12`) do not resolve. Use short ids or full ULIDs.
4. **Do not `kata delete` or `kata purge` without explicit user authorization.** Soft-delete and purge are a ladder, not a cleanup reflex.
5. **Do not put hooks, tokens, or server behavior in `.kata.toml`.**
6. **Do not route a bearer token to the wrong origin.** The local daemon token never goes to a hub. A catalog admin token never goes to a different hub origin. `--hub-token` is the deliberate cross-origin path. Browser sessions need both the HttpOnly instance cookie and `X-Kata-Web-Session`. Host validation is the DNS-rebinding boundary. Do not weaken one client class to make another work.
7. **Do not treat a joined hub as hostile.** Federation is mutual trust. Fix credential misrouting and stranded teardown state. Do not add ceremony against a hub the spoke already joined.
8. **Do not store raw API tokens in issue-sync config.** GitHub credentials resolve in the daemon (`[[github_sync.app]]`, host-bound env, or local `gh auth token`). Local edits to GitHub-owned fields can be overwritten on the next sync. Local comments stay local.
9. **Do not add `[connector]` (or any unknown) keys to daemon config.** Connector administration is not implemented.
10. **Do not change production schema** (migrations, bootstrap DDL, schema-version constants, tables, columns, indexes) without explicit consent in the current conversation. Ephemeral test DDL is the exception.
11. **Do not assume MCP tools are preloaded.** Call the section loader (`kata.load_issue_discovery`, `kata.load_issue_mutation`, and the other eleven) before the typed tools.
12. **Do not mark anonymous `--insecure-readonly` sessions writable.** That mode serves the shell and read snapshots only.
13. **Do not publish this harvest** by adding it to `docs/zensical.toml` unless a docs owner asks. Leave `repo_url` pointed at the documented upstream.
14. **Do not land a banned import.** `make import-bans` fails closed. Browser and `@kenn-io/kata-ui` sources stay on the declared UI allowlist and the credentialed API client. `pkg/` does not import privileged daemon internals except the two allowlisted edges below.

## CI import bans (AEGI-178)

Dune doctrine for this gate is skill `dune-electron-doctrine` and vault note `Architecture/fleet/DUNE-ELECTRON-VAULT-DOCTRINE-20260927.md` §3 (CI import-graph checks, FAIL-closed). Kata is the Open Engine / Kata lifecycle ledger, not an Electron app. The same banned-edge intent is applied to the planes this tree actually has.

| Plane | Paths | Rule |
| --- | --- | --- |
| Browser | `web/src` except tests and `web/src/lib/dev-environment.ts` | No Node builtins, no `process` / `process.env` / `import.meta.env`, no `dotenv`, no relative import that leaves `web/src`, no host module, no package outside the UI allowlist |
| Shared UI | `packages/kata-ui/src` | Same host bans, narrower package allowlist (`svelte`, `@kenn-io/kit-ui`) |
| Credentialed API | `openapi-fetch` only from `web/src/lib/api/client.ts` | Anywhere else is `browser-uncredentialed-api-client` |
| Public Go | `pkg/**` | No `go.kenn.io/kata/internal/...` except `internal/client` and `internal/connector/identityaudit` |
| Assignment | browser and all Go | No Linear, Jira, Asana, Trello, or Multica client. Multica is the one assignment plane and it is not embedded here |

Run it locally:

```sh
make import-bans
```

The Go CI job runs that target, and `go test ./...` runs `internal/importban` too. A parse failure or an unresolved relative import fails the command; the gate does not skip the file. A banned edge looks like:

```text
web/src/main.ts:1 [browser-node-builtin] banned import "node:fs" on the browser plane (privileged host module)
pkg/client/shortcut.go:3 [public-pkg-privileged-internal] banned import "go.kenn.io/kata/internal/daemon" from public package pkg/client/shortcut.go; use the declared HTTP API client
```

Remaining edges that are allowlisted instead of rewritten:

- `pkg/client` imports `go.kenn.io/kata/internal/client`, the declared HTTP transport.
- `pkg/connector/conformance` imports `go.kenn.io/kata/internal/connector/identityaudit`.
- `web/src/lib/dev-environment.ts` is host-only code that still lives under `web/src`. Browser files that import it fail with `browser-host-module`.

Host packages (`cmd/kata`, `internal/daemon`, `internal/db`, `internal/tui`) are the privileged plane and may import the daemon and database. A new third-party UI package is denied until it is added to the allowlist in `internal/importban`.

## Fleet trust guidelines

Vault and skill source of truth is the `fleet-information-unification` skill and `Architecture/fleet/FLEET-INFORMATION-UNIFICATION-DOCTRINE-20260927.md` (Fleet information unification, Multica AEGI-164 / AEGI-186). This file only points at it. Kata still closes ledger work; it does not store the fleet map.

Fleet SoT map: Multica assigns · Open Engine protocol · Kata closes · Hermes executes · Ringer receipts · prover proves · Vault narrative · Dune constraints.

1. Prefer Cursor cloud for draft/review.
2. Trust rotation: Claude Code → ChatGPT/Codex → Gemini (Antigravity) → Kimi.
3. Soft ship: draft → review → Multica stamp → apply (profile-mutate way-of-work).
4. CoS may soft-ship after stamp. Jack-gated: `--replace`, secrets, spend, and irreversible changes.

## Read order when context is tight

1. `AGENTS.md` (session contract; `CLAUDE.md` is the same file).
2. `docs/workflows/agents.md` (durable notes, claim, close).
3. `docs/guide/concepts.md` (identity, relationships, actor).
4. `docs/design/architecture.md` (invariants and trust boundaries).
5. The single operations page for the surface you are touching (federation, GitHub sync, hosted mode, Postgres, remote daemon).
6. `docs/agent-memory/BRANCHES.md` before reviving any remote branch.
7. `docs/agent-memory/INDEX.json` for entrypoints and risks.
