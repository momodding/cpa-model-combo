# BDR 0009: Quota tracking tab and unknown-quota score

## Behaviour

- A "Quota tracking" tab sits beside "Usage tracking". It holds quota polling settings, the unknown quota score, and the live quota status.
- Accounts whose quota is missing, failed, unsupported or older than the TTL are treated as having the configured score (default 50%), shown in the dashboard as a setting, not as a measured value.
- A known account above the score is preferred over an unknown one; a known account below it is tried after.
- Route policy `exclude` still removes unknown accounts, and `last` still tries them last.
- Invalid scores (negative, above 100, non-numeric) are rejected on save.
