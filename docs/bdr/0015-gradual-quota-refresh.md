# BDR 0015: Gradual quota refresh

## Behaviour

- Each account's quota refreshes at its own time, before its snapshot reaches the TTL, instead of all accounts refreshing together.
- Accounts no longer all show unknown quota (and the configured unknown score) at the same moment once per refresh round.
- Two accounts on the same provider refresh at different times; the same account always refreshes at the same offset.
- `refresh_seconds` larger than `ttl_seconds` behaves as if it were the TTL minus the fetch timeout.
- Failed refreshes still back off as before; an idle account is dropped once unused for `idle_seconds` or longer.
- The TTL-unknown rule, the unknown score, and routing order from BDR 0008 and 0009 are unchanged.
