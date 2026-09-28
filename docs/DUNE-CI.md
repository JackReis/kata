# Dune import gates

This file is the mechanical form of the Dune multi-provider doctrine for this product repo. It is intentionally absent from `docs/zensical.toml`, so it is not part of the public docs site.

The checker is `tools/duneimport`. The seat-specific rules are `tools/duneimport/policy.json`. CI runs the same command as a developer laptop. A red result is a hard stop.

## Run locally

```bash
make dune-import
go test ./tools/duneimport
```

`make dune-import` builds the checker and runs that binary. Optional flags, passed through a direct binary invocation, are `-root` (default `.`) and `-policy` (default `<root>/tools/duneimport/policy.json`).

`go run ./tools/duneimport` compiles the same program, then prints `exit status N` and exits 1 for every non-zero N. Use `make dune-import` or the built binary when the difference between exit 1 and exit 2 matters.

The pre-commit hook `dune-import` in `prek.toml` runs the same command.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Every scanned production file satisfies the policy. stderr is empty. |
| 1 | At least one banned edge. stderr names the file, line, import, rule id, and reason. |
| 2 | The gate is unsound. Do not ship. stderr starts with `FAIL-closed`. |

Exit 2 covers a missing policy, `fail_closed` not set to true, empty `denies`, an unknown JSON field, a scope root that scans no production files, a `from` or `except_from` prefix that matches no scanned file, a Go parse error, or a missing `go.mod` when a Go scope is configured. A typo in a package prefix fails the build instead of silently protecting nothing.

`*_test.go` files are not product edges. Tests may boot a daemon and a store. A banned import in a non-test file fails the gate. TypeScript tests that live under `web/src` or `packages/kata-ui/src` are scanned, because those trees ship in the UI.

## What is banned

Patterns match a full import path or that path followed by `/`. A pattern that ends in `:` is a string prefix, so `node:` matches `node:fs` and does not match `./fs`. `go.kenn.io/kata/internal/db` matches `go.kenn.io/kata/internal/db/sqlitestore` and does not match a sibling such as `internal/dbtest`.

### Feature colocation

These production packages must not import each other, except for the allowed bridges listed below:

- `internal/githubsync`
- `internal/federation`
- `internal/embedding`
- `internal/mcp`
- `internal/tui`
- `internal/hooks`

`internal/embedding` also must not import `internal/db` or `internal/daemon`. It is a provider HTTP client.

`internal/db` (the store interface and both engines) must not import feature, daemon, or client packages. `internal/db/sqlitestore` and `internal/db/pgstore` must not import each other. `internal/db/storeopen` is the only store package that sees both engines.

### Process and trust boundaries

Unprivileged clients must not reach privileged packages:

- `internal/tui` must not import the daemon, the store, GitHub sync, embeddings, MCP, hooks, or storage admin.
- `internal/mcp` must not import the daemon, the store, GitHub sync, federation, embeddings, the TUI, or hooks.
- `internal/client` must not import stores or feature packages.
- `pkg/client` must not import the daemon, stores, feature packages, or `internal/connector`.
- `pkg/connector` must not import the daemon, stores, feature packages, or `internal/client`.
- `web/src` and `packages/kata-ui/src` must not import Node side-effect modules (`node:*`, `fs`, `child_process`, `net`, `http`, `https`, `os`, `vm`, `worker_threads`). Dev scripts and Playwright tests live outside those trees and may use Node.

### Dependency allowlists

Opening a database from a new package fails the gate. The only production importers are:

| Target | Allowed importers |
| --- | --- |
| `internal/db/sqlitestore` | `internal/db/storeopen`, `internal/jsonl`, `internal/testenv` |
| `internal/db/pgstore` | module root (`service.go`), `cmd/kata`, `internal/storageadmin`, `internal/db/storeopen`, `internal/db/pgstore` |
| `internal/db/storeopen` | module root, `cmd/kata`, `internal/storageadmin` |
| SQLite drivers and `go.kenn.io/kit/vector/sqlitevec` | `internal/db/sqlitestore`, `internal/vector` |
| `github.com/jackc/pgx` | `internal/db/pgstore`, `internal/config`, `internal/testenv` |

`cmd/kata` and the module-root service are composition roots. They may wire features. Feature packages may not reach back into those roots except where a rule says so.

### Allowed bridges

These edges are product code today. The policy names them so they stay explicit:

- `internal/federation` may import `internal/daemon` (the replica worker runs in the daemon process).
- `internal/tui` may import `internal/federation` (the federation view).
- `internal/mcp` may import `internal/storageadmin` (the in-process storage admin seam).
- `internal/client` may import `internal/daemon` (daemon discovery).
- `pkg/client` may import `internal/client` (transport). It may not import the daemon or a store directly.
- `pkg/connector/conformance` may import `internal/connector/identityaudit`.
- Hooks and several features may import the `internal/db` interface. They may not import a concrete engine.

There is no god-file size lint in this repo. The shortcut ban is the driver and store allowlists plus the browser Node ban.

## Copy recipe

Use this checker in other product seats. Do not write a second gate.

1. Copy `tools/duneimport` (`main.go`, `main_test.go`). Leave the checker seat-agnostic.
2. Replace `policy.json`. Keep `"version": 1` and `"fail_closed": true`. Point `from` / `to` at that repo's real packages. Map the same six rules onto its layout: feature colocation, privileged versus unprivileged, layer allowlists, shortcut bans where an import edge exists, one shared checker, and exit 2 when the gate is missing.
3. Add `make dune-import` and `.github/workflows/dune-import-gates.yml` so pull requests build `./tools/duneimport` and run that binary with `contents: read`. Run the binary, not `go run`: this toolchain's `go run` collapses every non-zero status to 1.
4. Prove it: `go test ./tools/duneimport` is green on the current tree, and a temporary non-test file with a banned import exits 1.
5. If the policy file, the workflow, or a `from` prefix is missing, that is exit 2. Do not ship.

Seats that should consume this pattern: Open Engine, this product repo, and the Grok Bot Electron tree use the same checker. Copy it later to ringer, hermes-agent, multica-jr, and prover by replacing only `policy.json` and the workflow checkout. No Ringer `which_host` receipt was cited for this change, so the gate does not pin a host.
