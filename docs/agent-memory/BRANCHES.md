# Branch inventory

Captured 2026-09-27 from `git branch -a` and `git merge-base` / `git rev-list` against local `main` (`b37aa01`, 2026-08-24). Twelve unique names. No other remotes are configured. Ahead/behind are commits reachable only from that side. "Files touched" is `git diff --name-only main...<branch>` (empty when there is no merge base).

Calendar staleness: every non-`docs-assets` tip is at least five weeks before this harvest. `main` is still the line to build on. Nothing below is an open pull on this origin (`gh pr list` returned only merged `#1` and `#2`).

| Branch | Tip date | Ahead / behind `main` | Merge base | Purpose from commits | Flag |
| --- | --- | --- | --- | --- | --- |
| `main` | 2026-08-24 | 0 / 0 | itself | Current line. Tip raises `DefaultHTTPTimeout` 5s → 30s. 591 commits. | **Active tip** |
| `dev` | 2026-08-21 | 3 / 65 | `9522e82` 2026-07-26 Keep remote TUI project resolution path-free (#205) | Three commits whose subjects match `main`'s latest beads-import and timeout commits, but SHAs differ (`fa3a6aa` vs `b37aa01`). Trees are not equal. Unique diff is 4 paths. | **Divergent duplicate.** Do not merge. Not a fast-forward of `main`. |
| `fix/beads-import-aegis-20260810` | 2026-08-10 | 1 / 65 | same `9522e82` | One commit, `fix(kata): beads_import.go`. Diff against `main` removes assignee fallback, `--include-memories`, strict link mapping, and warning collection that `main` already has (`ad85e09`, `#2`). | **Dead end.** Replays an older importer over the landed one. |
| `docs-assets` | 2026-07-12 | unrelated | none (`git merge-base` fails; parent list empty) | Single orphan commit `33abc77` `docs assets` by `kata docs bot`. | **Generated storage, not a feature branch.** `AGENTS.md` says this ref is mutable docs-asset storage. Do not merge it to repair "591 behind". |
| `wesm/branch-orchestration-fixes` | 2026-07-07 | 25 / 116 | `cac5fdd` 2026-07-03 Add testing-without-tautologies skill (#149) | Wait timeouts, empty `If-Match` rejection, agent-mode wait/meta output, shared JSON canonicalizer. 42 paths not in `main`. | **Unmerged follow-up, not proven dead.** Rebase cost is high (116 behind). Read `docs/operations/agent-orchestration.md` before reviving. |
| `wesm/branch-orchestration-spec` | 2026-07-03 | 1 / 116 | same `cac5fdd` | One commit: "Add branch-orchestration primitives design sketch". 1 path. | **Spec sketch.** Prefer the shipped orchestration doc over this branch. |
| `pi-tasks-kata-plugin` | 2026-06-12 | 20 / 152 | `3ac7870` 2026-06-11 federation spoke leave/rejoin (#109) | Adds `plugins/pi-tasks-kata/` (package, tests, README) and then a chain of mutation-order, claim, and redaction fixes. 15 paths. Not present on `main`. | **Parked plugin.** Dead relative to `main` unless someone explicitly wants that plugin. |
| `windows-port` | 2026-05-31 | 6 / 180 | `3cb8d51` 2026-05-31 Adopt existing projects (#71) | "port the daemon to Windows", CI `windows-latest`, shared stop signaling. 30 paths. `main` already has `cmd/kata/*_windows.go` and `daemon_signaling_windows.go`. | **Landed elsewhere.** Do not replay. |
| `feat/issue-58` | 2026-05-28 | 22 / 189 | `9e390ca` 2026-05-28 token identity (#65) | Trusted-proxy actor header tests and config validation. `main` log contains `941c15a Trusted-proxy actor header (refs #58) (#66)` and `8ba62dc ... bearer+proxy test coverage (#67)`. 11 paths. | **Superseded.** Review loop already folded forward. |
| `feat/issue-59` | 2026-05-28 | 5 / 192 | `c5e9cc7` 2026-05-27 Consolidate agent guidance (#57) | Signed multi-arch image, drop GHCR for Artifact Registry via WIF, then roborev-ci review fixes. 5 paths. This tree's only Dockerfile is `docker/federation/Dockerfile`. | **Likely abandoned release experiment.** 192 behind; do not assume the WIF workflow exists on `main`. |
| `port-env-bind` | 2026-05-28 | 3 / 192 | same `c5e9cc7` | Bind `0.0.0.0:$PORT`, then extract hosted-mode docs. 8 paths. `main` has `docs/operations/hosted-mode.md`, `docs/design/hosted-mode.md`, and `e2e/port_env_test.go`. | **Landed elsewhere.** |
| `claude-hooks-design` | 2026-05-17 | 5 / 200 | `878b0d5` 2026-05-17 pin bearer token to origin | Five docs commits designing Claude Code hooks and short ids. 1 path. `main` later shipped `kata init --with-hooks`, `--with-codex-hooks`, and attention hooks (`8f4b42d`, `#191`, `#216`). | **Design draft after the feature shipped.** |

## Refactor loops

- **`dev` vs `main`.** Same three subjects (beads dependency import, `--include-memories`, HTTP timeout) exist as two histories after `9522e82`. Merging `dev` would not be a three-commit fast-forward; the trees already diverged across the 65 commits `main` has that `dev` lacks.
- **`feat/issue-58`.** The branch is a review stack (spec findings, integration tests, roborev-ci follow-ups) whose outcome is already described by `#66` and `#67` on `main`.
- **`feat/issue-59`.** Tip is "address roborev-ci review" on a container-publish change that did not become the tree's packaging story. Packaging docs live under `docs/development/packaging.md`.
- **`wesm/branch-orchestration-fixes`.** Long test-and-lint loop on `kata wait` (timeouts, pending refs, agent output). Some wait behavior may have landed by other commits; the 42-path diff says the branch is not contained in `main`. That is the one stale branch that might still hold unique fixes. Confirm with a file-level diff before copying patches.

## Dead ends (do not revive to "catch up")

- `fix/beads-import-aegis-20260810` — older than the importer on `main`.
- `windows-port`, `port-env-bind`, `claude-hooks-design` — corresponding behavior or docs exist on `main`.
- `pi-tasks-kata-plugin` — plugin tree never merged; 152 commits behind.
- `docs-assets` — orphan generated ref, no common history with `main`.
- `wesm/branch-orchestration-spec` — one design sketch after orchestration docs existed as a direction (the fixes branch and `docs/operations/agent-orchestration.md` are the later artifacts).

No branch was deleted, force-pushed, or merged for this inventory.
