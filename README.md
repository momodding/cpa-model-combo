# cpa-model-combo

[![test](https://github.com/momodding/cpa-model-combo/actions/workflows/test.yml/badge.svg)](https://github.com/momodding/cpa-model-combo/actions/workflows/test.yml)
[![release](https://github.com/momodding/cpa-model-combo/actions/workflows/release.yml/badge.svg)](https://github.com/momodding/cpa-model-combo/actions/workflows/release.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

`cpa-model-combo` is a native [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) model-routing and executor plugin. It routes configured model aliases through configurable strategies, cooldown, and failover.

## Fork provenance

This project is forked from [markhuangai/cpa-plugin-model-router](https://github.com/markhuangai/cpa-plugin-model-router). Upstream MIT attribution and terms remain in [LICENSE](LICENSE).

The plugin does not call providers directly. For each selected physical model it calls CPA's host model executor, so CPA keeps control of provider credentials, protocol translation, proxy policy, request logging, and usage capture.

## Features

- Expose logical model aliases through CPA's model registry.
- Route an alias to an ordered priority pool, a round-robin pool, or a quota-aware headroom pool.
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

The module uses `github.com/router-for-me/CLIProxyAPI/v8` v8.0.37; deploy it with stock CPA v8.0.37 and a matching native plugin artifact. CPA must support native plugins, model routing and execution, request lifecycle and usage callbacks, host model and auth callbacks, and management resources. This is a plugin-only integration; no CPA source changes are required.

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
      quota_polling:
        refresh_seconds: 60
        idle_seconds: 600
        ttl_seconds: 60
        max_stale_seconds: 300
        fetch_timeout_seconds: 5
        max_concurrent: 3
        cold_wait_ms: 1500
      routes:
        - alias: auto
          strategy: priority
          cooldown_seconds: 60
          allowed_accounts: ["credential-id-a", "credential-id-b"]
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

        - alias: most-headroom
          strategy: headroom
          unknown_quota: priority
          targets:
            - model: gpt-5.4-mini
            - model: claude-sonnet-4-5
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

Round-robin weights are manual outer-routing shares. For priority and round-robin routes, CPA selects the credential for unrestricted routes; restricted routes pin execution to an allowed credential. Headroom routes select and pin an eligible credential themselves.

### Upgrading to CPA v8 and rolling back

Back up configuration and SQLite consistently using SQLite's backup facility or after a clean shutdown, including any required WAL data. Stop the old plugin before upgrading; never run old and new plugin writers against the same database. Upgrade CPA externally to stock v8.0.37 and deploy its matching plugin artifact together. The transactional schema-3 migration retains existing request history, minute aggregates, prices, and preferences.

For rollback, stop the plugin and restore the matching old CPA and plugin artifacts and the pre-upgrade configuration and database backup. Keep post-upgrade data separately. Do not point older binaries at a schema-3 database, decrement its version, or delete sessions to force a downgrade. See [docs/usage-tracking.md](docs/usage-tracking.md#upgrading-to-schema-3-and-rolling-back).

### Configuration UI

The plugin registers a **Model Combo** page in CPA's management frontend. Open the Plugins section and select **Model Combo**. Its management paths use `model-combo`; existing installs must rename `plugins.configs.cpa-model-combo` to `plugins.configs.model-combo`. No old-key alias exists. The database remains `cpa-model-combo.db`, preserving usage history. Configuration provides route order, aliases, strategies, cooldowns, targets, weights, and a provider-grouped account selector. The management-authenticated account catalog supplies credential IDs, safe labels, status, and registered models; the browser does not retrieve client API keys for model discovery.

No accounts selected means all accounts allowed. With selections, model choices are the union of their registered models; otherwise they come from the full catalog. Registry membership indicates compatibility, not verified entitlement: entitlement is shown as unknown. Disabled or stale selections remain visible for removal, and changing accounts does not silently remove incompatible targets. Catalog loading or failure is shown explicitly; restricted configurations are validated against a fresh catalog and fail closed if it is unavailable.

The fixed **Save changes** and **Discard and reload** dock slides into view only while the current draft differs from the last configuration loaded or saved through CPA. Reverting every edit hides it again; invalid numeric drafts keep it visible so validation feedback and discard remain available. The dock reserves enough page space to avoid covering the final route and moves notifications above itself.

**Save changes** checks duplicate aliases, recursive routes, empty pools, duplicate targets, and cooldown values with the plugin's Go configuration parser before applying a shallow patch through CPA. The patch updates `enabled`, `priority`, `routes`, and `quota_polling` without replacing plugin-store metadata or unrelated config fields. The same page is available directly at:

```text
/v0/resource/plugins/model-combo/config
```

The dashboard's shared, Configuration, and Usage tracking source lives under dashboard/; Go embeds these files in the plugin and assembles one HTML response when the plugin starts. CPA exposes that page as a public plugin resource so it can embed it in the frontend. Reading or changing configuration still requires the management key. The page reads CPAMC's persisted `cli-proxy-auth` value from same-origin browser storage; it does not change CPAMC's stored session. If **Remember password** is disabled, the persisted session has no key, so the page reveals a fallback key field. A fallback key is cached only in that tab's session storage and is removed when CPA rejects it. CPA's Management API must be enabled and reachable from the browser.

### Usage tracking

Usage tracking stores one row for every delivered CPA execution usage record, including failures and direct requests. A routed row keeps the client-visible alias in `router_model` and the physical target in `provider_model`. Request Details groups delivered rows under their exact inbound trace, ordered by start time with stable ties. Lifecycle-only failures remain visible as parents without invented children. Parent outcome comes from inbound completion, not the last child. Older identity-less records retain legacy correlation; ambiguous traces are not silently merged.

The dashboard provides preset or custom time ranges, minute/hour/day trends, router/provider/source/tier/result filters, USD cost estimates, configurable columns, server-side sorting and pagination, and model pricing with optional models.dev synchronization. Both usage tables start at 50 rows, and Router model joins Provider, Source, Service tier, and Result in the hidden Usage breakdown defaults; existing saved column and page-size preferences remain authoritative. Token trends use packet columns, cost and efficiency use halo lines, and provider share uses a gapped donut. Every chart has stronger pointer and keyboard hover/focus states while preserving the existing tooltips and filter actions. Manual prices take precedence over synchronized catalog prices, and the pricing dialog closes after a successful save. Pricing units are USD per one million tokens; context tiers, service tiers, and cache accounting modes are supported.

Refreshes keep the previous dashboard visible while queries load. Starting a newer refresh aborts the older one, and only the newest generation may update the page. Automatic refresh runs every 15 seconds only while Usage tracking is selected and the document is visible. Reset deletes request sessions, provider history, and aggregates but preserves prices and dashboard preferences.

CPA's normalized `usage_plugin` records are the sole source of execution usage and token totals. Internal attempts without a delivered record are not reconstructed from response bodies or lifecycle errors. Parent endpoints are captured from CPA's inbound `request_path` metadata and may be route templates rather than literal URLs.

See [docs/usage-tracking.md](docs/usage-tracking.md) for the data contract, operational guidance, API routes, and implementation map.

### Route fields

| Field | Required | Meaning |
| --- | --- | --- |
| `alias` | yes | Client-visible model name. Matching is case-insensitive. Thinking suffixes are not allowed on aliases. |
| `strategy` | no | `priority` by default; also accepts `round-robin`, `random`, `last_known_good`, or `headroom`. |
| `unknown_quota` | no | Headroom routes only: `priority` (default), `last`, or `exclude`; see below. |
| `cooldown_seconds` | no | Seconds a failed target remains unavailable. Omitted or zero uses 60 seconds. |
| `allowed_accounts` | no | Ordered, unique, case-sensitive credential IDs. Omitted or empty means unrestricted; clearing the selection removes the restriction. |
| `targets` | yes | Ordered physical model target objects. Each object has a required `model` and an integer `weight` from 1 through 1000000; omitted weight defaults to 1. Empty and duplicate targets are rejected. |
| `models` | legacy | Legacy ordered string targets. Each target is normalized to weight 1; do not provide it together with `targets`. |

A route target cannot reference any configured route alias, including through a thinking suffix. This prevents recursive routes.

### Selection and failover

`priority` selects the first target that is not cooling down and ignores weights. `round-robin` advances through consecutive virtual slots according to each target's weight and skips cooling targets. For example, weights `3:1` produce `A,A,A,B`. Unrestricted routes leave credential selection to CPA. Restricted routes use a fresh account catalog, try eligible enabled credentials in saved order with the credential ID and provider pinned, and never fall back to an unselected account. A missing catalog or no eligible account fails closed. Retryable failures advance to the next permitted account and then target; a target is cooled when all its eligible accounts fail retryably. Nonretryable failures and cancellation stop immediately.

The plugin fails over for these numeric statuses:

- `401`, `402`, `403`, `408`, `429`
- `404`, except request-scoped persisted-item misses such as `store=false`
- all `5xx` statuses

`400` and `422` are treated as request errors and returned immediately by default. CPA's structured `model_not_found` error reporting an unknown or missing provider is an exception: its `400` cools the unavailable target and moves to the next candidate. An explicit `fallback.no_fallback_on_status: [400]` also excludes this exception. Cancellation and deadline errors never fail over. When the host callback loses a numeric status, the plugin fails over only for recognized quota, auth-unavailable, model/provider-unavailable, timeout, DNS, connection, broken-pipe, reset, or EOF messages. Ambiguous errors stop the route to avoid repeating a bad request against every provider.

A stream can move to another permitted account or target only when start-up fails before any payload bytes arrive. Startup errors preserve their HTTP status and error details before the stream begins. A failure after payload arrival is returned without splicing a second provider stream into the first, including when an incomplete frame is still buffered.

### Quota-aware headroom

Headroom ranks eligible target/credential pairs by their lowest applicable remaining quota fraction, highest first. Account-wide windows and windows matching the model family apply; unrelated windows do not. A depleted applicable window excludes the pair. Partial snapshots are scored using their parsed windows; unlimited snapshots score 1 and follow known or partial snapshots with the same score. Ties otherwise retain target and account order. Cooldowns, account restrictions, fallback and attempt limits still apply. A valid session affinity binding can move its eligible credential to the front.

`unknown_quota` controls candidates whose quota is unavailable, stale, pending, unsupported, or has no applicable window:

- `priority` (default) and `last` currently both place unknown candidates after scored candidates, in target/account order. If all candidates are unknown, this is priority order.
- `exclude` removes unknown candidates. If no candidates remain, the route returns the existing all-unavailable error.

Quota is fetched through CPA's local management API, not inferred from usage callbacks or response headers. Set these environment variables on the CPA process:

| Variable | Meaning |
| --- | --- |
| `MODEL_COMBO_MANAGEMENT_KEY` | Required management API key. If absent, quota is unknown with reason `management_key_missing`. Do not put the key in plugin YAML. |
| `MODEL_COMBO_MANAGEMENT_URL` | Defaults to `http://127.0.0.1:8317`. Only HTTP(S) URLs with host `127.0.0.1`, `::1`, or `localhost` are accepted; paths other than `/`, credentials, queries, fragments, and redirects are not supported. An invalid URL gives reason `management_url_invalid`. |

CPA does not expose a management API callback or its listener address to the plugin, so a reachable loopback listener and the environment key are required. The key is sent to the local management API, not to providers or dashboard diagnostics. CPA substitutes provider tokens for the `$TOKEN$` placeholder in the fixed upstream requests.

| Provider | Quota support |
| --- | --- |
| Claude | OAuth usage: five-hour and seven-day account windows, plus Opus and Sonnet windows where present. |
| Codex | Usage primary and secondary windows; the account ID header is included when available. |
| Antigravity | Model-family quota buckets; requires `project_id` in the credential catalog. If missing, quota is unknown (`missing_project_id`). |
| Other providers, including Gemini CLI, Kimi, xAI, Devin, Meta, API-key and plugin providers | Unknown (`unsupported_provider`). The typed CPA auth catalog does not expose plugin quota capabilities or quota plugin IDs, so plugin quota endpoints cannot currently be discovered. |

The optional `quota_polling` block in the example shows all defaults. Every value must be positive, and `ttl_seconds` must not exceed `max_stale_seconds`.

| Setting | Default | Meaning |
| --- | --- | --- |
| `refresh_seconds` | 60 | Background refresh interval for active credentials. |
| `idle_seconds` | 600 | Remove credentials unused for longer than this interval. |
| `ttl_seconds` | 60 | Age at which a request triggers a refresh. |
| `max_stale_seconds` | 300 | Maximum usable snapshot age and retry-backoff cap. Older snapshots become unknown. |
| `fetch_timeout_seconds` | 5 | Timeout for each shared quota fetch. |
| `max_concurrent` | 3 | Maximum simultaneous quota fetches. |
| `cold_wait_ms` | 1500 | Maximum wait for initially missing snapshots before routing with available data. |

Polling starts lazily for headroom requests and stops when no active credentials remain or no headroom routes are configured. Transient failures retain the previous snapshot until it becomes too stale and back off; authentication, unsupported and parse errors produce unknown quota. The Configuration page shows quota diagnostics, including state, windows, fetch age and error reasons, and refreshes the panel on opening and saving.

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
GET   /v0/management/plugins/model-combo/accounts
PATCH /v0/management/plugins/model-combo/config
PATCH /v0/management/plugins/model-combo/enabled
```

## Build And Test

Go 1.26 and a working C compiler are required.

```bash
make check
make build
```

`make build` writes the host-platform library to `dist/model-combo-v1.0.1.<ext>` by default. The default suite covers strict config parsing, priority and round-robin state, reconfiguration, failure classification, header sanitization, model rewriting, non-stream failover, stream boundaries, CPA usage attribution, persistence, pricing, management endpoints, the management page, and native RPC registration.

Run the opt-in black-box test against a local CPA source checkout:

```bash
CPA_SOURCE=../CLIProxyAPI \
  go test -tags=integration ./... -count=1 -v
```

The black-box test builds CPA and the native plugin in a temporary directory, starts logical providers on a local mock OpenAI-compatible server, loads the library through CPA, and verifies behavior without real provider credentials. Public CI pins CPA v8.0.37:

- the logical alias appears in `/v1/models`;
- the Model Combo menu and parser-backed validation endpoint are available;
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
DASHBOARD_E2E_ALLOW_MUTATIONS=1 \
npm run test:e2e
```

Set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` when Chromium is not available at `/usr/bin/google-chrome`.

### Manual local installation

1. Run `make build` on the CPA host machine.
2. Copy `dist/model-combo-v1.0.1.so` on Linux, `dist/model-combo-v1.0.1.dylib` on macOS, or `dist/model-combo-v1.0.1.dll` on Windows into the configured `plugins.dir`. CPA also scans `plugins.dir/<goos>/<goarch>`.
3. Add `plugins.configs.model-combo.enabled: true`. Add routes in YAML or through the Model Combo management page. Every target model must already be routable by CPA.
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
  | jq '.plugins[] | select(.id == "model-combo")'
```

`registered`, `enabled`, and `effective_enabled` should all be true. A copied library is trusted in-process code; only load artifacts you built or verified.

## Current ABI Limits

- Routed token-count requests return HTTP `501` with code `model_route_count_tokens_unsupported`. The current host callback contract exposes model execution but not routed token counting. Returning an explicit error avoids reporting a false zero.
- Host execution errors do not expose the upstream `Retry-After` header to the plugin. Cooldowns therefore use `cooldown_seconds`, even when a provider asks for a longer delay.
- Trace IDs join delivered executions to inbound lifecycle sessions when unambiguous. Identity-less historical records use legacy marker correlation and can remain unattributed; internal executions without a usage callback cannot be displayed as children.
- Status-less errors lose some structured failure information at the ABI boundary. The failure classifier is conservative and may stop on an unfamiliar transient error until that error is added explicitly.
- Cooldown and round-robin state is process-local. It survives config reconfiguration when a route is unchanged, but not a CPA process restart.

## Security Notes

The plugin removes client `Authorization`, proxy authorization, cookies, host/content-length, and headers whose names contain API key, token, secret, or credential markers before calling `host.model.*`. CPA selects and applies the target provider credential. Other request headers and query parameters are preserved.

Usage storage never includes prompt text, request or response bodies, failure bodies, lifecycle completion errors, response headers, raw API keys, or arbitrary credential metadata. It can retain safe credential IDs, indexes, sanitized label/email snapshots, and a short masked client-key display value. A snapshot can remain after credential removal; unavailable labels are not guessed. The plugin does not add query strings to stored endpoints. Source URLs are stripped of user info, query strings, and fragments, while values that resemble credentials fall back to a provider label.

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
