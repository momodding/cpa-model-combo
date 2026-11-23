package main

import (
	"sort"
	"strings"
	"time"
)

type quotaDiagnosticCredential struct {
	Provider     string        `json:"provider"`
	AuthIndex    string        `json:"auth_index"`
	Label        string        `json:"label"`
	State        string        `json:"state"`
	Windows      []quotaWindow `json:"windows"`
	Source       string        `json:"source"`
	FetchedAt    *time.Time    `json:"fetched_at"`
	AgeSeconds   *float64      `json:"age_seconds"`
	Error        string        `json:"error"`
	BackoffUntil *time.Time    `json:"backoff_until"`
}

type quotaDiagnosticIdentity struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	AuthIndex string `json:"auth_index"`
	Label     string `json:"label"`
}

type quotaDiagnosticSelection struct {
	Model      string                  `json:"model"`
	Credential quotaDiagnosticIdentity `json:"credential"`
	Score      float64                 `json:"score"`
	State      string                  `json:"state"`
	Reason     string                  `json:"reason"`
}

type quotaDiagnosticRoute struct {
	Alias     string                     `json:"alias"`
	Selection *quotaDiagnosticSelection  `json:"selection"`
	Skips     []quotaDiagnosticSelection `json:"skips"`
}

type quotaDiagnosticCooldown struct {
	Route        string    `json:"route"`
	Model        string    `json:"model"`
	CredentialID string    `json:"credential_id,omitempty"`
	Until        time.Time `json:"until"`
}

type quotaDiagnosticResponse struct {
	Polling struct {
		Running    bool       `json:"running"`
		ActiveKeys int        `json:"active_keys"`
		LastTick   *time.Time `json:"last_tick"`
		// missing, available, or rejected; never carries the key.
		Authentication string `json:"authentication"`
	} `json:"polling"`
	Credentials     []quotaDiagnosticCredential `json:"credentials"`
	Routes          []quotaDiagnosticRoute      `json:"routes"`
	Cooldowns       []quotaDiagnosticCooldown   `json:"cooldowns"`
	Events          []routingEvent              `json:"events"`
	EventCursor     uint64                      `json:"event_cursor"`
	EventsTruncated bool                        `json:"events_truncated"`
}

func diagnosticTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func diagnosticSelection(s headroomSelection) quotaDiagnosticSelection {
	return quotaDiagnosticSelection{Model: s.Model, Credential: quotaDiagnosticIdentity{ID: s.Credential.ID, Provider: s.Credential.Provider, AuthIndex: s.Credential.AuthIndex, Label: s.Credential.Label}, Score: s.Score, State: s.State, Reason: s.Reason}
}

// appendCooldowns copies unexpired model and account cooldowns of one route; the caller holds runtime.mu.
func appendCooldowns(out []quotaDiagnosticCooldown, route modelRoute, state *routeState, now time.Time) []quotaDiagnosticCooldown {
	for _, target := range route.Targets {
		model := strings.TrimSpace(target.Model)
		key := routeKey(model)
		if until := state.cooldowns[key]; until.After(now) {
			out = append(out, quotaDiagnosticCooldown{Route: route.Alias, Model: model, Until: until})
		}
		ids := make([]string, 0, len(state.accountCooldowns[key]))
		for id, until := range state.accountCooldowns[key] {
			if until.After(now) {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			out = append(out, quotaDiagnosticCooldown{Route: route.Alias, Model: model, CredentialID: id, Until: state.accountCooldowns[key][id]})
		}
	}
	return out
}

func quotaDiagnostics(p *modelRouterPlugin, eventsSince uint64) quotaDiagnosticResponse {
	result := quotaDiagnosticResponse{Credentials: []quotaDiagnosticCredential{}, Routes: []quotaDiagnosticRoute{}, Cooldowns: []quotaDiagnosticCooldown{}, Events: []routingEvent{}}
	if p == nil {
		return result
	}
	if m := p.quota; m != nil {
		m.mu.Lock()
		now := m.now()
		result.Polling.Running = m.running
		result.Polling.ActiveKeys = len(m.entries)
		result.Polling.LastTick = diagnosticTime(m.lastTick)
		result.Polling.Authentication = "available"
		if m.effectiveKeyLocked() == "" {
			result.Polling.Authentication = "missing"
		}
		for _, entry := range m.entries {
			snapshot := entry.snapshot
			if !entry.hasSnapshot {
				snapshot = quotaSnapshot{Err: "pending"}
			} else if now.Sub(snapshot.FetchedAt) > time.Duration(m.cfg.TTLSeconds)*time.Second {
				snapshot = quotaSnapshot{Err: "stale", Source: snapshot.Source, FetchedAt: snapshot.FetchedAt, BackoffUntil: snapshot.BackoffUntil}
			}
			item := quotaDiagnosticCredential{Provider: entry.credential.Provider, AuthIndex: entry.credential.AuthIndex, Label: entry.credential.Label, State: snapshot.State.String(), Windows: append([]quotaWindow{}, snapshot.Windows...), Source: snapshot.Source, FetchedAt: diagnosticTime(snapshot.FetchedAt), Error: snapshot.Err, BackoffUntil: diagnosticTime(snapshot.BackoffUntil)}
			if item.FetchedAt != nil {
				age := now.Sub(snapshot.FetchedAt).Seconds()
				if age < 0 {
					age = 0
				}
				item.AgeSeconds = &age
			}
			if (snapshot.Err == "management_http_401" || snapshot.Err == "management_http_403") && result.Polling.Authentication == "available" {
				result.Polling.Authentication = "rejected"
			}
			result.Credentials = append(result.Credentials, item)
		}
		m.mu.Unlock()
		sort.Slice(result.Credentials, func(i, j int) bool {
			if result.Credentials[i].Provider != result.Credentials[j].Provider {
				return result.Credentials[i].Provider < result.Credentials[j].Provider
			}
			return result.Credentials[i].AuthIndex < result.Credentials[j].AuthIndex
		})
	}
	if runtime := p.runtime; runtime != nil {
		runtime.mu.Lock()
		now := runtime.now()
		for _, route := range p.config.Routes {
			if state := runtime.states[routeKey(route.Alias)]; state != nil {
				result.Cooldowns = appendCooldowns(result.Cooldowns, route, state, now)
			}
			if route.Strategy != routeStrategyHeadroom {
				continue
			}
			item := quotaDiagnosticRoute{Alias: route.Alias, Skips: []quotaDiagnosticSelection{}}
			if state := runtime.states[routeKey(route.Alias)]; state != nil {
				if state.lastHeadroom.Model != "" {
					selection := diagnosticSelection(state.lastHeadroom)
					item.Selection = &selection
				}
				for _, skip := range state.headroomSkips {
					item.Skips = append(item.Skips, diagnosticSelection(skip))
				}
			}
			result.Routes = append(result.Routes, item)
		}
		runtime.mu.Unlock()
	}
	// The ring has its own mutex and is read after every runtime and quota lock is released.
	result.Events, result.EventCursor, result.EventsTruncated = p.events.since(eventsSince)
	return result
}
