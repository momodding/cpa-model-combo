# ADR 0004: Request ID parent/provider-child contract

## Status

Superseded by [ADR 0005](0005-stock-cpa-usage-identity-limits.md).

## Decision

Request details uses CPA's inbound `TraceID` as the authoritative parent `request_id`. Each completed inbound request is stored once in `request_sessions`; every delivered provider `usage_plugin` callback is a child/accounting row.

CPA plugin schema 7 adds `TraceID` and normalized `RequestEndpoint` to delivered usage records. Model Combo enables lifecycle parents only after schema-7 negotiation. Older hosts retain legacy request grouping: routed rows group by `router_request_id`; direct and unattributed rows remain standalone.

Lifecycle completion determines parent result, status, timestamps, and end-to-end latency. Provider callbacks retain provider/source/model metrics and are the sole input to overview and breakdown accounting. A completed parent can have zero children. No request body, query string, credential, or completion error is stored.

## Consequences

- Provider failover attempts are exact children of one inbound API request.
- A successful completion remains successful after an earlier provider 429.
- Request pagination and totals count parents; overview and grouping retain callback counts.
- New and legacy records never merge by inference.
- CPA schema 7 is required for exact parents; timing, body-hash, credential, and FIFO matching are prohibited.
