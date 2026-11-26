<!-- source-branch: main -->
<!-- work-branch: omp/gradual-quota-refresh -->

# gradual-quota-refresh

## Context
I reviewed the quota polling code on `marketplace/main` (2642c52). Gradual refresh is **not implemented**. In `quota_poller.go`, `loop()` uses one timer of `refresh_seconds`. When that timer fires, it marks every credential due together. The defaults set `refresh_seconds` = `ttl_seconds` = 60. A snapshot is stale once its age is greater than the TTL, so every snapshot crosses the TTL during the same refresh round. All credentials then read `unknown`/`stale` at once until their fetches finish, and `max_concurrent` (3) limits how quickly those fetches complete. The goal is to give each credential, keyed by `quotaKey` (`provider|auth_index`), its own refresh time that falls before its TTL. Credentials then refresh gradually and never expire together. The spread is per credential, not per provider.

## Approach

### 0. Isolation and decisions
- `cd /Users/momodding/Documents/cpa-model-combo-marketplace && git fetch marketplace && git worktree add ../cpa-model-combo-gradual-refresh -b omp/gradual-quota-refresh marketplace/main`. If the directory or branch already exists, add the first unused `-N` suffix (N ≥ 2) to both. Do all work in that worktree. Do not touch the dirty source checkout.
- Copy this plan to `plans/gradual-quota-refresh.md` in the worktree. Add `<!-- source-branch: main -->` (the PR base) and `<!-- work-branch: <actual> -->`.
- Add `docs/adr/0015-gradual-quota-refresh.md` and `docs/bdr/0015-gradual-quota-refresh.md`, following the short style of 0009. Record these decisions:
  - Each credential has its own refresh deadline: `fetchedAt + refreshDelay(key)`.
  - The delay is deterministic from an FNV-64a hash of `quotaKey`, so two accounts on the same provider get different phases.
  - The refresh period is `min(refresh_seconds, ttl_seconds)`, minus the fetch timeout, so a successful refresh lands before the TTL. A `refresh_seconds` above the TTL is effectively capped. Before this change, requests already triggered a refresh at the TTL.
  - Retryable failures reschedule at `BackoffUntil`, which keeps the existing backoff unchanged.
  - Idle eviction uses `>= idle_seconds`.
  - ADR/BDR 0008/0009 are unaffected: the TTL-unknown rule, the unknown score, and lazy, shared, bounded polling all stay. This change amends nothing.

### 1. Per-credential schedule (`quota_poller.go`)
- Add the field `nextRefresh time.Time` to `quotaPollEntry`. The zero value means due now.
- Add this method; there is no equivalent today:
```go
// refreshDelayLocked staggers credentials by key so their snapshots never expire together.
func (m *QuotaPollingManager) refreshDelayLocked(key string) time.Duration {
	window := time.Duration(min(m.cfg.RefreshSeconds, m.cfg.TTLSeconds)) * time.Second
	lead := time.Duration(m.cfg.FetchTimeoutSeconds) * time.Second
	if lead >= window {
		lead = window / 2
	}
	base := window - lead
	h := fnv.New64a()
	h.Write([]byte(key))
	return base - time.Duration(h.Sum64()%uint64(base/4+1))
}
```
  Import `hash/fnv`. With the defaults (60/60/5) the delay falls in [41.25s, 55s]. Config validation already guarantees that every value is positive, so `base` > 0.
- In `fetchOne`, inside the `m.entries[key] == entry` (current generation) branch, after the snapshot is updated:
  - On a retryable failure, set `entry.nextRefresh = entry.snapshot.BackoffUntil`.
  - Otherwise, set `entry.nextRefresh = entry.snapshot.FetchedAt.Add(m.refreshDelayLocked(key))`. This runs after the existing zero-`FetchedAt` → `now` fill, so place it after that block.
  - After `close(done)`/`pumpLocked()`, wake the loop with a non-blocking `select { case m.wake <- struct{}{}: default: }`, the same pattern as `Reconcile`.
  - Leave the generation-mismatch branch unchanged.
- In `Get`, change the trigger at line 88 to `if entry.pending == nil && !now.Before(entry.nextRefresh) {`. `nextRefresh` is always at or after `BackoffUntil` and always before the TTL, so it covers both the old backoff check and the old TTL check. Leave the TTL-based stale reporting in the `collect` block unchanged.
- Replace the body of `loop()` with a deadline scheduler and drop the old fixed-interval timer and its drain code:
```go
for {
	m.mu.Lock()
	if !m.enabled || len(m.entries) == 0 {
		m.running = false
		m.mu.Unlock()
		return
	}
	now := m.now()
	m.lastTick = now
	idle := time.Duration(m.cfg.IdleSeconds) * time.Second
	next := now.Add(idle)
	for key, entry := range m.entries {
		expiry := entry.lastUsed.Add(idle)
		if !now.Before(expiry) {
			if entry.pending != nil && !entry.started {
				close(entry.pending)
			}
			delete(m.entries, key)
			continue
		}
		if expiry.Before(next) {
			next = expiry
		}
		if entry.pending == nil && !now.Before(entry.nextRefresh) {
			entry.pending = make(chan struct{})
		}
		if entry.pending == nil && entry.nextRefresh.Before(next) {
			next = entry.nextRefresh
		}
	}
	m.pumpLocked()
	m.mu.Unlock()
	timer := time.NewTimer(next.Sub(now))
	select {
	case <-timer.C:
	case <-m.wake:
		timer.Stop()
	}
}
```
  Pending entries are excluded from `next`, so the loop cannot busy-spin. `fetchOne` wakes the loop once it sets a new deadline. Leave `SetManagementKey` unchanged: it creates a pending fetch directly, and `fetchOne` then reschedules.

### 2. Docs (`README.md`)
- In the `quota_polling` table, change `refresh_seconds` to: "Per-credential refresh period. Each credential refreshes at its own staggered time before `ttl_seconds` expires (period capped at `ttl_seconds` minus `fetch_timeout_seconds`), so credentials do not go unknown together."
- Change `idle_seconds` to "…unused for this interval or longer."

## Critical files & anchors
- `quota_poller.go`: `quotaPollEntry`, `Get` (line 88 trigger), `fetchOne` (lines 165–198), `loop` (lines 201–240).
- `quota_poller_test.go`: `pollerTestConfig` (Refresh 1, TTL 1, FetchTimeout 3 means `lead = 500ms` and delay ∈ [375ms, 500ms]). The existing tests must keep passing without edits.

## Verification
Add to `quota_poller_test.go`:
1. `TestQuotaRefreshDelayStaggersPerCredential`:
   - Build a manager config with the defaults (Refresh 60, TTL 60, FetchTimeout 5, Idle 600, MaxStale 300, MaxConcurrent 3, ColdWait 1500) and no headroom route.
   - Compute `refreshDelayLocked(quotaKey({Provider:"codex", AuthIndex: fmt.Sprintf("acct-%d", i)}))` for i in 0..199.
   - Assert every delay is in `[41250ms, 55s]`, meaning strictly below TTL − fetch timeout.
   - Assert at least 190 distinct values, which proves the spread is per credential within one provider.
2. `TestQuotaPollerRefreshesCredentialsIndependently`:
   - Use `pollerTestConfig()` with `IdleSeconds: 60` and a fake clock (copy the `clockMu`/`advance` pattern from `TestQuotaPollerTTLBackoffStaleAndReconcile`).
   - `fetch` counts calls per `AuthIndex` and returns known with `FetchedAt: m.now()`.
   - Credentials `a` = `{codex, "acct-0"}`, `b` = the first `{codex, "acct-N"}` (N = 1,2,…) whose `refreshDelayLocked` differs from `a`'s.
   - Do one cold `Get` of both, then read both `nextRefresh` values under the lock and let `first`/`second` be the earlier and later.
   - Advance to `first` and call `Get` for both:
     - Only the first credential's entry is pending; wait on that channel.
     - Assert per-key calls are 2 for the first credential and 1 for the second.
     - Assert both snapshots are `quotaKnown`, so the second is not stale because its age is below the TTL.
     - Assert the first credential's new `nextRefresh` is later than the second's.

In the worktree, all of these must pass:
`gofmt -l *.go` (empty), `go vet ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...`, `node --test tests/dashboard/*.test.mjs`, `bash -n .github/xgo/*.sh`, `CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -o /tmp/model-combo-gradual.dylib .`.

Run `CPA_SOURCE="$PWD/.cpa-source" go test -tags=integration ./... -count=1` only if `.cpa-source` exists in the worktree. Otherwise, and for the Playwright e2e, rely on GitHub CI for the PR and record that in ADR 0015 under "Verification status".

## Delivery
After everything passes, use the eval `commit` namespace to make one commit, `feat(quota): stagger quota refresh per credential`, with a body listing the checks run. Then:
- `git push -u marketplace <work-branch>`
- `gh pr create --repo momodding/cpa-model-combo --base main --head <work-branch> --title "feat(quota): gradual per-credential quota refresh"`, with a body summarizing the finding, the scheduler change and the verification.
- Wait for CI with `gh pr checks --watch`. If CI fails, fix it on the same branch and push again. Do not merge into `main`; the PR is merged only after it passes, per AGENTS.md. The user chose push + PR over the global no-push rule, so note that choice in ADR 0015.
