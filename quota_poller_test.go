package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func pollerTestConfig() routerConfig {
	return routerConfig{QuotaPolling: quotaPollingConfig{RefreshSeconds: 1, IdleSeconds: 2, TTLSeconds: 1, MaxStaleSeconds: 8, FetchTimeoutSeconds: 3, MaxConcurrent: 2, ColdWaitMS: 1000}, Routes: []modelRoute{{Strategy: routeStrategyHeadroom}}}
}

func TestQuotaPollerDedupesAndSurvivesCancellation(t *testing.T) {
	m := newQuotaPollingManager(pollerTestConfig())
	defer m.Reconcile(routerConfig{})
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	m.fetch = func(ctx context.Context, _ accountCredential, _ string) quotaFetchResult {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return quotaFetchResult{Snapshot: quotaSnapshot{State: quotaKnown, FetchedAt: time.Now()}}
		case <-ctx.Done():
			return quotaFailure("test", "cancelled", true)
		}
	}
	credential := accountCredential{Provider: "claude", AuthIndex: "a"}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan map[string]quotaSnapshot, 1)
	go func() { first <- m.Get(ctx, []accountCredential{credential}) }()
	<-started
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); m.Get(context.Background(), []accountCredential{credential}) }()
	}
	cancel()
	if got := (<-first)[quotaKey(credential)]; got.State != quotaUnknown {
		t.Fatalf("cancelled cold request: %+v", got)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("fetches = %d", calls.Load())
	}
	if got := m.Get(context.Background(), []accountCredential{credential})[quotaKey(credential)]; got.State != quotaKnown {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestQuotaPollerTTLBackoffStaleAndReconcile(t *testing.T) {
	cfg := pollerTestConfig()
	m := newQuotaPollingManager(cfg)
	defer m.Reconcile(routerConfig{})
	var clockMu sync.Mutex
	now := time.Now()
	m.now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
	advance := func(d time.Duration) { clockMu.Lock(); now = now.Add(d); clockMu.Unlock() }
	var calls atomic.Int32
	m.fetch = func(context.Context, accountCredential, string) quotaFetchResult {
		if calls.Add(1) == 1 {
			return quotaFetchResult{Snapshot: quotaSnapshot{State: quotaKnown, FetchedAt: m.now()}}
		}
		return quotaFetchResult{Snapshot: quotaSnapshot{State: quotaUnknown, Err: "rate_limited", FetchedAt: m.now()}, Retryable: true}
	}
	cred := accountCredential{Provider: "codex", AuthIndex: "x"}
	key := quotaKey(cred)
	get := func() quotaSnapshot { return m.Get(context.Background(), []accountCredential{cred})[key] }
	if got := get(); got.State != quotaKnown {
		t.Fatalf("cold = %+v", got)
	}
	advance(2 * time.Second)
	get()
	m.mu.Lock()
	done := m.entries[key].pending
	m.mu.Unlock()
	if done != nil {
		<-done
	}
	got := get()
	// Past TTL a failed refresh must not keep serving the last observation.
	if got.State != quotaUnknown || got.Err != "stale" || got.BackoffUntil.IsZero() {
		t.Fatalf("retry snapshot = %+v", got)
	}
	if calls.Load() != 2 {
		t.Fatalf("backoff did not suppress fetch: %d", calls.Load())
	}
	advance(2 * time.Second)
	get()
	m.mu.Lock()
	done = m.entries[key].pending
	m.mu.Unlock()
	if done != nil {
		<-done
	}
	got = get()
	if got.BackoffUntil.Sub(m.now()) != 2*time.Second {
		t.Fatalf("second backoff = %v", got.BackoffUntil.Sub(m.now()))
	}
	advance(10 * time.Second)
	if got = get(); got.State != quotaUnknown || got.Err != "stale" {
		t.Fatalf("stale = %+v", got)
	}
	m.Reconcile(routerConfig{QuotaPolling: cfg.QuotaPolling})
	m.mu.Lock()
	count := len(m.entries)
	m.mu.Unlock()
	if count != 0 {
		t.Fatalf("entries after disable = %d", count)
	}
}

func TestQuotaPollerIdleStop(t *testing.T) {
	cfg := pollerTestConfig()
	cfg.QuotaPolling.IdleSeconds = 1
	m := newQuotaPollingManager(cfg)
	defer m.Reconcile(routerConfig{})
	m.fetch = func(context.Context, accountCredential, string) quotaFetchResult {
		return quotaFetchResult{Snapshot: quotaSnapshot{State: quotaKnown, FetchedAt: time.Now()}}
	}
	m.Get(context.Background(), []accountCredential{{Provider: "claude", AuthIndex: "idle"}})
	deadline := time.After(4 * time.Second)
	for {
		m.mu.Lock()
		running, count := m.running, len(m.entries)
		m.mu.Unlock()
		if !running && count == 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("poller did not stop when idle")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestQuotaPollerDiscardsInflightAfterDisable(t *testing.T) {
	cfg := pollerTestConfig()
	cfg.QuotaPolling.ColdWaitMS = 10
	m := newQuotaPollingManager(cfg)
	started, release := make(chan struct{}), make(chan struct{})
	m.fetch = func(context.Context, accountCredential, string) quotaFetchResult {
		close(started)
		<-release
		return quotaFetchResult{Snapshot: quotaSnapshot{State: quotaKnown, FetchedAt: time.Now()}}
	}
	cred := accountCredential{Provider: "claude", AuthIndex: "a"}
	result := make(chan quotaSnapshot, 1)
	go func() { result <- m.Get(context.Background(), []accountCredential{cred})[quotaKey(cred)] }()
	<-started
	if got := <-result; got.State != quotaUnknown {
		t.Fatalf("cold timeout = %+v", got)
	}
	m.mu.Lock()
	done := m.entries[quotaKey(cred)].pending
	m.mu.Unlock()
	m.Reconcile(routerConfig{QuotaPolling: cfg.QuotaPolling})
	close(release)
	<-done
	m.mu.Lock()
	count := len(m.entries)
	m.mu.Unlock()
	if count != 0 {
		t.Fatalf("discarded fetch restored %d entries", count)
	}
}

func TestQuotaRefreshDelayStaggersPerCredential(t *testing.T) {
	m := newQuotaPollingManager(routerConfig{QuotaPolling: quotaPollingConfig{RefreshSeconds: 60, IdleSeconds: 600, TTLSeconds: 60, MaxStaleSeconds: 300, FetchTimeoutSeconds: 5, MaxConcurrent: 3, ColdWaitMS: 1500}})
	seen := map[time.Duration]bool{}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := 0; i < 200; i++ {
		d := m.refreshDelayLocked(quotaKey(accountCredential{Provider: "codex", AuthIndex: fmt.Sprintf("acct-%d", i)}))
		if d < 41250*time.Millisecond || d > 55*time.Second {
			t.Fatalf("delay %v outside [41.25s, 55s]", d)
		}
		seen[d] = true
	}
	if len(seen) < 190 {
		t.Fatalf("only %d distinct delays for 200 credentials", len(seen))
	}
}

func TestQuotaPollerRefreshesCredentialsIndependently(t *testing.T) {
	cfg := pollerTestConfig()
	cfg.QuotaPolling.IdleSeconds = 60
	m := newQuotaPollingManager(cfg)
	defer m.Reconcile(routerConfig{})
	var clockMu sync.Mutex
	now := time.Now()
	m.now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
	advanceTo := func(at time.Time) { clockMu.Lock(); now = at; clockMu.Unlock() }
	var callsMu sync.Mutex
	calls := map[string]int{}
	release := make(chan struct{}) // holds every refetch so the pending window is deterministic
	m.fetch = func(_ context.Context, c accountCredential, _ string) quotaFetchResult {
		callsMu.Lock()
		calls[c.AuthIndex]++
		refetch := calls[c.AuthIndex] > 1
		callsMu.Unlock()
		if refetch {
			<-release
		}
		return quotaFetchResult{Snapshot: quotaSnapshot{State: quotaKnown, FetchedAt: m.now()}}
	}
	delay := func(c accountCredential) time.Duration {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.refreshDelayLocked(quotaKey(c))
	}
	a := accountCredential{Provider: "codex", AuthIndex: "acct-0"}
	b := accountCredential{Provider: "codex", AuthIndex: "acct-1"}
	for i := 2; delay(b) == delay(a); i++ {
		b.AuthIndex = fmt.Sprintf("acct-%d", i)
	}
	creds := []accountCredential{a, b}
	start := m.now()
	if got := m.Get(context.Background(), creds); got[quotaKey(a)].State != quotaKnown || got[quotaKey(b)].State != quotaKnown {
		t.Fatalf("cold = %+v", got)
	}
	m.mu.Lock()
	first, second := a, b
	if m.entries[quotaKey(b)].nextRefresh.Before(m.entries[quotaKey(a)].nextRefresh) {
		first, second = b, a
	}
	firstAt := m.entries[quotaKey(first)].nextRefresh
	secondAt := m.entries[quotaKey(second)].nextRefresh
	m.mu.Unlock()
	advanceTo(firstAt)
	m.Get(context.Background(), creds)
	m.mu.Lock()
	firstDone, secondPending := m.entries[quotaKey(first)].pending, m.entries[quotaKey(second)].pending
	m.mu.Unlock()
	if firstDone == nil {
		t.Fatal("first credential not pending at its own deadline")
	}
	if secondPending != nil {
		t.Fatal("second credential refreshed before its own deadline")
	}
	close(release)
	<-firstDone
	got := m.Get(context.Background(), creds)
	callsMu.Lock()
	firstCalls, secondCalls := calls[first.AuthIndex], calls[second.AuthIndex]
	callsMu.Unlock()
	if firstCalls != 2 || secondCalls != 1 {
		t.Fatalf("calls first=%d second=%d, want 2 and 1", firstCalls, secondCalls)
	}
	if got[quotaKey(first)].State != quotaKnown || got[quotaKey(second)].State != quotaKnown {
		t.Fatalf("snapshots = %+v", got)
	}
	ttl := time.Duration(cfg.QuotaPolling.TTLSeconds) * time.Second
	if age := m.now().Sub(got[quotaKey(second)].FetchedAt); age <= 0 || age >= ttl {
		t.Fatalf("second snapshot age %v, want in (0, %v)", age, ttl)
	}
	if firstAt.Sub(start) >= ttl {
		t.Fatalf("first deadline %v not before TTL %v", firstAt.Sub(start), ttl)
	}
	m.mu.Lock()
	next := m.entries[quotaKey(first)].nextRefresh
	m.mu.Unlock()
	if !next.After(secondAt) {
		t.Fatalf("first next refresh %v not after second's %v", next, secondAt)
	}
}
