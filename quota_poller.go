package main

import (
	"context"
	"strings"
	"sync"
	"time"
)

type quotaPollEntry struct {
	credential  accountCredential
	snapshot    quotaSnapshot
	hasSnapshot bool
	lastUsed    time.Time
	failures    int
	pending     chan struct{}
	started     bool
}

type QuotaPollingManager struct {
	mu       sync.Mutex
	cfg      quotaPollingConfig
	enabled  bool
	entries  map[string]*quotaPollEntry
	running  bool
	active   int
	lastTick time.Time
	wake     chan struct{}
	ctx      context.Context
	fetch    func(context.Context, accountCredential) quotaFetchResult
	now      func() time.Time
}

func quotaKey(credential accountCredential) string {
	return strings.ToLower(strings.TrimSpace(credential.Provider)) + "|" + strings.TrimSpace(credential.AuthIndex)
}

func newQuotaPollingManager(cfg routerConfig) *QuotaPollingManager {
	m := &QuotaPollingManager{entries: make(map[string]*quotaPollEntry), wake: make(chan struct{}, 1), ctx: context.Background(), fetch: fetchQuota, now: time.Now}
	m.Reconcile(cfg)
	return m
}

func (m *QuotaPollingManager) Reconcile(cfg routerConfig) {
	enabled := false
	for _, route := range cfg.Routes {
		if route.Strategy == routeStrategyHeadroom {
			enabled = true
			break
		}
	}
	m.mu.Lock()
	m.cfg, m.enabled = cfg.QuotaPolling, enabled
	if !enabled {
		for key, entry := range m.entries {
			if entry.pending != nil && !entry.started {
				close(entry.pending)
			}
			delete(m.entries, key)
		}
	}
	m.pumpLocked()
	select {
	case m.wake <- struct{}{}:
	default:
	}
	m.mu.Unlock()
}

// Get waits only for initially missing snapshots; the caller never owns shared fetches.
func (m *QuotaPollingManager) Get(ctx context.Context, credentials []accountCredential) map[string]quotaSnapshot {
	m.mu.Lock()
	now := m.now()
	var cold []<-chan struct{}
	if m.enabled {
		for _, credential := range credentials {
			key := quotaKey(credential)
			entry := m.entries[key]
			if entry == nil {
				entry = &quotaPollEntry{}
				m.entries[key] = entry
			}
			entry.credential, entry.lastUsed = credential, now
			if entry.pending == nil && !now.Before(entry.snapshot.BackoffUntil) && (!entry.hasSnapshot || now.Sub(entry.snapshot.FetchedAt) >= time.Duration(m.cfg.TTLSeconds)*time.Second) {
				entry.pending = make(chan struct{})
			}
			if !entry.hasSnapshot && entry.pending != nil {
				cold = append(cold, entry.pending)
			}
		}
		if len(m.entries) > 0 && !m.running {
			m.running = true
			go m.loop()
		}
		m.pumpLocked()
	}
	wait := time.Duration(m.cfg.ColdWaitMS) * time.Millisecond
	m.mu.Unlock()
	if len(cold) > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		for _, done := range cold {
			select {
			case <-done:
			case <-ctx.Done():
				goto collect
			case <-timer.C:
				goto collect
			}
		}
	}
collect:
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]quotaSnapshot, len(credentials))
	now = m.now()
	for _, credential := range credentials {
		key := quotaKey(credential)
		entry := m.entries[key]
		if entry == nil || !entry.hasSnapshot {
			result[key] = quotaSnapshot{State: quotaUnknown, Err: "pending"}
		} else if now.Sub(entry.snapshot.FetchedAt) > time.Duration(m.cfg.TTLSeconds)*time.Second {
			result[key] = quotaSnapshot{State: quotaUnknown, Err: "stale", Source: entry.snapshot.Source, FetchedAt: entry.snapshot.FetchedAt, BackoffUntil: entry.snapshot.BackoffUntil}
		} else {
			result[key] = entry.snapshot
		}
	}
	return result
}

func (m *QuotaPollingManager) pumpLocked() {
	if !m.enabled {
		return
	}
	for key, entry := range m.entries {
		if m.active >= m.cfg.MaxConcurrent {
			return
		}
		if entry.pending == nil || entry.started {
			continue
		}
		entry.started = true
		m.active++
		credential, done := entry.credential, entry.pending
		timeout := time.Duration(m.cfg.FetchTimeoutSeconds) * time.Second
		go m.fetchOne(key, entry, credential, done, timeout)
	}
}

func (m *QuotaPollingManager) fetchOne(key string, entry *quotaPollEntry, credential accountCredential, done chan struct{}, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(m.ctx, timeout)
	result := m.fetch(ctx, credential)
	cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active--
	if m.entries[key] == entry {
		now := m.now()
		if result.Retryable {
			entry.failures++
			delay := time.Duration(m.cfg.RefreshSeconds) * time.Second
			capDelay := time.Duration(m.cfg.MaxStaleSeconds) * time.Second
			for i := 1; i < entry.failures && delay < capDelay; i++ {
				if delay > capDelay/2 {
					delay = capDelay
					break
				}
				delay *= 2
			}
			if delay > capDelay {
				delay = capDelay
			}
			if !entry.hasSnapshot {
				entry.snapshot = result.Snapshot
				entry.hasSnapshot = true
			}
			entry.snapshot.BackoffUntil = now.Add(delay)
		} else {
			entry.failures = 0
			entry.snapshot = result.Snapshot
			entry.snapshot.BackoffUntil = time.Time{}
			entry.hasSnapshot = true
		}
		if entry.snapshot.FetchedAt.IsZero() {
			entry.snapshot.FetchedAt = now
		}
		entry.pending, entry.started = nil, false
	}
	close(done)
	m.pumpLocked()
}

func (m *QuotaPollingManager) loop() {
	for {
		m.mu.Lock()
		if !m.enabled || len(m.entries) == 0 {
			m.running = false
			m.mu.Unlock()
			return
		}
		interval := time.Duration(m.cfg.RefreshSeconds) * time.Second
		m.mu.Unlock()
		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
			m.mu.Lock()
			now := m.now()
			m.lastTick = now
			for key, entry := range m.entries {
				if now.Sub(entry.lastUsed) > time.Duration(m.cfg.IdleSeconds)*time.Second {
					if entry.pending != nil && !entry.started {
						close(entry.pending)
					}
					delete(m.entries, key)
					continue
				}
				if entry.pending == nil && !now.Before(entry.snapshot.BackoffUntil) {
					entry.pending = make(chan struct{})
				}
			}
			m.pumpLocked()
			m.mu.Unlock()
		case <-m.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}
