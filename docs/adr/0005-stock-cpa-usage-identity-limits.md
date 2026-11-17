# ADR 0005: Stock CPA usage identity limits

- Status: Accepted; supersedes [ADR 0004](0004-request-id-parent-provider-child-contract.md)
- Date: 2026-11-14

## Context

In stock CPA v7.3.9 (plugin schema 6), interceptors receive request identity and request metadata, but delivered usage records contain no Request ID, Trace ID, or endpoint that joins them to an inbound request. Lifecycle completion support does not supply a join key for provider usage callbacks; Model Combo does not advertise request lifecycle support. Plugin-only code cannot reliably populate new stored Request ID or API path values from those callbacks.

## Decision

- Keep stock CPA and the existing plugin ABI, database schema, and management schema version 2; do not require a fork, schema upgrade, or lifecycle negotiation.
- Do not infer identity using timing, credentials, model or body matching, FIFO, markers, hashes, or router IDs. Do not store request bodies, query strings, credentials, or completion errors.
- Preserve historical identity, endpoint, and session values for reading. Keep existing legacy `router_request_id` grouping, not as a claim of exact inbound identity. ADR 0003's legacy grouping remains valid.
- Provider usage callbacks remain the sole accounting input. No historical data, pricing, or preferences migration is required.
- Request ID and API path remain available as ordinary hideable columns with unchanged defaults and saved choices. Require at least one visible data column: reject all-hidden saves rather than forcing Request ID visible. Read legacy malformed preferences unchanged; the UI reveals the first defined column when all are hidden.

## Consequences

New stock usage callbacks do not reliably populate Request ID or API path, while historical values remain accessible. Existing router groups retain their attempt expansion without asserting an exact inbound-request hierarchy. Hiding Request ID must not make those attempts inaccessible; see [BDR 0005](../bdr/0005-hideable-historical-request-columns.md).
