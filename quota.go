package main

import (
	"math"
	"strings"
	"time"
)

type quotaState uint8

const (
	quotaUnknown quotaState = iota
	quotaPartial
	quotaKnown
	quotaUnlimited
	quotaExhausted
)

type quotaWindow struct {
	Name      string    `json:"name"`
	Scope     string    `json:"scope,omitempty"`
	Remaining float64   `json:"remaining"`
	ResetAt   time.Time `json:"reset_at,omitempty"`
}

type quotaSnapshot struct {
	State        quotaState    `json:"state"`
	Windows      []quotaWindow `json:"windows,omitempty"`
	Source       string        `json:"source,omitempty"`
	FetchedAt    time.Time     `json:"fetched_at,omitempty"`
	Err          string        `json:"error,omitempty"`
	BackoffUntil time.Time     `json:"backoff_until,omitempty"`
}

func (s quotaState) String() string {
	switch s {
	case quotaPartial:
		return "partial"
	case quotaKnown:
		return "known"
	case quotaUnlimited:
		return "unlimited"
	case quotaExhausted:
		return "exhausted"
	default:
		return "unknown"
	}
}

func headroom(snapshot quotaSnapshot, model string) (float64, quotaState) {
	if snapshot.State == quotaUnknown {
		return 0, quotaUnknown
	}
	if snapshot.State == quotaUnlimited && len(snapshot.Windows) == 0 {
		return 1, quotaUnlimited
	}
	model = strings.ToLower(model)
	score, found := 1.0, false
	for _, w := range snapshot.Windows {
		scope := strings.ToLower(w.Scope)
		match := scope == "" || scope == "opus" || scope == "sonnet"
		if !match {
			for _, family := range []string{"gemini-", "claude-", "gpt-"} {
				if strings.HasPrefix(scope, family) && strings.Contains(model, family) {
					match = true
					break
				}
			}
		}
		if strings.Contains(scope, "opus") && !strings.Contains(model, "opus") || strings.Contains(scope, "sonnet") && !strings.Contains(model, "sonnet") {
			match = false
		}
		if !match || math.IsNaN(w.Remaining) || math.IsInf(w.Remaining, 0) {
			continue
		}
		found = true
		if w.Remaining <= 0 {
			return 0, quotaExhausted
		}
		if w.Remaining < score {
			score = w.Remaining
		}
	}
	if !found {
		return 0, quotaUnknown
	}
	if snapshot.State == quotaPartial {
		return score, quotaPartial
	}
	if snapshot.State == quotaUnlimited {
		return score, quotaUnlimited
	}
	return score, quotaKnown
}
