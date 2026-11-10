<!-- source-branch: main -->
<!-- work-branch: omp/fix-v1-0-1-request-release-dashboard -->

# fix-v1-0-1-request-release-dashboard

## Context

Release `v1.0.1` from repository `momodding/cpa-model-combo` with five user-visible corrections: logical routed request parents, versioned native artifacts, non-overlapping management toggles, visible inherited fallback statuses, and identity shown as display name `Model Combo` with technical plugin ID `model-combo`. Repository/module name and default database remain `cpa-model-combo` / `cpa-model-combo.db`; runtime/config/management identity cleanly cuts over to `model-combo` with no alias.

Preserve ABI exports, functional router terminology, MIT attribution, callback-level overview accounting, five logical request rows for the six-callback fallback smoke, and standalone direct calls.

## Confirmed constraints

- Start from current `marketplace/main`; PR #2 is merged and `AGENTS.md`, ADR 0001/0002, and BDR 0001/0002 bind execution.
- Create dedicated branch/worktree before any repository edit. Project governance overrides intermediate commits: no commit, push, or PR until every required check passes.
- Runtime entries are exactly `model-combo-v1.0.1.so`, `model-combo-v1.0.1.dylib`, and `model-combo-v1.0.1.dll`. Archives are renamed to `model-combo_1.0.1_<goos>_<goarch>.zip`.
- CPA loader derives ID `model-combo` from `model-combo-v1.0.1.<ext>` using its supported `-v` delimiter. Ship one versioned entry only; no `model-combo.<ext>` or `cpa-model-combo.<ext>` compatibility copy.
- Existing installs must rename config key `plugins.configs.cpa-model-combo` to `plugins.configs.model-combo`. Keep `cpa-model-combo.db` so identity cutover does not discard usage history.
- Backend fallback defaults remain `401, 402, 403, 404, 408, 429, 500, 502, 503, 504`; omitted and empty lists inherit them.
- No ABI rename, database migration, retry-policy expansion, new UI framework, or dependency.

## Approach

### 1. Isolate work and bind decisions

- Fetch the independent repository, then create sibling worktree `../cpa-model-combo-v1.0.1` on `omp/fix-v1-0-1-request-release-dashboard` from current `marketplace/main` (smallest unused numeric suffix if occupied). Leave current target branch and source-fork worktree untouched.
- Copy this approved plan to `plans/fix-v1-0-1-request-release-dashboard.md` with required source/work branch comments.
- Before product edits, add `docs/adr/0003-v1-0-1-identity-packaging-and-request-contract.md` and `docs/bdr/0003-v1-0-1-visible-behavior.md`. Record all binding behavior: `Model Combo` display name, `model-combo` technical ID/config key/management path, repository and DB names retained, no identifier alias, `model-combo-v<version>` sole runtime entry, renamed `model-combo_<version>_*` archives, logical routed parent identity, shared toggle layout, and visible inherited fallback defaults. If current main already contains a conflicting ADR/BDR, stop before product edits.

### 2. Cut runtime and management identity to `model-combo` / `Model Combo`

- In `main.go`, set `pluginID = "model-combo"` and `pluginName = "Model Combo"`; keep GitHub repository URL and `defaultDataFileName = "cpa-model-combo.db"`. `Identifier()`, executor identifier RPC, metadata registration, and Go management paths continue deriving from these constants.
- In `management.go`, set resource `Menu` to `pluginName` and rewrite user-facing descriptions to `Model Combo`; generated route paths become `/plugins/model-combo/...` through `pluginID`.
- In `dashboard/shared.js`, change `CONFIG_API`, `VALIDATE_API`, and `USAGE_API` to `/v0/management/plugins/model-combo/...`. In `dashboard/configuration/tab.js`, treat `owner === 'model-combo'` as the plugin-owned model. Keep existing browser storage keys so users do not lose saved management credentials; those keys are private persistence namespaces, not displayed identity.
- In `dashboard.html`, set document title to `Model Combo`; existing `<h1>Model Combo</h1>` remains.
- Update `abi_test.go`, `management_test.go`, dashboard assembly assertions, and integration menu lookup for exact display name, ID, routes, and title. Update technical/user-facing errors that currently claim plugin identity `cpa-model-combo` to `model-combo` or `Model Combo` as appropriate.
- Update active install/config/management docs (`README.md`, `docs/usage-tracking.md`, and current `docs/v2` runbooks) to use config key and endpoints `model-combo`, while retaining repository/module URLs, repository heading, database filename, attribution, and historical plan/audit records. Explicitly document clean config-key migration; do not support both keys.

### 3. Repair logical request-parent metadata at aggregation boundary

- In `usage_query.go:aggregateRequestAttempts`, keep grouping by non-empty `router_request_id` and stable ordering by `router_attempt`, then sequence. Build parent request identity deliberately: `Attribution = "routed"`, `RouterModel` equals first non-empty routed model in ordered attempts, and `RouterRequestID` equals group ID. Preserve earliest request time/sequence, final outcome behavior, and aggregate token/latency/TTFT/cache/cost calculations. Provider attempt records remain unchanged and ordered in `attempts`.
- Do not derive parent identity in dashboard JavaScript; API response is authoritative.
- Extend `usage_store_test.go` with a fallback group whose last provider attempt lacks router metadata but earlier correlated attempt has it. Assert one routed parent with `router_model`, request ID, ordered failed/success children, current aggregate metrics, and no extra top-level row. Retain assertions that direct calls remain standalone and truly unattributed callbacks remain unattributed.

### 4. Version and rename release artifacts

- Set build default version to `1.0.1` and runtime `PLUGIN_ID` to `model-combo` in `Makefile`; update `.gitignore` and CI native-output paths from old runtime basename.
- In `.github/xgo/release.sh`, emit sole ZIP entry `${plugin_id}-v${version}.${extension}` and archive `${plugin_id}_${version}_${goos}_${goarch}.zip`. Keep raw xgo platform output handling and ABI exports unchanged.
- In `.github/workflows/release.yml`, pass `model-combo`, expect all five `model-combo_1.0.1_*` archive patterns, and assert each archive contains exactly its platform's `model-combo-v1.0.1.<ext>`. Explicitly fail if a stable or old-ID library is also present.
- Build CPA integration artifacts as `model-combo-v0.0.0-test.<ext>` while configuration uses `plugins.configs.model-combo`; this proves loader version parsing preserves ID. Update `plugin_path_test.go` to resolve default `cpa-model-combo.db` from versioned `model-combo` paths.
- Update README install and rollback instructions with exact `v1.0.1` names: rollback installs/selects a prior versioned `model-combo-v<version>` artifact; no stable alias exists.

### 5. Fix all management toggle layouts

- In `dashboard/configuration/panel.html`, use one structure for all three toggles: wrapping label, fixed visual `.toggle` containing checkbox plus empty track span, and text/help outside that fixed box. Convert `fallback-enabled` and `fallback-stream-before-payload`; preserve element IDs, `data-fallback-field`, checked/disabled behavior, and accessible label association.
- In `dashboard/configuration/styles.css`, scope track selectors to the empty visual track rather than every nested span. Let text/help wrap beside the 42×24 switch at narrow widths; preserve keyboard focus, checked, disabled, and touch behavior. Do not add per-toggle offsets.

### 6. Show inherited fallback statuses

- In `dashboard/shared.js`, define one display constant containing `401, 402, 403, 404, 408, 429, 500, 502, 503, 504` and use it for initial fallback state.
- In `dashboard/configuration/routes.js:fallbackDraftFromConfig`, use that constant when `fallback_on_status` is missing, non-array, or empty; preserve explicit non-empty arrays as comma-separated values. Leave exclusions, retry lists, booleans, and max attempts unchanged. This mirrors existing backend omitted/empty semantics; it does not create new policy.

### 7. Verify, then deliver through PR

- Run focused Go tests for metadata/ABI identity, management registration/routes, dashboard assembly, logical request aggregation/grouping, fallback config, versioned plugin path, and integration fixtures.
- Run minimum-supported CPA integration with `model-combo-v0.0.0-test.<ext>`. Observable proof: CPA lists ID `model-combo`, metadata/menu show `Model Combo`, management/config/usage endpoints use `model-combo`, plugin reads config from `plugins.configs.model-combo`, and default DB remains `cpa-model-combo.db`.
- Serve assembled management page and use real browser at desktop and narrow/mobile widths. Verify navbar/browser title `Model Combo`; all three toggles in checked, unchecked, focused, and disabled states without text overlap; omitted/empty fallback response displays canonical defaults; explicit non-empty list remains unchanged; save snapshot produces backend-accepted values.
- Run `bash -n .github/xgo/*.sh`, then pinned xgo packaging for all five targets. For each archive, `unzip -Z1` must return exactly one expected `model-combo-v1.0.1.<ext>`; checksums must cover exactly five renamed archives.
- Run `make check` (`go test ./...`, `go test -race ./...`, `go vet ./...`) and repository formatting checks. Smoke-load built native library through supported CPA and issue one routed fallback request; assert six overview callbacks, five logical details, routed parent identity, and ordered children.
- Inspect final diff: no old ID in active config, runtime, management, dashboard API, or release paths; remaining `cpa-model-combo` occurrences are only repository/module URLs, repository title, DB/storage namespace, attribution/history, or explicit migration-from text. Preserve `LICENSE`; do not commit release output or browser fixtures.
- Only after every check passes, create one complete commit, push work branch to `marketplace`, and open PR to `main` with exact evidence. Require PR checks; do not merge or push to source-fork `origin` without separate request.

## Critical files & anchors

- `main.go` — `pluginID`, `pluginName`, metadata, and `Identifier()` define CPA-visible identity.
- `management.go` — registration resource menu and all management route prefixes.
- `dashboard/shared.js` — hard-coded browser API roots plus fallback initial state.
- `usage_query.go` — `aggregateRequestAttempts` owns logical parent identity.
- `.github/xgo/release.sh` — sole runtime entry and archive naming.

## Verification

- Identity scenario: load `model-combo-v0.0.0-test.so` (platform extension substituted) with only `plugins.configs.model-combo`; expect plugin ID `model-combo`, metadata/menu/title `Model Combo`, working `/v0/management/plugins/model-combo/...`, and `plugins/cpa-model-combo.db`.
- Request scenario: one routed request with attempt 0 failing 429 and attempt 1 succeeding, where final attempt lacks copied router metadata; expect one parent labeled router alias, two ordered children, six callback overview/five logical integration totals.
- UI scenario: omitted and `[]` fallback statuses both render `401, 402, 403, 404, 408, 429, 500, 502, 503, 504`; explicit `[429, 503]` renders `429, 503`; no toggle text intersects switch at tested desktop/mobile widths.
- Package scenario: each `model-combo_1.0.1_<platform>.zip` has one `model-combo-v1.0.1.<ext>` entry and no other file.

## Assumptions & contingencies

- Repository, Go module, GitHub URLs, and database filename intentionally remain `cpa-model-combo`; runtime/config ID, release archives, management paths, and visible name change. If CPA current main no longer accepts `-v` version syntax, stop before packaging because no compatibility filename is allowed.
- Existing users must rename config key; no alias or automatic migration. Preserve existing DB by leaving default filename unchanged.
- Required package or supported-toolchain smoke failure blocks commit/push/PR. Local Go 1.27 c-shared failure is not waived; rerun with repository-supported toolchain and proceed only if that smoke passes.

## Acceptance criteria

- CPA/plugin management identifier is `model-combo`; plugin-management name, management navbar, dashboard title, and dashboard heading are `Model Combo`.
- Active config key and management endpoints use `model-combo`; repository/module and `cpa-model-combo.db` remain unchanged and documented.
- Routed fallback/retry has one non-unattributed logical parent and ordered attempt children; callback and logical counts retain ADR/BDR 0001 behavior.
- Five `model-combo_1.0.1_<platform>.zip` files each contain exactly one versioned `model-combo-v1.0.1.<ext>` and no alias.
- All management toggles remain readable and accessible across verified states/viewports.
- Omitted/empty fallback statuses visibly show canonical defaults; explicit lists remain unchanged.
- Every required check passes before commit, push, and PR; PR is sole route to `main`.
