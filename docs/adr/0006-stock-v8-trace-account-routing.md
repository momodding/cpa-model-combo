# ADR 0006: Stock v8 trace and account routing

## Status and provenance

Accepted. This repository carries the hierarchy history in [ADR 0004](0004-request-id-parent-provider-child-contract.md) and the stock-v7 limitation in [ADR 0005](0005-stock-cpa-usage-identity-limits.md), originally developed in the sibling `cpa-model-combo-request-id-hierarchy` repository. With user approval, this decision supersedes ADR 0005's stock-v7 identity limitation for stock v8 while retaining ADR 0004's hierarchy and historical-data intent. The historical records remain unchanged; their custom schema-7 requirement does not apply to stock v8.

## Context

Stock CLIProxyAPI v8.0.37 exposes inbound trace and execution request identities in delivered usage records, lifecycle metadata, credential identities, and per-credential model registry membership. Registry membership does not establish entitlement. Usage records do not carry the inbound endpoint; CPA lifecycle metadata supplies `request_path`, which can be a route template. The usage stream is not a complete transport-attempt feed.

## Decision

- Use the stock v8 SDK and plugin ABI, without modifying CPA. Keep the trace ID, execution request ID, and lifecycle request ID distinct. Transactionally migrate SQLite to schema 3, preserving historical request payloads and legacy grouping.
- Join delivered executions only to an unambiguous trace; preserve separate lifecycle IDs to detect collisions. Completion determines parent outcome. Do not synthesize children, usage, or cost when no usage callback was delivered.
- Persist safe credential IDs/indexes and sanitized label or email snapshots. Refresh snapshots through valid host callback contexts, not during usage ingestion. Never persist tokens, raw metadata, completion errors, or guessed historical identities.
- Empty or omitted `allowed_accounts` is unrestricted. Restricted routes pin each execution to an enabled allowed credential and provider, using fresh registry membership; failures never relax the restriction. Model membership denotes compatibility only; entitlement remains unknown.
- Rollback restores a consistent pre-upgrade configuration/database backup and matching old CPA/plugin artifacts. Do not run old writers against schema 3 or downgrade the schema in place.

## Consequences

A lifecycle completion that arrives before its before-auth session has been persisted is dropped rather than inserted: nested host completions share the trace and an inserted row would fabricate a parent. Before-auth persistence is synchronous, so this is expected only if the host delivers both concurrently.

The deployment must pair the plugin with CPA v8.0.37. Some direct traffic can have unavailable labels before a catalog refresh; labels already stored may outlive deleted accounts. Lifecycle-only failures remain visible, but unreported internal attempts cannot be shown. Trace collisions must remain explicitly ambiguous. These privacy and accuracy limits take precedence over complete-looking histories.
