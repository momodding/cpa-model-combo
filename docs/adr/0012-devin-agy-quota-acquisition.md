# ADR 0012: Devin and agy quota acquisition

Extends ADR 0011 (unsupported providers).

- Built-in Devin: POST the Management Center GetUserStatus probe through `/v0/management/api-call`; CPA substitutes `$TOKEN$`. Daily/weekly remaining percent / 100; missing or invalid windows are unknown, never zero.
- agy: POST `{"auth_index"}` to `/v0/management/quota/fetch`; the host dispatches to the account's quota provider. No CLI spawning, HOME queries, or plugin-ID guessing.
- 501 => `quota_provider_unavailable`; 404 => `quota_endpoint_or_account_unavailable`; both nonretryable. agy group quotas have no model mapping, so they display but do not affect routing headroom.
