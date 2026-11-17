# BDR 0007: Cooldown skips and target account selection

## Status

Accepted by user approval of the cooldown, target accounts, and usage KPIs plan. Supersedes BDR 0006's route-level selection and verified-membership requirement for unknown model support, and its prohibition on unreported children only for explicitly marked non-dispatched skips. BDR 0001 and BDR 0004's provider-child descriptions are extended by this separate skip category; their provider accounting and parent hierarchy remain unchanged.

## Behavior

Each target owns an ordered account selection. Legacy route selections are copied only into targets without their own selection; an explicit empty selection allows all accounts. Missing or disabled selections remain visible and prevent saving. Empty host model lists mean support unknown, not entitlement or confirmed compatibility; selected credentials may be attempted, but populated mismatching lists remain ineligible.

Known cooling targets and accounts may be skipped without consuming a dispatched target attempt. Request Details shows these as cooling down and not dispatched, separately from delivered provider executions, with no fabricated execution ID, failure, latency, tokens or cost. Provider callbacks remain the sole accounting input; parent outcome remains completion-derived and ambiguous identities are never guessed.

Native callback errors that cannot be distinguished from provider failures are not displayed as non-dispatched cooldown skips. Known host-summary and plugin-local deadlines still enable preflight skips.

Target accounts are available in a collapsed accessible disclosure; target order can be changed with drag and keyboard controls and persists with each target's accounts. Valid empty usage KPIs show zero or None; unavailable data shows an em dash, and refresh failures retain prior values.

The approved delivery is a verified marketplace pull request from an isolated worktree; failed or blocked required verification prevents commit, push and PR.
