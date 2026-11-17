# BDR 0004: Request details parent/child behavior

## Status

Superseded by [BDR 0005](0005-hideable-historical-request-columns.md).

## Behavior

One inbound CPA request renders one parent row keyed by `request_id`. Expanding it shows ordered provider attempts. The parent displays inbound endpoint, requested model, stream state, final result, status, and end-to-end latency. Children display attempt number and provider accounting data.

For one request to `POST /v1/chat/completions`, a `gpt5.6-sol` attempt returning 429 followed by `claude-sonnet-5.5` returning 200 renders one successful parent with two children.

Direct requests with one provider callback remain expandable. Completed requests without provider callbacks remain visible. Callback delivery before completion may produce a temporary exact parent that lifecycle metadata replaces on refresh.

## Filtering and ordering

Time, result, and router-model filters evaluate parent fields. Provider, source, and service-tier filters match any child. Sorting and pagination count parents.

Children sort by `(router_attempt, requested_at, sequence)` only when every child shares one non-empty `router_request_id`; otherwise they sort by `(requested_at, sequence)`.

## Compatibility

Schema-7 hosts provide exact identity. Older hosts show legacy rows without lifecycle parents. Historical rows without `request_id` continue legacy grouping and are never assigned a guessed parent.
