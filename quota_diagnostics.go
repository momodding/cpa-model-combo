package main

import (
	"sort"
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

type quotaDiagnosticResponse struct {
	Polling struct {
		Running    bool       `json:"running"`
		ActiveKeys int        `json:"active_keys"`
		LastTick   *time.Time `json:"last_tick"`
	} `json:"polling"`
	Credentials []quotaDiagnosticCredential `json:"credentials"`
	Routes      []quotaDiagnosticRoute      `json:"routes"`
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

func quotaDiagnostics(p *modelRouterPlugin) quotaDiagnosticResponse {
	result := quotaDiagnosticResponse{Credentials: []quotaDiagnosticCredential{}, Routes: []quotaDiagnosticRoute{}}
	if p == nil {
		return result
	}
	if m := p.quota; m != nil {
		m.mu.Lock()
		now := m.now()
		result.Polling.Running = m.running
		result.Polling.ActiveKeys = len(m.entries)
		result.Polling.LastTick = diagnosticTime(m.lastTick)
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
		for _, route := range p.config.Routes {
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
	return result
}
