# BDR 0003: v1.0.1 visible Model Combo behavior

- Status: Accepted
- Date: 2026-11-10

## Decision

Users see `Model Combo` in CPA metadata, management navigation, and dashboard title. CPA addresses the plugin as `model-combo`: configuration is under `plugins.configs.model-combo` and management endpoints are under `/plugins/model-combo/...`.

## Observable behavior

- Existing users rename `plugins.configs.cpa-model-combo` to `plugins.configs.model-combo`; no compatibility key is accepted. Usage history remains in `cpa-model-combo.db`.
- Releases provide five `model-combo_1.0.1_<platform>.zip` archives, each containing only its versioned `model-combo-v1.0.1.<ext>` runtime entry.
- A routed fallback request appears once, as a `routed` parent with router model and request ID, plus ordered provider attempt children. Six callback overview records still yield five logical smoke-scenario requests under BDR 0001.
- All three management toggles remain readable, focusable, and usable across narrow and desktop layouts.
- Omitted or empty fallback statuses display `401, 402, 403, 404, 408, 429, 500, 502, 503, 504`; explicit non-empty lists display unchanged.
