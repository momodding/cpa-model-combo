# ADR 0007: Cooldown skips and target account routing

## Status

Accepted by user approval of the cooldown, target accounts, and usage KPIs plan. Supersedes ADR 0006's prohibition on synthetic children and its route-level account ownership only as described below; supersedes the historical no-storage-change consequences of ADR 0001 and ADR 0003 for this migration. ADR 0004 and ADR 0005's accounting and identity safeguards remain applicable.

## Decision

- Account allowlists belong to targets. A missing target list inherits a legacy route list during loading; an explicit empty list is unrestricted. Save only target-level lists. Restricted execution never escapes its target's ordered list; unrestricted execution remains host-selected.
- An originally empty host model list means unknown support and permits attempting the configured target on a selected enabled credential. A nonempty list remains authoritative even if filtering removes its entries. Registry membership is not entitlement; catalog failures fail closed.
- Combine plugin-local scoped cooldowns with available host credential summary deadlines. A known pre-dispatch cooldown skip is a separate non-dispatched routing event, not a provider execution. Store skips separately in transactional SQLite schema 4; provider callbacks alone supply usage, failures, tokens and cost. Preserve lifecycle identity and collision handling.
- CPA v8.0.37 host callback RPC loses the native cooldown type and Retry-After. As separately approved, use known host-summary and plugin-local cooldowns for preflight; treat ambiguous callback errors as ordinary failures, never as proven non-dispatch.
- If trusted native executor correlation is unavailable, use an ephemeral server-issued private header to resolve the exact trace and lifecycle request. Overwrite client values, remove the header before provider dispatch, never persist the token, and verify the transport on stock CPA v8.0.37. Never infer identity from timing or credentials.
- Keep the plugin ABI and CPA unchanged. Rollback requires a consistent pre-upgrade configuration and database backup and matching previous artifacts; old writers must not write the migrated database.
- Deliver from an isolated feature worktree after relevant verification passes, via a marketplace pull request, without direct merge to main. Failed or blocked required verification prevents commit, push and PR.
