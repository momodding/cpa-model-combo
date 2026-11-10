# ADR 0003: v1.0.1 identity, packaging, and request contract

- Status: Accepted
- Date: 2026-11-10

## Context

Release v1.0.1 corrects the CPA-visible identity, native artifact naming, routed request parent metadata, fallback-status presentation, and management toggle layout without changing ABI exports, database storage, or routing policy.

## Decision

- Display `Model Combo`; technical plugin ID, config key, and management path `model-combo`.
- Retain repository/module `cpa-model-combo` and default database `cpa-model-combo.db`; provide no identifier alias. Existing installs rename their config key.
- Ship exactly one native runtime entry per target: `model-combo-v<version>.<ext>`, and archives named `model-combo_<version>_<goos>_<goarch>.zip`.
- Group routed attempts under a parent identified as `routed`, with the first ordered routed model and the shared router request ID. Preserve ordered provider attempts, callback-level overview counts, direct calls, and ADR 0001 logical-request counts.
- Use one accessible toggle structure for all management toggles and show inherited fallback defaults when the configured list is omitted or empty.

## Consequences

CPA derives `model-combo` from the versioned entry through its supported `-v` delimiter. No stable or old-ID runtime alias exists. The config-key cutover preserves usage history because the database filename is unchanged.
