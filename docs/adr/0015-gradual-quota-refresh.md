# ADR 0015: Gradual per-credential quota refresh

## Status

Accepted by the user's approval of the gradual-quota-refresh plan. ADR 0008 and 0009 are unaffected: the TTL-unknown rule, the unknown score, and lazy, shared, bounded polling all stay. This change amends nothing.

## Context

`QuotaPollingManager.loop()` used one timer of `refresh_seconds` and marked every credential due together. With the defaults (`refresh_seconds` = `ttl_seconds` = 60), every snapshot crossed the TTL in the same round, so all credentials read `unknown`/`stale` at once until their fetches finished, throttled by `max_concurrent`.

## Decision

- Each credential, keyed by `quotaKey` (`provider|auth_index`), has its own refresh deadline `nextRefresh = fetchedAt + refreshDelay(key)`. The zero value means due now.
- `refreshDelay` is deterministic from an FNV-64a hash of `quotaKey`, so two accounts on the same provider get different phases.
- The refresh period is `min(refresh_seconds, ttl_seconds)` minus the fetch timeout (halved window if the timeout is not smaller), with a hash-derived offset of up to a quarter of that, so a successful refresh lands before the TTL. A `refresh_seconds` above the TTL is effectively capped; before this change, requests already triggered a refresh at the TTL. With the defaults (60/60/5) the delay is in [41.25s, 55s].
- Retryable failures reschedule at `BackoffUntil`; the existing backoff is unchanged.
- `loop()` sleeps until the earliest of any `nextRefresh` or idle expiry; `fetchOne` wakes it after setting a new deadline. Pending entries are excluded from the wait, so it cannot busy-spin.
- Idle eviction uses `>= idle_seconds`.
- The user chose push + pull request over the global no-push rule for this work.

## Verification status

Local checks passed: `gofmt -l *.go` (empty, exit 0), `go vet ./...` (exit 0), `go test -count=1 ./...` (exit 0, 27.338s), `go test -race -count=1 ./...` (exit 0, 113.671s), `node --test tests/dashboard/*.test.mjs` (33/33 passed, exit 0), `bash -n .github/xgo/release.sh` and `bash -n .github/xgo/setup.sh` (exit 0), and `CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -o /tmp/model-combo-gradual.dylib .` (exit 0). `.cpa-source` is absent from the worktree, so the integration tests and the Playwright e2e rely on GitHub CI for the pull request.
