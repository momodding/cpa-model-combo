# BDR 0005: Hideable historical request columns

- Status: Accepted; supersedes [BDR 0004](0004-request-details-parent-child-behavior.md)
- Date: 2026-11-14

## Decision

Request ID and API path are ordinary hideable Request details columns. Their historical values remain readable, but stock CPA usage callbacks cannot reliably populate them for new records. No heuristic identity binding is performed; see [ADR 0005](../adr/0005-stock-cpa-usage-identity-limits.md).

## Observable behavior

- Existing column visibility defaults and saved choices remain unchanged, including an explicitly empty hidden list. Neither column is automatically hidden based on record contents.
- Routed attempts expand within legacy router groups; those groups do not establish an exact inbound-request hierarchy. Existing historical sessions and direct one-child expansion remain readable.
- When Request ID is visible, the parent attempt toggle and child attempt label appear in that cell. Otherwise they appear in the first visible data cell, before its existing formatted value or status badge. Rows without children have no toggle.
- At least one data column must remain visible. The last visible column cannot be hidden in the UI, and an all-hidden save is rejected without replacing the prior preferences. Legacy all-hidden preferences are read unchanged, while the UI reveals the first defined column.
- Expansion remains accessible through its existing controls and keys; no fixed utility column or new preference is introduced.
