# Final Implementation Audit: Account-Aware Routing

Scope: account-aware routing plan only.

Method: static reading of repository files and the untracked contract `docs/v2/05-account-routing-contract.md`. No code changed. No build, test, commit, or push run. No credential, auth data, or `host.auth.get` access.

## 1. Verdict

- Route model baseline exists: ordered model routes (`routes`) with targets, strategies, cooldown, and last-known-good policy. Account-unaware.
- Account-pinning contract is BLOCKED (`docs/v2/05-account-routing-contract.md` §2, overall line 35).
- Therefore account-pool and account-restriction requirements are BLOCKED and NOT implemented.
- Prior survey found no "combo" concept in source, config, docs, or dashboard; not re-run in this audit. Canonical term is `routes` / `targets`.

## 2. Route Model Baseline (observed)

| Item | Location | Observation |
|---|---|---|
| Strategy constants | `config.go:15-25` | `routeStrategyPriority = "priority"`, `routeStrategyRoundRobin = "round-robin"`, `routeStrategyRandom = "random"`, plus `last_known_good` (per prior survey). |
| Default LKG policy | `config.go:27-29` | `SuccessRateThreshold 0.9`, `MinimumSamples 20`, `ObservationWindowMinutes 60`. |
| Target type | `config.go:40-43` | `modelTarget{Model, Weight}`. No account field. |
| Route type | `config.go:51-57` | `modelRoute{Alias, Strategy, CooldownSeconds, ...}`. No account field. |
| YAML target | `config.go:59-62` | `modelTargetYAML{model, weight}`. |
| YAML route | `config.go:64-70` | `modelRouteYAML`: `alias`, `strategy`, `cooldown_seconds`, legacy `cooldown-seconds`, `success_rate_threshold`, `minimum_samples`. |
| Plugin config | `config.go:76-87` | `routerConfig{Enabled, DataPath, RetentionDays, ...}`; `fallbackPolicy()` at `:88`. |
| Canonical key | `config.go` `routerConfigYAML`, `decodeRouterConfig` (per prior survey) | `routes` canonical; `model-routes` legacy. Both set is an error. |
| Docs | `README.md:127-158` | Route field table; "Migrating From Fork" section. |

Conclusion: observed route types (`modelTarget`, `modelRoute`, YAML variants) have no account, auth ID, or auth index field (per prior survey; `config.go` not re-read in this audit).

## 3. Runtime Baseline (observed)

| Item | Location | Observation |
|---|---|---|
| Runtime struct | `runtime.go:12-17` | `routeRuntime{mu, now, rng, ...}`. |
| Per-route state | `runtime.go:24-30` | `routeState{signature, cursor, cooldowns map[string]time.Time, ...}`. Cooldown key source not confirmed; `MarkFailure` takes a `model` argument. |
| Selection | `runtime.go:113-120` | `Select` delegates to `SelectExcluding(route, excluded)`. |
| Failure marking | `runtime.go:216-234` | `MarkFailure(route, model)`. |
| Seeding | `runtime.go:46-49` | `runtimeSeeds` uses `crypto/rand`. |

Conclusion: cooldown and selection state are in memory. No account field observed in `routeRuntime`/`routeState` declarations (per prior survey; `runtime.go` not re-read in this audit).

## 4. Strict Pin Contract (BLOCKED)

Source: `docs/v2/05-account-routing-contract.md` (pinned CPA `v7.3.9`, commit `61fdfc34`, source root `.cpa-source/`, untracked).

| Contract | Status | Note |
|---|---|---|
| C1 `host.auth.list` lists accounts | VERIFIED | §3.1, §3.2 |
| C2 list entries carry `id`, `auth_index` | VERIFIED | §3.2 |
| C3 `auth_id` pin in `HostModelExecutionRequest` | VERIFIED | §3.3 |
| C4 sync and stream attach pin | VERIFIED (sync), PARTIAL (stream) | §3.4 |
| C5 `PinnedAuthMetadataKey` carries pin | VERIFIED | §3.4, §3.5 |
| C6 conductor parses pin | VERIFIED | §3.6 |
| C7 pick path honors pin (non-home) | BLOCKED | §4.1 |
| C8 home dispatch honors pin | PARTIAL | §4.2 |
| C9 pin matched to `auth.ID` or `auth.Index` | BLOCKED | §4.3 |
| C10 unknown pinned ID fails closed | BLOCKED | §4.4 |
| C11 disabled pinned account fails closed | BLOCKED | §4.4 |
| C12 cooling or unavailable pin fails closed | BLOCKED | §4.4 |
| C13 provider mismatch fails closed | BLOCKED | §4.4 |
| C14 no silent fallback when pin set | BLOCKED | §4.4 |
| C15 callback exposes only documented fields | PARTIAL | §5 |

Consequences:
- Pin can be sent (C3-C6 verified). Enforcement is not verified (C7-C14 blocked).
- Pin key semantics (ID vs Index) unresolved (C9).
- Strict account restriction MUST NOT ship as a guarantee.
- Silent fallback to another account cannot be ruled out from the verified evidence.

Investigation-only next step per contract §6 (P2): trace pick and conductor paths in `.cpa-source/sdk/cliproxy/auth/` and `internal/pluginhost/auth_callbacks.go`.

## 5. Evidence Matrix by Plan Area

| Plan area | Status | Evidence (path : symbol/line) | Gap |
|---|---|---|---|
| Strict pin contract | BLOCKED | `docs/v2/05-account-routing-contract.md` §2 (C7-C14), §6, §7 | Enforcement, key semantics, fail-closed behavior unverified. |
| Config / runtime | Baseline only, not account-aware | `config.go:15-87`; `runtime.go:12-120` | No account field in route, target, or runtime state. |
| Discovery | VERIFIED callback exists (contract); wiring NOT confirmed | Contract §3.1-§3.2; `.cpa-source/sdk/pluginabi/types.go` `MethodHostAuthList` (lines 95-110 per contract §3.1) | Plugin-side call sites of `host.auth.list` not searched in this audit. Evidence is contract-only. |
| Execution / stream | Route-target based | `execute.go:22` `executeWithHost`; `:26` `p.matchingRoute(request.Model)`; stream path in `stream.go` (modified, uncommitted; not read in this audit) | `hostRequest` (`execute.go:82-93`) sets no `AuthID`. Fields: EntryProtocol, ExitProtocol, Model, Stream, Body, Headers, Query, Alt. Stream pin path not read (C4 PARTIAL). |
| Attribution / storage | Usage-only persistence (per prior survey) | `usage_store.go:21-24` (`usageStorageSchemaVersion = 1`), `:28` (`usageStore`), `:37` (`minuteAggregate`), `:51`, `:59` (`Reconfigure`), `:106` (`openUsageDatabase`), `:142`, `:171` (`openSQLiteDatabase`); `attribution.go` tracker (per prior survey, not re-read) | No account-term search performed on `usage_store.go` in this audit; schema DDL not read. Account state would need a schema change and version bump. |
| Affinity | Not implemented | No symbol found for affinity or sticky account | Requires new design; depends on pin contract. |
| Management UI | Route-level only | `management.go:44-47` `modelRouterValidationRequest{Enabled, Routes}`; paths `:16-18` `/v0/resource/plugins/<pluginID>/config`, `/v0/management/plugins/<pluginID>/validate`, `/v0/management/plugins/<pluginID>/usage`; `dashboard.go` `assembleModelRouterDashboard`; `dashboard/configuration/*`, `dashboard/usage/tab.js` (per prior survey) | No account field in validation request (`:44-47`). Dashboard text (per prior survey) says "priority or round-robin" while `config.go` also defines `random` and `last_known_good`. Management API surface not audited. |
| Tests / integration | Existing suite run; account-routing tests absent | `config_test.go`, `execute_test.go`, `runtime_test.go`, `stream_test.go`, `integration_test.go`, `management_test.go`, `dashboard_test.go`, `usage_store_test.go`, `usage_management_test.go`; `tests/e2e/dashboard.test.mjs` | No account-routing test identified. Run status: see §9. Latest test-only additions: LKG fixture fix (`runtime_test.go`), concurrent `Clone`/RNG select test (`runtime_test.go`), stream LKG success-on-completion test (`stream_test.go`). |
| Release / platform | Build targets defined (per prior survey), NOT run | `Makefile` (`fmt-check`, `test`, `race`, `vet`, `check`; `CGO_ENABLED=1 -buildmode=c-shared`, `PLUGIN_ID=model-router`, `VERSION 0.4.2`); `main.go` `pluginVersion = 0.4.2`; `.github/workflows/test.yml`, `release.yml`, `codeql.yml` (per prior survey) | Values per prior survey, not re-read in this audit. No account-routing release gate identified. |

## 6. Plan Conflicts

1. "Combo" config assumption conflicts with source. Canonical unit is `routes`.
2. Strict account pin assumption conflicts with BLOCKED contract (C7, C14).
3. Pin key assumption (ID or Index) is unresolved (C9).
4. Account state persistence assumption conflicts with usage-only storage scope. Needs a schema change and version bump.
5. Dashboard strategy text conflicts with `config.go` strategy set.
6. Contract §1 scopes the management API out of the contract. Management API surface was not audited in this document. This does not mean product design excludes it. Account UI and validation need a separate design decision.
7. Working tree is dirty and contract docs are untracked. Baseline is not committed.

## 7. Requirements Status

| Requirement | Status |
|---|---|
| Account-pool definition | BLOCKED (depends on C9 and discovery semantics) |
| Account restriction / strict pin | BLOCKED (C7, C10-C14) |
| Account discovery via `host.auth.list` | Contract VERIFIED; implementation NOT present |
| Account-aware execution | NOT implemented |
| Account-aware storage | NOT implemented |
| Affinity | NOT implemented |
| Account-aware UI and validation | NOT implemented |
| Tests for account routing | NOT implemented |

## 8. Not Verified in This Audit

- Pick-path enforcement of `auth_id` (contract C7).
- ID vs Index matching (C9).
- Fail-closed behavior (C10-C14).
- Stream pin attachment beyond sync trace (C4).
- `execute.go`: read lines 1-34 and 81-96 only. Lines 35-80 and 97-113 not read; `:74` loop and `:113` not verified. `stream.go`, `router.go`, `failure.go` not read.
- Management API surface overall (`management.go:72-116`, `:118-170`, usage endpoints, dashboard JS). Not audited; no claim made either way.
- CPA behavioral matrix tests (contract C7-C14). Not run; no claim of passing.

## 9. Gate Results

Latest run after test-only edits (`runtime_test.go`, `stream_test.go`); no production code changed:

- `git diff --check`: EXIT=0.
- `go test ./... -count=1`: `ok`, EXIT=0 (after `stream_test.go` additions: `ok 11.159s`).
- `go test -race ./... -count=1`: `ok 107.062s`, EXIT=0 (after `stream_test.go` additions).
- `go vet ./...`: EXIT=0 (after `stream_test.go` additions).
- `make fmt-check`: EXIT=0.
- `go build ./...`: EXIT=0; linker warning `built for newer macOS version (13.0) than being linked (12.0)` non-fatal.
- Initial failures before fix: `TestRouteRuntimeLastKnownGoodFallsBackToPriority`, `TestRouteRuntimeCloneKeepsLastKnownGoodOnlyForSameSignature`. Cause: test fixture left zero-value LKG policy; runtime defaults apply only on decode path. Fixed in test by explicit `{0.5, 1, 60}` policy. Runtime defaults unchanged (`0.90`, `20`, `60`).
