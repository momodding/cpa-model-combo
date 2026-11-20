# ADR 0008: Headroom quota-aware routing

## Status

Accepted by approval of the headroom quota-aware routing plan and the explicit decision to supersede ADR 0007's host-selected unrestricted execution for headroom routes only.

## Decision

Headroom routes rank preflight-eligible target and credential pairs by quota remaining, pinning the selected credential and provider even for unrestricted targets. Existing strategies retain their current execution behavior. Cooldowns, target account restrictions, fallback classification and usage-callback accounting remain authoritative.

A plugin-owned HTTP client queries the loopback management API using a separately configured bearer key. Only fixed provider URLs may be sent through the management API. Tokens and management keys must not appear in diagnostics or logs. Quota is not inferred from usage or response headers. Missing, unsupported or stale quota is unknown, subject to the route's configured policy.

The v8.0.37 auth list exposes project ID but not plugin quota capability or plugin ID; absent metadata makes plugin quota unknown. Credential-bearing auth retrieval is not used for capability discovery. Polling is shared per credential, bounded, lazy, and disabled when no headroom routes remain.

Deliver in an isolated feature worktree. The session prohibits pushing; local verification and any permitted commit do not replace the required verified pull request.
