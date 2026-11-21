# ADR 0009: Quota tracking tab and numeric unknown-quota score

## Status

Accepted by the user's decisions on the quota-tracking plan: configured unknown score of 50%, no stale scoring, and a separate Quota tracking tab. Amends ADR 0008, which left unknown quota to the per-route `unknown_quota` policy and kept serving observations until `max_stale_seconds`.

## Decision

- `quota_polling.default_unknown_quota_percent` (default 50, finite, 0-100) is the routing score of a candidate with unknown quota. It is a routing input, never a provider measurement.
- Headroom ranking sorts every candidate by score descending: known remaining quota, or the configured score when unknown. Ties prefer known over unknown, then limited over unlimited. The route policy `last` still ranks unknown after known; `exclude` still skips unknown; `priority` ranks by score.
- An observation older than `ttl_seconds` is unknown. Last-known quota is never scored. `max_stale_seconds` only caps polling retry backoff; the dashboard labels it "Maximum backoff (seconds)".
- Polling settings, the unknown score and quota status move from the Configuration tab to a Quota tracking tab. They remain part of the same saved configuration, so the save dock and Ctrl+S work on both tabs.

## Consequences

Account restrictions, cooldowns, fallback, affinity and the loopback-only fixed-destination provider access from ADR 0008 are unchanged. Operators who relied on a stale reading steering traffic now get the configured score once the TTL passes.
