# request-details-request-id-hierarchy
<!-- source-branch: omp/fix-v1-0-1-request-release-dashboard -->
<!-- work-branch: omp/request-details-request-id-hierarchy -->

## Context

Revamp Request details so each inbound CPA API request is one parent row keyed by CPA's generated request ID, with every provider execution from that request shown as ordered children. In the target fallback case, inbound request `1234` on `POST /v1/chat/completions` has child 1 `gpt5.6-sol` with HTTP 429 and child 2 `claude-sonnet-5.5` with HTTP 200; parent `1234` is successful and expands to both children.

Investigation confirms the UI hierarchy is possible, but not exactly from Model Combo alone. CPA already exposes the inbound ID as `RequestInterceptRequest.TraceID` / `RequestCompletion.TraceID`, preserves that ID in callback context, and exposes the path as `request_path` metadata / logging endpoint; however `UsageRecord` omits both, while Model Combo currently groups routed attempts by a plugin-generated random `router_request_id`. Exact correlation therefore requires an additive CPA usage-callback contract change plus Model Combo storage/query/UI changes. Do not implement a timestamp/body-hash approximation: concurrent identical requests could be assigned to the wrong parent.

## Approach

### 1. Isolate both repository changes and bind the contract

- Create a Model Combo sibling worktree from `marketplace/omp/fix-v1-0-1-request-release-dashboard` on `omp/request-details-request-id-hierarchy`; this feature depends on PR #3's existing logical-parent query/UI. Create a separate CLIProxyAPI worktree from commit `61fdfc341b96178a8dcb53f2efc46cbc341d267c` on `omp/usage-request-identity`; never edit Model Combo's ignored `.cpa-source` checkout as a deliverable.
- Persist this approved plan in Model Combo at `plans/request-details-request-id-hierarchy.md` with source/work branch comments. Before product edits, add Model Combo `docs/adr/0004-request-id-parent-provider-child-contract.md` and `docs/bdr/0004-request-details-parent-child-behavior.md`. Record inbound CPA `TraceID` as authoritative parent identity, lifecycle completion as parent outcome, provider callbacks as children/accounting rows, exact host support requirement, legacy fallback behavior, zero-child parents, and unchanged overview/group callback accounting.
- Keep the repositories as two reviewable changes: CPA first exposes additive identity; Model Combo consumes it while remaining readable on older hosts. Push CPA through a user fork and upstream PR if direct upstream write is unavailable; do not vendor or commit `.cpa-source` into Model Combo.

### 2. Extend CPA's native usage callback with inbound identity

- In CLIProxyAPI `sdk/pluginabi/types.go`, raise `SchemaVersion` from 6 to 7 and add `SchemaVersionUsageRequestIdentity = 7`; update the schema-version contract test. Schema 7 means usage callbacks include exact inbound identity.
- Add `TraceID string` and `RequestEndpoint string` to `sdk/pluginapi.UsageRecord`. `TraceID` is the inbound CPA API request ID from `internal/logging.GetRequestID(ctx)`; `RequestEndpoint` is the normalized method and path from `internal/logging.GetEndpoint(ctx)`, for example `POST /v1/chat/completions`.
- In `internal/pluginhost/adapters_usage_translation.go:usageAdapter.HandleUsage`, populate those fields directly from the callback context when translating `coreusage.Record` to `pluginapi.UsageRecord`. Keep the internal accounting record unchanged: identity belongs to plugin delivery, and callback context already survives `host.model.execute` through `HostCallbackID`.
- Extend CPA adapter tests to prove one direct provider usage callback and one nested `host.model.execute` usage callback both carry the same inbound trace and endpoint. Also assert empty context values remain empty for non-HTTP/internal callers. Deliver this as a separate CPA commit/PR.

### 3. Persist inbound parent sessions separately from provider usage

- Add `storedRequestSession` in Model Combo with exact fields: `RequestID`, `Endpoint`, `RequestedModel`, `RouterModel`, `Attribution`, `Stream`, `Outcome`, `StatusCode`, `StartedAt`, `CompletedAt`, and derived `LatencyNS`. Do not store `RequestCompletion.Error`; it can contain provider payloads or credentials.
- Add a request-session tracker keyed by lifecycle `RequestID`, because interception and completion share that execution ID even when inbound `TraceID` differs. In `InterceptRequestBeforeAuth`, stage only external requests when negotiated host schema is at least 7: ignore metadata `source == "plugin_host_model_callback"`; retain `TraceID`, `request_path`, requested model, stream flag, and whether the requested model matches a configured route. On completion, look up and consume by `completion.RequestID`, then persist under the staged inbound `TraceID` (or execution ID fallback). Prune entries older than 24 hours and cap at 50,000 by dropping oldest, matching the existing attribution tracker's bounds. Continue existing direct/routed attribution behavior.
- Implement `pluginapi.RequestLifecyclePlugin` on `modelRouterPlugin`. In `registerModelRouter`, set `requestIdentityEnabled = request.SchemaVersion >= 7`, return plugin schema 7 and advertise `request_lifecycle_plugin: true` only when enabled; hosts below schema 7 retain the current schema-3 registration and legacy request table with no duplicate parent sessions. Dispatch `pluginabi.MethodRequestComplete` only for enabled registrations. On completion consume only a staged lifecycle request; if both staged and completion trace IDs are non-empty but differ, drop the session and record a storage error rather than misgroup it. Persist under the staged inbound `TraceID`, falling back to `completion.RequestID` only for enabled non-HTTP calls with no trace. Use an idempotent upsert so duplicate completion delivery cannot create duplicate parents; completion outcome/status/timestamps are authoritative.
- Add a `request_sessions` SQLite table keyed by `request_id`, indexed by `started_at_ns`, and bump `usageStorageSchemaVersion` from 1 to 2. Replace the current “unsupported migration” branch with one transaction that migrates v1 to v2 by creating this table/index and updating `store_state.storage_schema_version`; leave existing `requests`, aggregates, prices, preferences, and sequence values untouched. Include `request_sessions` in retention pruning and usage reset.
- Existing databases retain historical provider records. Historical records without inbound IDs continue through the existing legacy grouping: routed rows group by `router_request_id`, direct/unattributed rows remain standalone. Never guess a parent ID for old rows.

### 4. Attach every provider callback to the exact inbound request

- Decode CPA's additive usage fields at Model Combo's ABI boundary without changing the pinned SDK dependency: introduce `usageRPCRecord` embedding `pluginapi.UsageRecord` plus `TraceID string` and `RequestEndpoint string`; unmarshal `usage.handle` into it and pass both values into usage recording. Keep this local envelope until Model Combo intentionally raises its minimum CPA SDK version in a separate migration; do not add reflection or duplicate persisted identity fields.
- Extend `storedUsageRecord` with `RequestID` serialized as `request_id` and `RequestEndpoint` serialized as `request_endpoint`, both omitted when empty. Populate them from CPA's usage callback independently of attribution matching; attribution still supplies `RouterModel`, `RouterAttempt`, and legacy `RouterRequestID`.
- For routed execution, keep one plugin-generated `router_request_id` per fallback loop only as legacy/attempt-order metadata. It must no longer be the primary parent key when `RequestID` is present.
- Preserve callback-level overview and Usage breakdown accounting: parent sessions are stored in their own table and never added to token, cost, latency, failure, or request counters. Provider callbacks remain the sole accounting input.

### 5. Build request pages from parents plus ordered provider children

- In `usage_types.go`, add request-page wire fields `request_id`, `request_endpoint`, `requested_model`, and `stream` to `usageRequestDetail`; keep child usage metrics and the existing `attempts` array. In `usage_query.go`, group records with non-empty `RequestID` by that ID and join them to `request_sessions`. Emit one top-level detail per inbound request and attach matching provider records in `Attempts`. Choose one ordering mode per parent: if every child has the same non-empty `RouterRequestID`, sort all children by `(RouterAttempt, RequestedAt, Sequence)`; otherwise sort all children by `(RequestedAt, Sequence)`. Never use a pairwise mixed comparator.
- Parent identity comes from `storedRequestSession`: request ID, endpoint, requested model, routed/direct attribution, stream flag, final outcome/status, start time, and end-to-end latency. Prefer a child's exact `RequestEndpoint` over the staged `request_path` when filling the parent endpoint; zero-child parents retain the staged path. Aggregate child tokens, cache counters, TTFT, throughput, and estimated cost using the existing aggregation helpers; leave parent provider/source/API-key fields empty because those belong to children. A successful final completion makes the parent successful even when an earlier child is HTTP 429.
- Include completed sessions with zero provider children so terminal cooldown/auth/validation failures remain visible as inbound CPA requests. If provider usage arrives before asynchronous completion, emit a temporary parent synthesized from the exact callback `RequestID`; replace it with lifecycle metadata on the next refresh.
- Keep filtering semantics explicit: time/result/router-model filters evaluate parent fields; provider/source/service-tier filters match any child. Sorting and pagination count parents, never children. Bump the management response `schema_version` to 2 because request-detail identity and parent semantics change.
- Retain legacy handling for records with empty `RequestID`: routed fallback groups by `RouterRequestID`; direct/unattributed records remain standalone. Do not merge legacy rows with new request sessions.

### 6. Make Request details visibly parent/child

- In `dashboard/shared.js`, add visible request columns `request_id` (`Request ID`), `request_endpoint` (`API path`), and `requested_model` (`Requested model`). Put `Request ID` first and keep existing selectable provider/accounting columns.
- In `dashboard/usage/tables.js`, move expand/collapse control from Router model into Request ID. Parent rows show the inbound ID and endpoint/requested model; child rows show `Attempt 1`, `Attempt 2`, and their provider model/provider/result/metrics. Use `request_id` as expansion state key, with a sequence fallback only for legacy rows lacking one.
- Render every parent with children as expandable, including direct requests with one provider child. Preserve keyboard focus, `aria-expanded`, `aria-controls`, and descriptive labels such as `Expand provider attempts for request 1234`.
- In `dashboard/usage/styles.css`, distinguish parent and child rows with existing theme variables, indent the Request ID cell for children, and preserve horizontal scrolling and narrow-screen behavior. Update the Request details help text in `dashboard/usage/panel.html` to state that one CPA API request expands to its provider attempts.

### 7. Document the changed request-detail contract

- Update the usage tracking contract and README examples so `request_id` means inbound CPA API request ID, `router_request_id` is legacy/internal attempt correlation, and Request details total/pagination count inbound parents.

## Critical files & anchors

- CPA `internal/pluginhost/adapters_usage_translation.go` — `usageAdapter.HandleUsage` owns context-to-plugin usage identity delivery.
- CPA `sdk/pluginapi/types.go` — `UsageRecord` is the additive native plugin contract.
- Model Combo `main.go` / `abi.go` — request interception and lifecycle registration/dispatch create authoritative parents.
- Model Combo `usage_store.go` — schema-v2 migration, parent-session persistence, retention, and reset.
- Model Combo `usage_query.go` — parent join, child ordering, filter/sort/pagination, and legacy fallback.

## Verification

- CPA unit contract: publish usage under context request ID `1234` and endpoint `POST /v1/chat/completions`; assert delivered `pluginapi.UsageRecord.TraceID == "1234"` and `RequestEndpoint == "POST /v1/chat/completions"`. Repeat through nested `host.model.execute` and expect the same values.
- Registration compatibility: host schema 7 yields plugin schema 7 with `request_lifecycle_plugin: true`; host schema 6 yields the current schema 3 with lifecycle false, no `request_sessions` writes, and unchanged legacy grouping.
- Model Combo storage migration: open a schema-v1 fixture containing requests/prices/preferences, migrate to v2, and assert all old data is unchanged, `request_sessions` exists, reset clears both detail tables, and retention removes expired parents and children.
- Model Combo request query: record parent `1234`, child attempt 0 `gpt5.6-sol`/429, and child attempt 1 `claude-sonnet-5.5`/200. Query `/usage/requests`; expect total `1`, parent ID `1234`, endpoint `POST /v1/chat/completions`, result success, and exactly two ordered children with those models/statuses. Assert overview and group totals remain `2` provider callbacks, not `3` rows.
- Cover direct success with one child, terminal route failure with zero children, provider usage arriving before completion, concurrent identical requests with distinct IDs, and legacy records lacking request IDs.
- Dashboard behavior test: render the response above, expand request `1234`, and assert parent plus two child rows, `aria-expanded`/`aria-controls`, child labels and statuses, and persistence of expansion across refresh. Verify desktop and narrow widths in a real browser.
- Run CPA focused pluginhost tests, Model Combo focused storage/query/dashboard tests, then `go test ./...` in both repositories. Build the native plugin against the CPA contract and run one integration request whose first provider returns 429 and second returns 200; assert the API response succeeds and Request details shows one parent/two children with the same inbound ID.

## Assumptions & contingencies

- Exact grouping requires CPA plugin schema 7. Model Combo negotiates this at registration: schema-7 hosts enable lifecycle parents and exact child IDs; older hosts keep the existing `router_request_id`/standalone behavior and do not persist new parents. If upstream CPA declines public `UsageRecord` fields, implement the same `TraceID` and `RequestEndpoint` values in CPA's private schema-7 native usage RPC envelope and keep Model Combo's `usageRPCRecord` decoder unchanged; do not fall back to timing, body hashes, credentials, or FIFO matching.
- Base Model Combo work on `marketplace/omp/fix-v1-0-1-request-release-dashboard` until PR #3 merges because that branch contains the current parent/attempt query and dashboard hierarchy. After PR #3 merges, target the new feature PR at `main`; before merge, keep it as a dependent branch rather than copying or reimplementing those commits.
- `RequestEndpoint` uses CPA's existing normalized `METHOD /path` value. Query strings and request bodies are not stored.
