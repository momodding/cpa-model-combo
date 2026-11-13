# cpa-model-combo

[![test](https://github.com/momodding/cpa-model-combo/actions/workflows/test.yml/badge.svg)](https://github.com/momodding/cpa-model-combo/actions/workflows/test.yml)
[![release](https://github.com/momodding/cpa-model-combo/actions/workflows/release.yml/badge.svg)](https://github.com/momodding/cpa-model-combo/actions/workflows/release.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

`cpa-model-combo` is a native [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) model-routing and executor plugin. It routes configured model aliases through priority routing, round-robin routing, cooldown, and failover.

## Fork provenance

This project is forked from [markhuangai/cpa-plugin-model-router](https://github.com/markhuangai/cpa-plugin-model-router). Upstream MIT attribution and terms remain in [LICENSE](LICENSE).

The plugin does not call providers directly. For each selected physical model it calls CPA's host model executor, so CPA keeps control of provider credentials, protocol translation, proxy policy, request logging, and usage capture.

## Features

- Expose logical model aliases through CPA's model registry.
- Route an alias to an ordered priority pool or a round-robin pool.
- Assign round-robin weights to give models with multiple CPA providers a proportional outer share.
- Fail over on quota, rate-limit, auth, provider, server, and recognized transport failures.
- Cool failed targets in memory and skip them on later requests.
- Preserve a requested thinking suffix unless a target defines its own suffix.
- Rewrite non-streaming and streaming response model fields back to the requested alias.
- Retry a stream only before the first upstream payload is received.
- Preserve unchanged cooldown and round-robin state when CPA reconfigures the plugin.
- Configure ordered routes and target pools through a dedicated CPA management page.
- Track routed attempts and direct provider requests with separate router-model and provider-model identities.
- Persist request usage, minute aggregates, USD model prices, and dashboard preferences in a dedicated SQLite database with WAL enabled.
- Review tokens, estimated cost, latency, TTFT, throughput, failures, and pricing coverage without replacing visible data during refreshes.

## Compatibility

The module is built against `github.com/router-for-me/CLIProxyAPI/v7` v7.3.9 and requires CPA v7.3.9 or newer. That release preserves numeric HTTP statuses returned through native host callbacks so configured fallback rules can classify upstream errors correctly. CPA v7.2.143 introduced asynchronous native `usage_plugin` callbacks that survive request completion; older hosts can silently omit usage records. The plugin negotiates RPC schema v2 or v3 on older hosts and schema 7 when offered; schema v3 avoids resending the full request body with every streaming response chunk. Exact inbound request-ID hierarchy requires a host offering schema 7, including lifecycle and usage identity fields; schema-6 hosts retain legacy grouping. The CPA host must support native plugins, `model_router`, `executor`, `model_registrar`, request interception, `usage_plugin`, and the `host.model.*` callback methods. The configuration and usage page also requires CPA's `management_api` capability and plugin resource menus.

Build the plugin for the same operating system and architecture as CPA. A Go `c-shared` library is not portable across OS or CPU targets.

## Configuration

```yaml
plugins:
  enabled: true
  dir: "/absolute/path/to/plugins"
  configs:
    model-combo:
      enabled: true
      priority: 100
      data_path: "/CLIProxyAPI/plugins/cpa-model-combo.db"
      retention_days: 365
      routes:
        - alias: auto
          strategy: priority
          cooldown_seconds: 60
          targets:
            - model: gpt-5.4-mini
              weight: 1
            - model: claude-sonnet-4-5
              weight: 1
            - model: gemini-2.5-pro(8192)
              weight: 1

        - alias: balanced
          strategy: round-robin
          cooldown_seconds: 30
          targets:
            - model: provider-a/model
              weight: 3
            - model: provider-b/model
              weight: 1
```

`plugins.enabled` and `plugins.configs.model-combo.enabled` must both be true. Install only versioned `model-combo-v<version>` libraries with the host extension: `.so`, `.dylib`, or `.dll`; `v1.0.1` uses `model-combo-v1.0.1.<ext>`.

Only two usage-storage settings are supported:

| Field | Default | Meaning |
| --- | --- | --- |
| `data_path` | CPA `plugins/cpa-model-combo.db` | Dedicated SQLite database path. Explicit relative paths resolve from the CPA process working directory. |
| `retention_days` | `365` | UTC days of request details and aggregates to retain, from 1 through 3650. |

`data_path` accepts filesystem paths, not `file://` URIs. On Windows, use a relative path such as `plugins/cpa-model-combo.db`, an absolute path such as `D:/CLIProxyAPI/plugins/cpa-model-combo.db`, or a single-quoted YAML path such as `'D:\CLIProxyAPI\plugins\cpa-model-combo.db'`. Relative paths resolve from the CPA process working directory; do not add a slash before a Windows drive letter.

When `data_path` is omitted, the plugin locates the CPA root from the loaded library, then the CPA executable, then the current working directory. It stores the database at `<CPA root>/plugins/cpa-model-combo.db`; outside a detected CPA layout it uses `<working directory>/plugins/cpa-model-combo.db`. Existing `model-router.db` history is not migrated; set `data_path` explicitly to retain it.

Usage records are committed synchronously. On supported CPA versions, asynchronous native usage delivery survives request completion. Usage history, prices, and dashboard preferences survive a normal CPA restart when the same database file remains mounted and writable. Retention deletes expired rows; high-volume installations should monitor the database file and choose retention based on request rate and available storage.

SQLite storage schema 1 upgrades automatically to schema 2, adding request sessions without rewriting historical records. Make a consistent database backup including live WAL data before upgrading: a schema-1-only plugin cannot open the upgraded database, and rolling back to a backup loses history written since that backup. A separate database is another rollback option.

### Configuration UI

The plugin registers a **Model Combo** page in CPA's management frontend. Open the Plugins section and select **Model Combo**. Its management paths use `model-combo`; existing installs must rename `plugins.configs.cpa-model-combo` to `plugins.configs.model-combo`. No old-key alias exists. The database remains `cpa-model-combo.db`, preserving usage history.

Model discovery uses the management session to read CPA's configured client API keys, then keeps the first non-empty key in browser memory only while requesting `/v1/models`. The client key is not rendered or stored. If no client key is configured, the page attempts the model request without authorization for CPA installations without frontend authentication. Existing targets that are absent from the live catalog remain visible as disabled `<model> (unavailable)` choices until they are replaced, so loading the page never changes saved routes. New UI targets must be selected from the live catalog; custom or suffixed targets can still be managed through YAML or the Management API.

The fixed **Save changes** and **Discard and reload** dock slides into view only while the current draft differs from the last configuration loaded or saved through CPA. Reverting every edit hides it again; invalid numeric drafts keep it visible so validation feedback and discard remain available. The dock reserves enough page space to avoid covering the final route and moves notifications above itself.

**Save changes** checks duplicate aliases, recursive routes, empty pools, duplicate targets, and cooldown values with the plugin's Go configuration parser before applying a shallow patch through CPA. The patch updates `enabled`, `priority`, and `routes` without replacing plugin-store metadata or unrelated config fields. The same page is available directly at:

```text
/v0/resource/plugins/model-combo/config
```

The dashboard's shared, Configuration, and Usage tracking source lives under dashboard/; Go embeds these files in the plugin and assembles one HTML response when the plugin starts. CPA exposes that page as a public plugin resource so it can embed it in the frontend. Reading or changing configuration still requires the management key. The page reads CPAMC's persisted `cli-proxy-auth` value from same-origin browser storage; it does not change CPAMC's stored session. If **Remember password** is disabled, the persisted session has no key, so the page reveals a fallback key field. A fallback key is cached only in that tab's session storage and is removed when CPA rejects it. CPA's Management API must be enabled and reachable from the browser.

### Usage tracking

Usage tracking stores one row for each CPA usage record delivered for a physical routed attempt, including failed attempts before failover, and for each delivered direct-provider record. A routed row keeps the client-visible router alias in `router_model` and the physical CPA target in `provider_model`; direct rows have no router alias. This separation is used consistently in filters, summaries, grouped results, and request details.

With CPA plugin schema 7, Request details groups provider attempts under an inbound `request_id` parent; completed requests without provider callbacks remain visible. For example, request `1234` to `POST /v1/chat/completions` can show `gpt5.6-sol` returning 429 followed by `claude-sonnet-5.5` returning 200 under one successful parent. Request-page totals count parents, while overview and breakdown count provider callbacks. Older hosts and historical records without request IDs group routed attempts by `router_request_id`; direct and unattributed records remain standalone.

Request details includes Request ID, API path, and Requested model columns; Request ID is always visible. The usage management API uses `schema_version: 2`: request items expose `request_id`, `request_endpoint`, `requested_model`, `stream`, and provider `attempts`/`attempt_count` when available. See the usage tracking guide for ordering, filters, and legacy details.

The dashboard provides preset or custom time ranges, minute/hour/day trends, router/provider/source/tier/result filters, USD cost estimates, configurable columns, server-side sorting and pagination, and model pricing with optional models.dev synchronization. Both usage tables start at 50 rows, and Router model joins Provider, Source, Service tier, and Result in the hidden Usage breakdown defaults; existing saved column and page-size preferences remain authoritative. Token trends use packet columns, cost and efficiency use halo lines, and provider share uses a gapped donut. Every chart has stronger pointer and keyboard hover/focus states while preserving the existing tooltips and filter actions. Manual prices take precedence over synchronized catalog prices, and the pricing dialog closes after a successful save. Pricing units are USD per one million tokens; context tiers, service tiers, and cache accounting modes are supported.

Refreshes keep the previous dashboard visible while queries load. Starting a newer refresh aborts the older one, and only the newest generation may update the page. Automatic refresh runs every 15 seconds only while Usage tracking is selected and the document is visible. Reset deletes request sessions, provider history, and aggregates but preserves prices and dashboard preferences.

CPA's normalized `usage_plugin` records are the sole source of provider accounting. CPA supplies token counters, cache-read and cache-creation counters, timing, provider metadata, and failure state; the plugin correlates records to routed or direct markers and applies local pricing and aggregation. Schema-7 lifecycle completion additionally stores inbound request metadata and final outcome. CPA v7.2.142 and older can lose usage callbacks and are not supported; the plugin does not recreate missing records from response bodies.

See [docs/usage-tracking.md](docs/usage-tracking.md) for the data contract, operational guidance, API routes, and implementation map.

### Route fields

| Field | Required | Meaning |
| --- | --- | --- |
| `alias` | yes | Client-visible model name. Matching is case-insensitive. Thinking suffixes are not allowed on aliases. |
| `strategy` | no | `priority` by default, or `round-robin`. |
| `cooldown_seconds` | no | Seconds a failed target remains unavailable. Omitted or zero uses 60 seconds. |
| `targets` | yes | Ordered physical model target objects. Each object has a required `model` and an integer `weight` from 1 through 1000000; omitted weight defaults to 1. Empty and duplicate targets are rejected. |
| `models` | legacy | Legacy ordered string targets. Each target is normalized to weight 1; do not provide it together with `targets`. |

A route target cannot reference any configured route alias, including through a thinking suffix. This prevents recursive routes.

### Selection and failover

`priority` selects the first target that is not cooling down and ignores weights. `round-robin` advances through consecutive virtual slots according to each target's weight and skips all slots for targets that are cooling down. For example, weights `3:1` produce `A,A,A,B`; CPA then selects the actual credential configured behind each selected model.

The plugin fails over for these numeric statuses:

- `401`, `402`, `403`, `408`, `429`
- `404`, except request-scoped persisted-item misses such as `store=false`
- all `5xx` statuses

`400` and `422` are treated as request errors and returned immediately by default. CPA's structured `model_not_found` error reporting an unknown or missing provider is an exception: its `400` cools the unavailable target and moves to the next candidate. An explicit `fallback.no_fallback_on_status: [400]` also excludes this exception. Cancellation and deadline errors never fail over. When the host callback loses a numeric status, the plugin fails over only for recognized quota, auth-unavailable, model/provider-unavailable, timeout, DNS, connection, broken-pipe, reset, or EOF messages. Ambiguous errors stop the route to avoid repeating a bad request against every provider.

A stream can move to the next target only when start-up fails before any payload bytes arrive. Startup errors preserve their HTTP status and error details before the stream begins. A failure after payload arrival cools the target but is returned to the client without splicing a second provider stream into the first, including when an incomplete frame is still buffered.

### Thinking suffixes

If a client asks for `auto(high)` and the selected target is `gpt-5.4-mini`, CPA executes `gpt-5.4-mini(high)`. If the selected target is already `gemini-2.5-pro(8192)`, the target suffix wins.

Successful response fields such as `model`, `modelVersion`, `response.model`, and `message.model` are rewritten to the exact requested alias, including its suffix.

## Migrating From The Fork Configuration

Move the old top-level list under the plugin config and rename the canonical cooldown field from a hyphen to an underscore.

```yaml
# Before: Z-M-Huang/CLIProxyAPI fork
model-routes:
  - alias: auto
    strategy: priority
    cooldown-seconds: 60
    models:
      - gpt-5.4-mini
      - claude-sonnet-4-5
```

```yaml
# After: upstream CPA plus this plugin
plugins:
  enabled: true
  dir: "plugins"
  configs:
    cpa-model-combo:
      enabled: true
      routes:
        - alias: auto
          strategy: priority
          cooldown_seconds: 60
          targets:
            - model: gpt-5.4-mini
              weight: 1
            - model: claude-sonnet-4-5
              weight: 1
```

For staged migration, the plugin accepts `model-routes` instead of `routes` and `cooldown-seconds` instead of `cooldown_seconds` inside its config. Do not provide both forms of either field; registration fails rather than choosing one silently.

The fork-specific `/v0/management/model-routes` endpoints are not part of this plugin. The configuration page uses CPA's generic plugin endpoints, which remain available for automation:

```text
GET   /v0/management/plugins/model-combo/config
PUT   /v0/management/plugins/model-combo/config
PATCH /v0/management/plugins/model-combo/config
PATCH /v0/management/plugins/model-combo/enabled
```

## Build And Test

Go 1.26 and a working C compiler are required.

```bash
make check
make build
```

`make build` writes the host-platform library to `dist/cpa-model-combo.<ext>`. The default suite covers strict config parsing, priority and round-robin state, reconfiguration, failure classification, header sanitization, model rewriting, non-stream failover, stream boundaries, CPA usage attribution, persistence, pricing, management endpoints, the management page, and native RPC registration.

Run the opt-in black-box test against a local CPA source checkout:

```bash
CPA_SOURCE=../CLIProxyAPI \
  go test -tags=integration ./... -count=1 -v
```

The black-box test builds CPA and the native plugin in a temporary directory, starts logical providers on a local mock OpenAI-compatible server, loads the library through CPA, and verifies all of the following without real provider credentials. Public CI runs it against the minimum supported CPA v7.3.9 commit:

- the logical alias appears in `/v1/models`;
- the cpa-model-combo menu and parser-backed validation endpoint are available;
- a `429` from the first target fails over to the second target;
- the client sees the requested alias in the response;
- the failed target remains on cooldown for the next request;
- routed and direct streaming and non-streaming requests remain distinct, and every usage callback delivered by CPA is persisted once;
- cache-creation counters delivered by CPA remain nonzero in the stored request and cost breakdown when the provider reports them;
- usage history, model prices, and dashboard preferences survive a CPA restart.

The test currently runs on Linux and macOS.

The browser E2E test exercises the dashboard against a disposable local CPA instance. It changes route configuration, pricing, and dashboard preferences, then resets usage history, so do not point it at a shared or production instance. Install its pinned browser dependency with `npm ci`, then run:

```bash
DASHBOARD_URL=http://127.0.0.1:8317/v0/resource/plugins/model-combo/config \
DASHBOARD_MANAGEMENT_KEY='replace-with-disposable-instance-key' \
npm run test:e2e
```

Set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` when Chromium is not available at `/usr/bin/google-chrome`.

### Manual local installation

1. Run `make build` on the CPA host machine.
2. Copy `dist/cpa-model-combo.so` on Linux, `dist/cpa-model-combo.dylib` on macOS, or `dist/cpa-model-combo.dll` on Windows into the configured `plugins.dir`. CPA also scans `plugins.dir/<goos>/<goarch>`.
3. Add `plugins.configs.cpa-model-combo.enabled: true`. Add routes in YAML or through the cpa-model-combo management page. Every target model must already be routable by CPA.
4. Start CPA with `go run ./cmd/server --config /path/to/config.yaml` or your normal CPA binary.
5. Verify that the alias is listed:

```bash
curl -fsS http://127.0.0.1:8317/v1/models \
  -H "Authorization: Bearer $CPA_API_KEY" \
  | jq '.data[] | select(.id == "auto")'
```

6. If the Management API is enabled, verify plugin registration:

```bash
curl -fsS http://127.0.0.1:8317/v0/management/plugins \
  -H "Authorization: Bearer $MANAGEMENT_PASSWORD" \
  | jq '.plugins[] | select(.id == "cpa-model-combo")'
```

`registered`, `enabled`, and `effective_enabled` should all be true. A copied library is trusted in-process code; only load artifacts you built or verified.

## Current ABI Limits

- Routed token-count requests return HTTP `501` with code `model_route_count_tokens_unsupported`. The current host callback contract exposes model execution but not routed token counting. Returning an explicit error avoids reporting a false zero.
- Host execution errors do not expose the upstream `Retry-After` header to the plugin. Cooldowns therefore use `cooldown_seconds`, even when a provider asks for a longer delay.
- Nested host execution has no explicit requested-router metadata field. The plugin correlates CPA usage records to a short-lived in-memory marker; a record that cannot be correlated is stored as `unattributed`.
- Status-less errors lose some structured failure information at the ABI boundary. The failure classifier is conservative and may stop on an unfamiliar transient error until that error is added explicitly.
- Cooldown and round-robin state is process-local. It survives config reconfiguration when a route is unchanged, but not a CPA process restart.

## Security Notes

The plugin removes client `Authorization`, proxy authorization, cookies, host/content-length, and headers whose names contain API key, token, secret, or credential markers before calling `host.model.*`. CPA selects and applies the target provider credential. Other request headers and query parameters are preserved.

Usage storage never includes prompt text, request or response bodies, failure bodies, lifecycle completion errors, response headers, or raw API keys. Sessions store request ID, endpoint, requested and router models, attribution, stream flag, outcome, status, timestamps, and latency; provider records can store a short masked API-key display value. The plugin stores the endpoint string CPA supplies and adds no query strings. Source URLs are stripped of user info, query strings, and fragments, while values that resemble credentials fall back to a provider label.

The database is not encrypted by the plugin. Protect the configured path with filesystem permissions and volume access controls. Native plugins execute in the CPA process with CPA's permissions. Only load artifacts you built or verified, and do not put credentials in route model names.

## Publishing And Plugin Store Registration

The first release is `v0.1.0`; workflow assets are:

```text
cpa-model-combo_0.1.0_linux_amd64.zip
cpa-model-combo_0.1.0_linux_arm64.zip
cpa-model-combo_0.1.0_darwin_amd64.zip
cpa-model-combo_0.1.0_darwin_arm64.zip
cpa-model-combo_0.1.0_windows_amd64.zip
checksums.txt
```

Each ZIP contains one root-level library named `cpa-model-combo.so`, `cpa-model-combo.dylib`, or `cpa-model-combo.dll`.

Manual handoff sequence:

1. Rename or create `https://github.com/momodding/cpa-model-combo`, then update shared `origin` after the rename.
2. Push merged commits and create/push tag `v0.1.0`.
3. Let the release workflow publish assets; verify five ZIPs and `checksums.txt`.
4. Open a `router-for-me/CLIProxyAPI-Plugins-Store` PR changing only `registry.json` with release evidence:

```json
{
  "id": "cpa-model-combo",
  "name": "cpa-model-combo",
  "description": "Routes logical model aliases across priority and weighted round-robin target pools with cooldown, failover, and usage tracking.",
  "author": "momodding",
  "version": "0.1.0",
  "repository": "https://github.com/momodding/cpa-model-combo",
  "homepage": "https://github.com/momodding/cpa-model-combo",
  "license": "MIT",
  "tags": ["Router"]
}
```

After the registry PR is merged, install through CPA management UI or `POST /v0/management/plugin-store/cpa-model-combo/install`, then configure routes from the **cpa-model-combo** page or generic config API.

## License

MIT. See [LICENSE](LICENSE). The usage dashboard and pricing workflow were adapted from AITNR's MIT-licensed CAP Token Usage Tracker; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
