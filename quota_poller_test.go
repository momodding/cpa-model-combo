package main

import (
	"context"
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
	m.fetch = func(ctx context.Context, _ accountCredential) quotaFetchResult {
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
	m.fetch = func(context.Context, accountCredential) quotaFetchResult {
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
	m.fetch = func(context.Context, accountCredential) quotaFetchResult {
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
	m.fetch = func(context.Context, accountCredential) quotaFetchResult {
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
