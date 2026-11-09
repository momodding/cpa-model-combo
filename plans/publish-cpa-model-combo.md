<!-- source-branch: omp/fix-management-plugin-page -->
<!-- work-branch: omp/publish-cpa-model-combo -->
# publish-cpa-model-combo

## Context

Current repository has mixed identities: runtime ID/artifacts/config use `model-combo`, default storage and release workflow still use `model-router`, public metadata points at `markhuangai/cpa-plugin-model-combo`, and version is `0.4.2`. User chose a clean cutover to:

- plugin ID and exact display name: `cpa-model-combo`
- publisher: `momodding`
- canonical repository: `https://github.com/momodding/cpa-model-combo`
- first marketplace release: `v0.1.0`
- default database: `cpa-model-combo.db`
- no compatibility aliases or duplicate old artifact names

Plugin is a fork of `https://github.com/markhuangai/cpa-plugin-model-router`; README and retained MIT notice must make that lineage explicit.

Official marketplace is `https://github.com/router-for-me/CLIProxyAPI-Plugins-Store`. Its registry requires `id`, `name`, `description`, `author`, and exact GitHub `repository`; releases require `v<version>`, five platform ZIPs named `<id>_<version>_<goos>_<goarch>.zip`, root-level `<id>.(so|dylib|dll)`, and `checksums.txt` in sha256sum format.

Execution starts from clean current HEAD `f70dbb5` on `omp/fix-management-plugin-page`, which includes prior routing/dashboard work. Existing sibling worktree uses another branch and is not touched.

## Approach

1. **Create dedicated worktree and persist approved plan**
   - Explicit worktree request overrides normal in-place branch checkout: from the current HEAD, create branch `omp/publish-cpa-model-combo` at sibling path `/Users/momodding/Documents/cpa-model-combo-marketplace` with `git worktree add -b`.
   - Immediately copy this approved plan into that worktree as `plans/publish-cpa-model-combo.md`, recording source/work branch comments there. Run all edits, checks, and todo commits from the new worktree.
   - Keep original worktree clean and leave the new worktree present after completion. At completion, merge the work branch into the recorded source branch from the original worktree; do not delete worktree/branch or push.

2. **Apply one clean runtime and package identity**
   - In `main.go`, set `pluginID`, exact `pluginName`, `pluginVersion`, author, repository URL, and default-path description to the chosen identity/version.
   - Change module path in `go.mod` to `github.com/momodding/cpa-model-combo`; no internal import migration is expected because package code does not import its own module.
   - Change default database basename in `plugin_path.go` to `cpa-model-combo.db` and update path tests. This is intentionally a clean cutover: old `model-router.db` is not auto-migrated or copied; users retaining history must set `data_path` explicitly.
   - Replace identity-bearing runtime text and fixed management paths in `abi.go`, `config.go`, `dashboard/shared.js`, and `dashboard/configuration/tab.js`. Keep functional router terms (`modelRouterPlugin`, route APIs, router model fields) because they describe behavior, not stale product identity.
   - Update public-identity, dashboard, plugin-path, and integration assertions in `abi_test.go`, `dashboard_test.go`, `plugin_path_test.go`, `integration_test.go`, plus any directly failing identity fixtures found by the final exact-string scan. Do not rewrite incidental response IDs that are not user-facing identity contracts.

3. **Make release output marketplace-compatible at `0.1.0`**
   - Set `PLUGIN_ID := cpa-model-combo` and `VERSION ?= 0.1.0` in `Makefile`.
   - Update `.github/workflows/test.yml` native smoke output to `cpa-model-combo.so`.
   - Update `.github/workflows/release.yml` build inputs and asset validation from the stale module/ID to `github.com/momodding/cpa-model-combo` and `cpa-model-combo`; validate exactly five `0.1.0` archives whose sole library entry matches the runtime ID.
   - Reuse generic `.github/xgo/release.sh`; change it only if a renamed-value assertion is hard-coded there. Do not commit existing `dist/` outputs or hand-edit generated archives/checksums.

4. **Rewrite public documentation and fork attribution**
   - Rewrite `README.md` title, badges, repository links, configuration key, library names, data-path examples, and version-sensitive commands for `cpa-model-combo`.
   - Add a concise fork/provenance section stating this project is forked from `https://github.com/markhuangai/cpa-plugin-model-router`, linking upstream and preserving its MIT attribution. Keep current feature description; remove wording that falsely presents this repository as the original standalone plugin.
   - Preserve the upstream copyright and MIT terms in `LICENSE`; do not replace Mark Huang's notice with publisher metadata.

5. **Prepare and validate marketplace handoff**
   - Build a temporary candidate entry against the latest official `registry.json`, appending it in PR order without re-sorting existing plugins:
     - `id`: `cpa-model-combo`
     - `name`: `cpa-model-combo`
     - `description`: `Routes logical model aliases across priority and weighted round-robin target pools with cooldown, failover, and usage tracking.`
     - `author`: `momodding`
     - `version`: `0.1.0`
     - `repository` and `homepage`: `https://github.com/momodding/cpa-model-combo`
     - `license`: `MIT`
     - `tags`: `["Router"]`
     - omit optional logo/locales until real assets/translations exist
   - Validate JSON shape, unique ID, ID syntax, repository format, version format, and correspondence between registry ID and release filenames/library basenames. Keep candidate temporary rather than adding a dead manifest file that marketplace does not consume.
   - Final handoff must include exact registry JSON entry and manual external sequence: rename/create GitHub repository, update shared `origin` only after rename, push merged commits, create/push tag `v0.1.0`, let release workflow publish assets, verify latest release/checksums, then open an official-store PR changing only `registry.json` with release evidence. No push, GitHub release, or marketplace PR is performed by this execution.

## Critical files & anchors

- `main.go:10-15, 67-75` — runtime ID/name/version and public metadata.
- `go.mod:1` — canonical module path.
- `plugin_path.go:9` and `plugin_path_test.go` — clean-cutover default database.
- `abi.go:257`, `config.go:122,174` — user-visible identity errors.
- `dashboard/shared.js:2-6`, `dashboard/configuration/tab.js:113` — management API routes and model-owner filtering.
- `abi_test.go:13-33,58`, `dashboard_test.go:20-22`, `integration_test.go:104-132` — identity and end-to-end loading contracts.
- `Makefile:1-2`, `.github/workflows/test.yml:38-42`, `.github/workflows/release.yml:67-77,101-126` — build/release names and version.
- `.github/xgo/release.sh:9-12,80-147` — generic cross-build/package behavior to reuse.
- `README.md:1-7,39-76` — public identity, configuration, database path, and provenance.
- `LICENSE:1-13` — upstream notice that must remain.

## Verification

1. Exact-string scan over tracked source/docs/workflows: no stale `markhuangai/cpa-plugin-model-combo`, product-level `model-combo`, release-level `model-router`, or `0.4.2`; allow functional router terminology only after reviewing each hit.
2. `bash -n .github/xgo/*.sh` and workflow/YAML parse check.
3. `make check` (`gofmt` check, unit tests, race tests, vet).
4. `CPA_SOURCE="$PWD/.cpa-source" go test -tags=integration ./... -count=1` to build/load `cpa-model-combo` in CPA and exercise routed non-streaming/streaming behavior plus management APIs.
5. Run dashboard E2E command `npm run test:e2e` if its browser prerequisite is available; otherwise report that visual/browser path as unchecked rather than installing unrelated tooling.
6. Run `.github/xgo/release.sh` with the pinned workflow image, module `github.com/momodding/cpa-model-combo`, ID `cpa-model-combo`, version `0.1.0`; inspect all five ZIP entry lists and run `sha256sum -c dist/checksums.txt`.
7. Merge candidate marketplace entry into a freshly fetched official `registry.json` in a temporary location; parse it and assert schema version, required fields, unique IDs, formats, and release-name correspondence. Remove temporary registry and release scaffolding after proof; generated ignored `dist/` outputs may remain uncommitted only if already ignored.

## Assumptions & contingencies

- `https://github.com/momodding/cpa-model-combo` may not exist until manual handoff. Local metadata intentionally targets it now; live repository/release checks occur only after user renames or creates it.
- Full ID rename breaks existing `plugins.configs.model-combo` and automatic discovery of `model-combo` libraries. This is accepted clean-cutover behavior. README calls out new key/library and explicit `data_path` escape hatch for old history; no shim or dual registration.
- Worktree destination is currently unused. If it becomes occupied before execution, stop and report instead of silently choosing another path.
- Docker/xgo is required for full five-platform release proof. If unavailable after code/unit/integration verification, finish reachable work and report release packaging as the exact external blocker; do not claim marketplace readiness.
- External mutations are intentionally manual under the accepted handoff boundary: repository rename/create, remote update, push/tag, GitHub Release publication, and marketplace PR.
