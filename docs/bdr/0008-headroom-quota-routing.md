# BDR 0008: Headroom quota-aware routing

## Status

Accepted by approval of the headroom quota-aware routing plan and the explicit decision to supersede BDR 0007's host-selected unrestricted execution for headroom routes only.

## Behavior

Headroom routes order eligible target and credential pairs by their lowest applicable remaining quota. Exhausted pairs are skipped; unknown quota follows the configured priority, exclude or last policy. Session affinity may move an eligible bound credential to the front. Each attempt pins its selected credential; existing eligibility, cooldown, attempt limits, fallback and usage accounting remain in force.

Quota comes from supported provider or plugin quota APIs through the local management API, never from usage callbacks or passive headers. Unavailable capabilities, missing management credentials and stale data are reported as unknown. The management key and provider tokens are never exposed in diagnostics. The dashboard provides route configuration, polling settings and read-only quota status.

Other routing strategies retain their existing behavior. Delivery requires verification and a pull request; pushing is prohibited in this session.
