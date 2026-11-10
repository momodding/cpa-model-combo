# ADR 0001: Assert logical request counts in CPA integration

- Status: Accepted
- Date: 2026-11-09

## Context

The CPA integration workflow failed after request-detail pages began grouping routed fallback attempts by `router_request_id`. The smoke scenario emits six provider usage callbacks, but `/usage/requests` exposes five top-level logical requests because one routed request has two provider attempts.

## Decision

Keep production grouping unchanged. Update the integration assertions to distinguish callback-level overview totals from request-detail totals:

- overview and grouped accounting remain six provider callbacks;
- request-detail pages and `dashboard.requests.total` report five logical requests;
- routed fallback failure is asserted through the parent request's `attempts`, not as a separate top-level failed request.

## Consequences

The test validates the published request-detail contract instead of treating a fallback child as an independent request. No database schema, API format, or runtime routing behavior changes.
