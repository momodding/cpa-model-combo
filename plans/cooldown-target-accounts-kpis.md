# Cooldown, target accounts, and usage KPIs

## Context

Implement fixes and enhancements in momodding/cpa-model-combo against CLIProxyAPI v8.0.37 (ce07d189b3de2a39d37fd68ae093b102ce3aabee). Preserve module github.com/momodding/cpa-model-combo, plugin ID model-combo, ABI 1/schema 6, and existing routing strategies. Work is plugin-only. Do not modify CPA, fork PR #2, unrelated worktrees, or .cpa-source.

Approved decisions: target-level account ownership with copy-on-load legacy migration; hybrid plugin-local cooldown tracking plus host.auth.list summary availability; synthetic cooldown-skip children stored separately from provider accounting; empty valid KPI names shown as None and numbers as 0. Empty credential model lists mean unknown support and permit attempting the configured target with the selected credential. Nonempty lists remain authoritative. A private interceptor header is approved for exact trace transport if existing trustworthy metadata is insufficient.

User-approved delivery overrides conflicting global Git instructions: dedicated feature worktree; all relevant checks pass before commit, push only to marketplace, and open a PR. No direct merge to main and no intermediate commits before verification. Blocked required verification prohibits commit/push/PR.

## Approach

### 1. Isolate execution and record decisions

Persist this approved plan as plans/cooldown-target-accounts-kpis.md. Record source/work branch names. Create a dedicated worktree and branch omp/cooldown-target-accounts-kpis (smallest unused numeric suffix if needed) from the verified marketplace main baseline; preserve unrelated changes. Drive the numbered behavior groups through todos without intermediate commits.

Before behavior edits, create docs/adr/0007-cooldown-target-accounts.md and docs/bdr/0007-cooldown-target-accounts.md; confirm 0007 remains available, otherwise use the next sequence. Explicitly supersede applicable synthetic-child restrictions in existing decisions while retaining provider callbacks as the sole accounting input. Read all applicable ADR/BDR clauses first; exact clauses outside already established 0004/0005/0006 are unverified — confirm first. Record all approved decisions above, including unknown-model attempts and private trace transport. Do not change unrelated binding decisions.

### 2. Move allowlists to targets and preserve unknown model support

In config.go, add AllowedAccounts []string to modelTarget; add presence-aware allowed_accounts to modelTargetYAML. Continue accepting route-level allowed_accounts only as legacy input. Use the existing strict string-array parsing and trim/duplicate validation. Distinguish absence from explicit []: inherit the legacy route list only when the target field is absent, including legacy models converted to targets. Explicit [] is unrestricted. Save canonical target-level lists only; remove route-level ownership from runtime/callers after migration. Include each target's ordered list in routeSignature so configuration changes invalidate appropriate runtime state.

Update execute.go routeAccounts and both execution loops to use the selected target's list. No selected ID may fall back to another credential outside that list. Preserve account ordering, thinking-suffix matching, max_attempts as a target limit, and current strategy semantics. Missing/disabled selected credentials remain unavailable; catalog errors fail closed for restricted execution. Unrestricted execution must remain host-selected rather than pinning every enumerated credential.

In accounts.go accountCatalog, retain whether the host model list was empty BEFORE removing self aliases/invalid entries. Add an explicit model-support-unknown flag to the catalog DTO rather than interpreting a filtered empty list as unknown. accountCredential.supportsModel returns true for a configured target only when support is unknown, otherwise retains exact/base-model comparison. A catalog RPC failure is not unknown model support. Disabled status remains authoritative. Do not add an antigravity-specific branch or invent entitlement; entitlement remains unknown.

Apply the identical eligibility rule in management.go validation and dashboard/configuration validation. Error field paths identify routes[i].targets[j].allowed_accounts[k]. Keep configured/discovered model choices selectable when selected credentials have unknown lists; do not advertise their support as verified. No API key retrieval, raw auth metadata, or new discovery service. host.auth.models already reads registry.GetModelsForClient(auth.ID).

### 3. Preflight cooldown fallback and exact trace correlation

Reuse runtime.go synchronization, clock injection, route state, and strategy selection. Add credential/model-scoped retry deadlines for restricted targets so failure of one account never cools a healthy sibling. Preserve route/model cooldown behavior for unrestricted host selection. Expired deadlines cease blocking without timers. Clear successful scope and invalidate removed targets/account selections on reconfiguration. Do not turn an empty model list into a cooldown.

Extend catalog snapshots with safe Disabled/Unavailable/NextRetryAfter fields from host.auth.list; never persist/display StatusMessage or raw host errors. Known future credential deadlines plus local credential/model deadlines block only the relevant candidate. Skip a model only when every eligible allowed account is unavailable, or an existing model-wide cooldown applies. Disabled/unsupported/missing accounts are not mislabeled Cooling down. When support is unknown, it is an eligible candidate unless explicitly disabled or cooling. Do not claim access to CPA per-model cooldown snapshots: v8.0.37 does not expose them through host callbacks.

Have selection/preflight return structured skip information (model, optional credential identity, retry time, reason) rather than silently seeding an excluded set. Both execute.go and stream.go consume the same preflight decision. Record one model-skip child for a fully cooling target; include credential only when unambiguous. For an account skipped while another account on the same target can run, record the credential-level cooling skip too. Use the earliest finite deadline at which a candidate may become runnable for model-level retry time. Count skips separately from dispatched target attempts so preflight skips do not consume max_attempts. Preserve strategy ordering among eligible targets; deterministic skipped-row ordering follows configured target/account order.

Parse only structured model_cooldown indicators and bounded retry values from host errors/responses; never treat every 429 as proof of non-dispatch. Exact RPC error envelope and header transport are unverified — confirm first in host_callbacks.go and CPA transport before implementing the parser. Never retain raw messages. A confirmed native pre-dispatch model_cooldown rejection may become a cooling skip and update the local scoped deadline; ordinary executed failures retain current retry policy and provider accounting. Unknown retry values use the existing configured route cooldown rather than arbitrary new constants. All-cooling exhaustion retains model_route_cooldown; other exhaustion retains model_route_unavailable. Never sleep waiting for cooldown.

Streaming follows the identical preflight rules only before the first emitted payload. After payload delivery, do not switch target/account or fabricate a skipped attempt. Preserve existing stream close/cancel handling.

Trace transport: first verify whether trustworthy lifecycle/trace identity already reaches ExecutorRequest.Metadata. If not, InterceptRequestBeforeAuth replaces any client-supplied X-Model-Combo-Correlation header on routed requests with a crypto/rand token referencing exact TraceID and lifecycle RequestID in synchronized plugin state. Strip that header from non-routed requests too. Return it through RequestInterceptResponse.Headers; never alter request body. At executor entry, resolve only server-issued tokens; never correlate by model, timestamp, client API key, or credential. hostRequest clones headers and removes the private header before all provider callbacks. Propagation through stock CPA is an integration acceptance gate. Handle concurrent requests and repeated executor entry without cross-linking; allocate attempt ordinals in the correlation entry. Remove entries on lifecycle completion and reclaim abandoned entries using bounded retention consistent with the existing request-tracking lifecycle (exact attribution.go retention constant unverified — confirm first). Never persist the token. Missing/invalid correlation must not attach a skip to another parent; expose an uncorrelated diagnostic without inventing a parent. Forwarding the private header upstream is forbidden.

### 4. Store and display skipped attempts without accounting inflation

In usage_store.go add storage schema 4 with a transactional additive migration from 3. Preserve all requests, aggregates, session payloads, pricing, preferences, and sequence state. New table routing_skips: trace_id TEXT, lifecycle_request_id TEXT, attempt INTEGER, requested_at_ns INTEGER, payload BLOB; composite primary key (trace_id,lifecycle_request_id,attempt), timestamp index for retention. New RecordRoutingSkip method writes only this table, not Record or updateMinuteAggregateSQLite. Payload contains model, safe credential label/ID as appropriate to existing response conventions, retry_at, status="cooling_down", dispatched=false, and order. No tokens/cost/raw error fields. Include skip records in reset and retention, preserving settings. Exact reset/pruning function bodies are unverified — confirm first.

In usage_query.go read skips in the same SQLite snapshot as request sessions. Join by BOTH lifecycle ID and TraceID; do not merge ambiguous trace collisions. Attach to the parent even when there are zero provider executions. Provider rows remain authoritative for tokens, costs, failure counters, latency aggregates, charts, top models, and overview/group request counts. Synthetic rows do not enter any accounting accumulator. Existing parent outcome continues to come from completion. Preserve historical request IDs and existing collision warnings. Add status/dispatched/retry_at/order to the request-detail response without changing plugin schema 6. Preserve existing provider fields and their meanings; attempt_count may include displayed skips, but labels must say attempts rather than executions when mixed.

In dashboard/usage/tables.js render Cooling down as a neutral status, with model, optional safe credential, attempt order, and retry timestamp. Display unavailable credential as unavailable, not a guessed account. Synthetic rows show no token/cost/latency values, do not imply a provider failure, and cannot influence parent outcome. Keep expansion, focus restoration, column preferences, and collision warnings. Never substitute the parent's request ID as a fake provider execution ID for synthetic children.

### 5. Compact target account controls and drag ordering

In dashboard/configuration/routes.js normalize/migrate and serialize target lists using the same absence-versus-empty rule as Go. In tab.js include target lists and target order in configurationSnapshot, dirty checking, save, and reload. Remove route-level account controls.

Use each target row as the model parent and a collapsed-by-default account-selection child. Provide an accessible disclosure with aria-expanded/aria-controls; selected summary is All accounts or N accounts selected. Use a multi-select dropdown with labeled checkboxes, preserving selected account order; stale IDs remain visible as unavailable and block save rather than silently disappearing. Reuse existing components/styles; no new dependency. List model labels as model (safe account labels), with title tooltip and visual truncation. Known supporting accounts provide labels; unknown-support accounts are identified as support unknown instead of asserted providers. Keep configured models selectable for unknown-support selections.

Add target drag handles and drag/drop within the same combo only. Reuse moveTarget/state splice/DOM reorder logic; drops onto another combo are rejected. Preserve model, weight, and allowed_accounts together. Existing up/down buttons remain keyboard-accessible. Save then reload must reproduce exact target and account order. Renumber field paths, disclosure IDs, and controls after moves. No route-level drag feature is required.

### 6. Restore KPI rendering and cover every refresh path

User-reported defect: full render omitted renderUsageSummary. Ensure dashboard/usage/tables.js renderUsageDashboard invokes renderUsageSummary exactly once alongside table/filter/chart rendering. The prior scout report disagreed about the current call; do not add a duplicate if already present, and trace the observed failure through the assembled asset and actual response instead of declaring it fixed.

In dashboard/usage/tab.js renderUsageSummary, use None for valid empty top-router/top-provider lists, numeric zero for valid empty metrics, and — for unavailable data only. Keep last valid values on refresh failure with existing error/stale messaging; do not replace failures with fake zeros. Verify /v0/management/plugins/model-combo/usage/dashboard through usage.handle, SQLite, queryUsage, management JSON, and UI if the render fix alone is insufficient. All five KPIs must update on initial load, refresh, polling, and filter changes. Preserve generation/abort guards and scoped table refresh behavior.

## Critical files & anchors

- config.go: modelTarget, modelRouteYAML, decodeRouterConfig; accounts.go: accountCatalog/supportsModel.
- runtime.go: SelectExcluding/MarkFailure/MarkSuccess; execute.go: routeAccounts/hostRequest; stream.go execution loop.
- main.go: interception/completion; usage_store.go: initializeSQLiteDatabase/RecordSession; usage_query.go: queryUsage/requestParent.
- dashboard/configuration/routes.js and tab.js: normalizeRoute/serializeRoutes/configurationSnapshot/createTargetRow/moveTarget.
- dashboard/usage/tab.js and tables.js: refreshUsage/renderUsageSummary/renderUsageDashboard/request rows.

## Verification

Add regression coverage in existing Go and browser suites, not source-string assertions:

1. Priority/round-robin/random/LKG choose eligible fallback; controlled-clock expiry; all-cooling retry; one cooling selected account plus a healthy sibling; disallowed healthy account never used; unknown empty model list attempted, nonempty mismatch rejected; catalog failure fails closed; disabled unknown account still rejected. Cover blocking and streaming, including failure after first payload with no fallback.
2. Concurrent identical requests get separate exact skip parents. Client-spoofed headers cannot cross-link; upstream never sees internal header. Cancellation/completion releases correlation state. Real CPA integration verifies interceptor-to-executor transport and native cooldown error interpretation.
3. Schema 3→4 preserves existing data; skips survive restart, appear under zero-provider parents, respect retention/reset and collisions; adding only skips leaves overview/group requests, failures, tokens, costs, and top models unchanged. Test malformed legacy migration rollback.
4. Go/JS legacy route-list migration, explicit target [] override, invalid lists, canonical target-only save. Browser tests drag target with pinned accounts, Save→Reload order, collapsed disclosure, keyboard reorder, stale account errors, unknown-model eligibility and tooltip labels.
5. Browser tests all five KPI values for populated and empty responses, initial full dashboard load, refresh, timer polling, filters, failure with prior data, and stale response rejection. Exercise the actual rendered surface and capture screenshots.

Run these existing verification commands once after the complete change, fixing failures before delivery:

```sh
test -z "$(gofmt -l *.go)"
bash -n .github/xgo/*.sh
go test ./...
go test -race ./...
go vet ./...
node --test tests/dashboard/*.test.mjs
CPA_SOURCE="$PWD/.cpa-source" go test -tags=integration ./... -count=1
mkdir -p dist
CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -o dist/model-combo-v1.0.1.so .
npm ci
npx playwright-core install --with-deps chromium
```

Run npm run test:e2e against the disposable CPA fixture using the exact setup and environment in .github/workflows/test.yml: DASHBOARD_URL, DASHBOARD_MANAGEMENT_KEY, DASHBOARD_E2E_ALLOW_MUTATIONS=1, PLAYWRIGHT_CHROMIUM_EXECUTABLE. Use its Linux amd64/arm64 matrix and native Windows SQLite command for relevant platform checks. Add the existing JS unit command to CI so these regressions remain gated. Do not change the artifact version as part of this work.

Required Linux integration/browser verification must run before commit/push/PR in an available Linux environment; do not use a PR as a workaround for that prerequisite. After checks pass, commit only task-owned changes using the selective commit API, push the feature branch to marketplace, open the PR, and require its checks before any merge.

## Assumptions & contingencies

No implementation or runtime tests were performed during read-only planning. Paths/symbols above come from direct reads or named scout reports; explicitly marked remaining anchors must be confirmed before editing. Existing APIs should be extended rather than adding parallel routing or usage subsystems.

Native macOS CPA integration previously failed and Docker was unavailable. Do not repeat known-failing checks merely to confirm them. Find an available Linux runner/host; if inaccessible, finish reachable implementation/checks and report the exact verification blocker without committing or pushing.

Host.auth.list exposes credential summary retry state, not full per-model CooldownSnapshotForAuth. Hybrid preflight can avoid known cooldowns but cannot guarantee detecting an unseen native model-specific cooldown before the first host call. Native structured rejection updates local state; do not call it a provider execution without evidence. Unknown support permits an attempt but does not guarantee CPA can execute it.

If private-header propagation or native error transport cannot satisfy exact correlation/non-dispatch classification on stock v8.0.37, stop that part with evidence and request a decision; do not change CPA, leak headers, fabricate trace associations, or silently omit requested behavior. Before conflicting with any additional binding ADR/BDR, ask and record the decision.

<!-- source-branch: omp/sdk-v8-trace-accounts-3 -->
<!-- work-branch: omp/cooldown-target-accounts-kpis -->
