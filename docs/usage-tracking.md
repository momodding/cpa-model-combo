# Usage Tracking

This document is the implementation contract for Model Combo usage tracking. Keep it with the feature when rebasing or syncing changes from another remote.

## Scope

The feature adds a **Usage tracking** tab to the existing Model Combo management page. **Configuration** remains the default tab, and the centered tab controls sit above the route table and **Add route** button.

Usage tracking covers both kinds of CPA traffic:

| Attribution | `router_model` | `provider_model` | Stored row |
| --- | --- | --- | --- |
| Routed | Client-visible Model Router alias | Physical CPA target | One row per delivered CPA usage record for an attempted target, including failures before failover |
| Direct | Empty | Physical CPA model | One row per delivered CPA usage record for a direct request |
| Unattributed official record | Empty | Model from CPA's usage record | One row when a safe correlation cannot be made |

Router and provider identities must remain separate throughout storage, filtering, aggregation, pricing, and display. A routed response can be rewritten to the alias for the client without changing the stored physical provider model.

Request details instead presents inbound requests as parents keyed by CPA's `request_id`, with delivered provider records as attempt children. Overview and grouped accounting still count provider callbacks, not parents. A completed request can appear with no children; a direct request with one callback is expandable.

The feature intentionally does not include:

- encryption or API-key recovery;
- API-key labels;
- batching or flush-interval controls;
- response compression controls;
- CNY conversion;
- CSV or image export;
- database backup or restore endpoints;
- a separate full-mode dashboard.

## Configuration and persistence

```yaml
plugins:
  configs:
    model-combo:
      enabled: true
      data_path: "/CLIProxyAPI/plugins/cpa-model-combo.db"
      retention_days: 365
      routes: []
```

| Field | Default | Validation |
| --- | --- | --- |
| `data_path` | CPA root plus `plugins/cpa-model-combo.db` | Converted to a clean absolute path; the parent directory is created with mode `0700` |
| `retention_days` | `365` | Integer from 1 through 3650 |

`data_path` is a filesystem path, not a `file://` URI. Windows examples are `plugins/cpa-model-combo.db`, `D:/CLIProxyAPI/plugins/cpa-model-combo.db`, and `'D:\CLIProxyAPI\plugins\cpa-model-combo.db'` in YAML. Relative paths resolve from the CPA process working directory. Configure drive-letter paths without an extra leading slash; SQLite URI encoding is handled internally.

If `data_path` is absent, resolution checks:

1. the loaded shared-library path for an ancestor named `plugins`;
2. the CPA executable directory for a `plugins` directory;
3. the current working directory for a `plugins` directory;
4. `<absolute working directory>/plugins/cpa-model-combo.db` when no CPA layout is found.

The resolver does not inspect, copy, or migrate former database locations. An upgrade with no explicit `data_path` starts a new database. Configure an old path explicitly when its history must remain active.

The plugins directory must be writable and persistently mounted. Use an explicit absolute path elsewhere when production mounts plugin binaries read-only. Process restart persistence requires the next process to open the same file. Container replacement persistence requires that file's parent directory to be mounted outside the disposable container layer.

The database is dedicated to Model Router and uses SQLite WAL with file mode `0600`. A successful record, price edit, or preference edit commits before its API call returns. There is no in-memory write batch to flush during shutdown. WAL permits an old and a new plugin generation to overlap during a hot update without holding a process-lifetime bbolt lock.

The SQLite schema contains:

```text
cpa-model-combo.db
├── store_state
│   ├── storage_schema_version
│   ├── next_sequence
│   ├── last_prune_day
│   ├── prices_json
│   └── preferences_json
├── requests
├── request_sessions
└── minute_aggregates
```

`requests` stores provider usage records; `request_sessions` stores completed inbound request metadata. `minute_aggregates` stores UTC minute aggregates keyed by attribution, router model, provider model, provider, source, service tier, and result. Overview queries read provider records so filtering and price changes are reflected consistently.

Retention pruning runs when the database opens or is reconfigured and then at most once per UTC day during writes. SQLite may retain reusable pages after deletes and WAL checkpoints, so disk planning must account for peak retained request volume, including failed routed attempts.

SQLite storage schema 2 automatically migrates a schema-1 database transactionally by adding `request_sessions` and its index, preserving existing records, prices, and preferences. Back up the database consistently, including any live WAL data, before upgrading. An older plugin that supports only storage schema 1 cannot open the upgraded schema-2 database; rollback requires restoring a pre-upgrade backup or using a separate database, and restoring a backup discards later history. Historical records are not backfilled with guessed request IDs.

### v0.3 to v0.4 migration

When the configured path is a v0.3 bbolt file, v0.4 creates `<data_path>.migration-v1.json`, copies the source into `<data_path>.sqlite-v1.migrating`, validates row counts, sequence state, metadata, and SHA-256 content digests, then renames the source to `<data_path>.bbolt-v1.bak` before installing the SQLite file. The marker is removed only after the installed database passes `PRAGMA quick_check` and the same parity checks. If CPA or the plugin is interrupted, reopening the same path resumes the recorded phase. A changed source, invalid marker, mismatched digest, or ambiguous artifact set stops with an error and leaves the source/backup available for diagnosis.

The v0.3 to v0.4 update requires one controlled plugin unload/delete/reinstall while CPA remains running so the old bbolt handle is closed. Do not delete the configured data path. After that transition, v0.4.x updates can overlap generations through SQLite WAL. To roll back a failed migration while the plugin is unloaded:

```bash
cp -- "<data_path>.bbolt-v1.bak" "<data_path>"
rm -f -- "<data_path>.migration-v1.json" "<data_path>.sqlite-v1.migrating"
```

The backup is the original bbolt file and must not be overwritten with an unverified copy.

**Reset usage** deletes rows from `requests`, `request_sessions`, and `minute_aggregates`. It preserves model pricing and dashboard preferences in `store_state`.

## CPA usage delivery and attribution

CPA's `usage_plugin` callbacks are the sole source of provider accounting. CPA normalizes provider metadata, timing, failures, totals, cache reads, cache creation, and reasoning before delivering `usage.handle`; Model Router does not parse response bodies or stream chunks for usage and does not persist a fallback usage record. With CPA plugin schema 7, lifecycle completion additionally records inbound request metadata and final outcome, independently of provider accounting.

```text
request starts
    |
    +-- routed alias --> mark alias + physical attempt --> host.model.*
    |
    +-- direct model --> request interceptor --> host request

CPA usage.handle callback --> correlate marker and store one request row
                         +--> unmatched or ambiguous: store as unattributed
```

Correlation uses request time, provider model, and an in-memory keyed fingerprint of the client credential when available. The fingerprint secret is random for the process and is never persisted. A uniquely matched marker is consumed. Conflicting candidates and timestamp-less callbacks with multiple active candidates are not guessed; the delivered record is retained as `unattributed`. Markers expire after 24 hours and are capped at 50,000 entries.

CPA v7.3.9 or newer is required. That release preserves numeric HTTP statuses returned through native model callbacks, which lets Model Router apply its configured fallback rules. CPA v7.2.143 also detached asynchronous native usage delivery from request cancellation while preserving context values. CPA v7.2.142 and older can cancel `usage.handle` before Model Router receives it; the plugin does not recreate missing records from response bodies.

Exact request-ID parents require a CPA host offering plugin schema 7, which adds `UsageRecord.TraceID` and `UsageRecord.RequestEndpoint`; the plugin registers its request lifecycle capability only when the host offers schema 7 or newer. A schema-6 host retains legacy behavior: routed records with a `router_request_id` group together, while direct and unattributed records are standalone. Historical records lacking `request_id` use the same fallback and are never joined to new parents by inference. Schema 7 is a capability requirement in addition to the CPA version requirements above; a version number alone does not establish that a host offers it.

Routed stream retry behavior is unchanged: an attempt can fail over only before any upstream payload is emitted. CPA remains responsible for stream timing and usage totals.

## Stored data and privacy boundary

Each request row can contain:

- UTC request time and sequence;
- routed, direct, or unattributed classification;
- router alias and physical provider model as separate fields;
- provider, executor protocol, sanitized source, service tier, and reasoning effort;
- success/failure state and numeric status when available;
- latency, time to first token, generation duration, and output throughput;
- input, output, reasoning, cached, cache-read, cache-creation, and total tokens;
- a short masked API-key display value;
- an estimated USD cost resolved at query time.

When supplied by a schema-7 host, provider records also carry the inbound `request_id` and `request_endpoint` (for example `POST /v1/chat/completions`). A completed lifecycle session stores its request ID, endpoint, requested and router models, attribution, stream flag, outcome, status, start/end times, and latency. The plugin stores the endpoint string CPA supplies and does not add query strings or bodies.

The plugin does not persist:

- raw API keys or credential fingerprints;
- prompts or request bodies;
- model response bodies;
- failure bodies;
- request URL query strings or lifecycle completion errors;
- response headers.

CPA can place an API key in `UsageRecord.Source`. For API-key authentication, source values equal to the API key, values with common credential prefixes, and token-shaped strings with at least eight characters fall back to a provider/executor label. HTTP(S) sources retain only scheme, host, port, and path; URL user info, query strings, and fragments are removed.

The database is not encrypted. The plugin creates and reopens it with `0600` file permissions; protect its directory with filesystem permissions and volume access controls as well.

## Pricing

Prices are USD per one million tokens and are keyed by the physical provider model. Supported rates are:

- input;
- output;
- cache read;
- cache creation.

A model can also define ordered context thresholds, service-tier-specific schedules, and one of these input accounting modes:

| Mode | Meaning |
| --- | --- |
| `input_includes_cache` | Cache-read and cache-creation tokens are already part of input and are subtracted before applying the normal input rate |
| `input_excludes_cache` | Input, cache-read, and cache-creation tokens are billed independently |
| empty | Provider-default calculation |

The price book has a monotonically increasing revision. Save and sync requests must name the revision they loaded; a stale revision returns `409` instead of overwriting another edit.

**Sync models.dev** fetches `https://models.dev/api.json` over HTTPS with a 15-second timeout and response-size limits. Matching uses provider priority, ignored suffixes, and explicit mappings. Manual rows are never overwritten by synchronized values.

Cost is calculated when querying. Changing a price updates estimates for retained history without rewriting request rows. Unmatched requests are counted as unpriced rather than silently treated as zero-cost.

## Dashboard behavior

The Usage tracking tab includes:

- preset and custom local-time ranges converted to UTC;
- minute, hour, and day granularity;
- router model, provider model, source, service tier, and result filters;
- token, cost, request, and top-model summaries;
- token, provider-share, cost, and efficiency charts;
- grouped usage and per-request tables with sorting, column controls, and pagination;
- model pricing and models.dev sync;
- an explicit typed confirmation for history reset.

Request details shows **Request ID**, **API path**, and **Requested model**. Request ID is always visible and is removed from saved hidden-column preferences. Expansion state uses `id:<request_id>` or `legacy:<sequence>` for rows without an ID. For example, request `1234` to `POST /v1/chat/completions` can expand to `gpt5.6-sol` (429), then `claude-sonnet-5.5` (200): its lifecycle parent is successful with status 200, while both provider attempts remain visible and count in overview and breakdown.

Children sharing one non-empty `router_request_id` sort by `router_attempt`, `requested_at`, then `sequence`; otherwise they sort by `requested_at`, then `sequence`. Lifecycle fields supply the parent result and end-to-end latency. If provider usage arrives before completion, a temporary parent is derived from children with the exact request ID until lifecycle metadata arrives.

The token chart keeps cache-read and separately reported reasoning segments mutually exclusive with their parent counters according to each record's accounting mode, including when one time bucket mixes providers with different accounting conventions.

All four charts expose a viewport-clamped tooltip on pointer hover or keyboard focus. Arrow keys and Home/End move between buckets or provider segments; Escape dismisses the active readout. Token tooltips show visible token series, the visible total, request count, and estimated cost. Cost and efficiency tooltips show the values represented by their lines. Provider-share tooltips show requests, share, tokens, and cost. Clicking an exact provider segment or legend entry, or pressing Enter/Space on a focused segment, toggles the existing provider-model filter. The aggregate Other segment is inspectable but not filterable.

The browser loads overview, groups, and request details through one dashboard query. All three use one SQLite read snapshot and one price-book revision. Overview and group queries scan matching provider records; request details scans provider records and sessions to construct and filter complete parents before sorting and pagination.

Range and filter changes are debounced for 250 ms. Changing granularity refreshes only overview; sorting, grouping, and pagination refresh only the affected table. Partial refreshes reuse the displayed time interval. When another change overlaps unfinished work, the latest state receives a complete refresh. Existing metrics, charts, and rows stay mounted during a refresh. Each refresh owns an `AbortController` and generation number; only the newest active generation can render. Browser cancellation does not cancel native query execution. Table scroll positions are restored after row replacement.

Polling runs every 15 seconds only when:

```text
Usage tracking is selected
AND the document is visible
AND no newer refresh has replaced the timer
```

Leaving the tab or hiding the document stops the timer and aborts the active request. Returning starts one fresh request set. Dashboard preferences are saved to SQLite after a short debounce. Both tables default to 50 rows. Router model, Provider, Source, Service tier, and Result are hidden by default in Usage breakdown; every column checkbox change updates `hidden_group_columns`. Existing saved selections, including an empty hidden list or a saved page size, remain authoritative, except that Request ID cannot be hidden.

The page follows CPAMC light, white, and dark themes, redraws canvas charts after theme changes, supports keyboard tab and chart navigation, exposes visible focus states, fits metric values without ellipses, and collapses controls and charts for mobile widths. Token trends use packet-style columns, cost and efficiency use halo lines, and provider share uses a gapped donut. Hover and keyboard focus share the same active treatment: packet buckets widen and gain a marker, lines gain a guide and enlarged halo markers, and donut segments lift while peers dim. Successful request results use green pills. Saving valid model pricing closes the modal after the persisted price book is applied; validation or request failures leave it open.

## Management API

All endpoints require CPA Management API authentication.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/v0/management/plugins/model-combo/usage/dashboard` | Overview and both tables from one read snapshot |
| `GET` | `/v0/management/plugins/model-combo/usage/overview` | Summary, series, filter values, and storage status |
| `GET` | `/v0/management/plugins/model-combo/usage/groups` | Sorted and paginated grouped usage |
| `GET` | `/v0/management/plugins/model-combo/usage/requests` | Sorted and paginated request details |
| `GET` | `/v0/management/plugins/model-combo/usage/prices` | Read the price book |
| `PUT` | `/v0/management/plugins/model-combo/usage/prices` | Replace prices and sync settings at an expected revision |
| `POST` | `/v0/management/plugins/model-combo/usage/prices/sync` | Synchronize observed models from models.dev |
| `GET` | `/v0/management/plugins/model-combo/usage/preferences` | Read dashboard preferences |
| `PUT` | `/v0/management/plugins/model-combo/usage/preferences` | Save dashboard preferences |
| `POST` | `/v0/management/plugins/model-combo/usage/reset` | Delete usage after receiving `{"confirm":"reset"}` |

Overview, group, and request queries accept RFC3339 `from` and `to` plus optional `attribution`, `router_model`, `provider_model`, `source`, `service_tier`, and `result` filters. `router_model` always matches a literal route alias. Use `attribution=direct`, `attribution=unattributed`, or `attribution=routed` to filter by request origin; this keeps aliases named `direct` or `unattributed` distinct from the synthetic traffic classes. Group and request endpoints also accept `sort`, `order`, `offset`, and `limit`; the maximum page size is 500.

The dashboard endpoint accepts the same filters and `granularity`, plus `group_dimension`, `group_sort`, `group_order`, `group_offset`, `group_limit`, `request_sort`, `request_order`, `request_offset`, and `request_limit`. Defaults and limits match the individual endpoints. It returns `overview`, `groups`, and `requests`, with a shared `generated_at`, `schema_version`, and `price_book_revision` on the outer response. Usage overview, group, request, and dashboard responses use management `schema_version: 2`, distinct from CPA plugin schema 7 and SQLite storage schema 2.

Request-page `items` are parents, and `total` counts matching parents rather than provider callbacks. Parent details include `request_id`, `request_endpoint`, `requested_model`, `stream`, `result`, `status_code`, and `latency_ns` when available; `attempts` contains provider details and `attempt_count` reports their number when nonzero. Identity and attempt fields may be omitted when unavailable. Parent token and estimated cost values aggregate children. Parent time, attribution, router-model, and result filters apply to the parent; provider-model, source, and service-tier filters match any one child. Zero-child parents do not match those child-specific filters.

## Implementation map

```text
config.go                    storage configuration and validation
plugin_path*.go              default database path discovery
main.go                      plugin capabilities and usage callbacks
abi.go                       schema negotiation and usage dispatch
request_sessions.go          inbound lifecycle staging and completion
attribution.go               correlation markers and ambiguity handling
usage_types.go               persisted and API data contracts
usage_store.go               SQLite WAL lifecycle, records, reset, prices, preferences
usage_migration.go           v0.3 bbolt parity migration and crash recovery
usage_query.go               filters, aggregation, sorting, pagination
usage_pricing.go             price validation and cost calculation
modelsdev.go                 catalog fetch, matching, and synchronization
usage_management.go          authenticated management endpoints
dashboard.html               Single-page dashboard shell; Go embeds dashboard/ sources into this response
dashboard/shared.*           Shared styles, authentication, theme, and page elements
dashboard/configuration/     Configuration panel, route templates, styles, and behavior
dashboard/usage/             Usage tracking panel, charts, tables, pricing, and reset behavior
dashboard/app.js             Ordered page startup and cross-tab lifecycle events
integration_test.go          real CPA plugin, request, stream, and restart coverage
```

## Validation checklist

Before publishing changes to this feature, run:

```bash
make check
make build
CPA_SOURCE=../CLIProxyAPI \
  go test -tags=integration ./... -count=1
```

Public CI runs this suite against the exact CPA v7.3.9 commit so the minimum supported delivery contract remains covered.

For repeatable query performance comparisons, point the optional benchmark at a representative SQLite database:

```bash
CPA_USAGE_BENCH_DB=/path/to/usage-copy.db \
  go test -run '^$' -bench '^BenchmarkUsageQueries$' \
  -benchmem -benchtime=1x -count=5 -timeout=45m
```

The benchmark opens the supplied database in read-only mode without initialization, migration, pruning, or reset. It fixes ranges relative to the newest stored request and compares the original bulk-query algorithm with the streaming implementation. Its cases cover short, weekly, monthly, custom, and full-retained ranges; all granularities and filters; combined and empty searches; every group dimension and request sort; and pagination. Output includes response serialization, elapsed time, and allocations. Record five-run medians, browser rendering time, and end-to-end time separately; local database results do not establish production performance under unknown container limits.

Then install the built library in a disposable CPA instance and verify:

1. Configuration is the default tab and keyboard tab navigation works.
2. Routed and direct streaming and non-streaming requests remain distinct, and every delivered CPA usage record appears once.
3. Request `1234` with a 429 followed by a 200 appears as one successful parent with two ordered provider attempts; overview and groups count both callbacks.
4. Refresh keeps old values and scroll positions visible while requests are pending.
5. Every chart tooltip and highlight works with pointer and keyboard input, including after zoom, resize, refresh, and theme changes.
6. Metrics show their complete values without ellipses at desktop, tablet, and mobile widths.
7. Breakdown columns use the default hidden set, and saved column selections survive reload and restart.
8. A successful price save closes the modal; validation failures keep it open.
9. Usage, prices, and preferences remain after process restart and container recreation with the writable plugins directory mounted.
10. Light, white, and dark themes redraw charts without console errors.
11. Reset removes history but preserves prices and preferences.
12. Settled filter changes issue one dashboard query; granularity and table controls issue only their affected endpoint and preserve the displayed interval.
13. Rapid overlapping control changes render only the latest complete state; long minute-level series retain all buckets and support chart navigation without argument-limit errors.

## Origin

The dashboard information architecture, pricing workflow, persistence model, and operational lessons were adapted from [AITNR/cap-token-usage-tracker](https://github.com/AITNR/cap-token-usage-tracker) at commit `056175a7b3165365d0fffa36283430020aa9d900`. Model Router uses its own implementation and reduced scope. See [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).
