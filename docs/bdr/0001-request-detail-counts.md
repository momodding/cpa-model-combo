# BDR 0001: Routed fallback appears as one request

- Status: Accepted
- Date: 2026-11-09

## Decision

A user-facing usage request is one logical routed request even when failover invokes multiple providers. Request details show one parent row with ordered attempt children. Direct calls remain one row each.

## Observable behavior

For the CPA smoke scenario:

- six provider usage callbacks contribute to overview and grouped accounting;
- five logical requests appear in `/usage/requests` and dashboard request pagination;
- the failed provider is visible in the successful routed request's `attempts` list.

## Rationale

Users need to see one routed request and its failover path, not duplicate requests for a single client call. This preserves accurate token and cost aggregation while exposing failed attempts.
