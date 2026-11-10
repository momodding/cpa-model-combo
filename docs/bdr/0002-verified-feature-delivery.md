# BDR 0002: Verified feature delivery

- Status: Accepted
- Date: 2026-11-10

## Decision

Users receive changes through a verified pull request. Every feature starts in its own worktree and branch; every defect investigation starts on a fixing branch. Changes are committed and pushed only after all relevant available checks pass.

## Observable behavior

- `main` is not edited directly for feature work.
- Independent features can proceed in separate worktrees.
- A failed or unavailable required check blocks commit, push, and pull-request delivery.
- A change that conflicts with an ADR or BDR pauses for explicit user direction, then records that direction before implementation.

## Rationale

This protects stable code from incomplete work while preserving a visible, user-approved decision history.
