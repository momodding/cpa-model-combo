# ADR 0011: Automatic management authentication for quota polling

Status: accepted by user decision (automatic, in-memory). Amends ADR 0008's "separately configured bearer key" decision; all other ADR 0008 decisions stand.

- Cause of `management_key_missing`: `fetchQuota` read only `MODEL_COMBO_MANAGEMENT_KEY` from the CPA process environment; the browser's management login was never passed to the poller.
- Decision: quota polling uses the key CPA already authenticated on a registered `/v0/management/plugins/model-combo/*` route (Authorization Bearer/raw, then `X-Management-Key`). It is kept in memory only, never persisted, logged, serialized or sent to the browser. Resource routes, unregistered paths and wrong methods never supply a key.
- Precedence: latest in-memory key, then `MODEL_COMBO_MANAGEMENT_KEY`, else `management_key_missing`. After a CPA restart a dashboard visit is required unless the environment key is set. A rejected (401/403) key is shown as rejected and replaced by the next authenticated request; blocked entries refetch immediately, and in-flight results from an older key are discarded and refetched.
- Provider support is checked before the key, so `agy`/`devin` report `unsupported_provider`. Diagnostics gain `polling.authentication` (`missing|available|rejected`).
- Provider tokens still never leave CPA (`$TOKEN$` substitution via the loopback `api-call`); URL validation unchanged.
- Dashboard: Quota tracking is redesigned as Auth Files-style account cards (see BDR 0011). Implementation ordering note: Go changes began before this record; this ADR was written before any further feature edits.
