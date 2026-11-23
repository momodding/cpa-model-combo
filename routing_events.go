package main

import (
	"sync"
	"time"
)

type routingEvent struct {
	Seq                uint64                   `json:"seq"`
	At                 time.Time                `json:"at"`
	Route              string                   `json:"route"`
	RouterRequestID    string                   `json:"router_request_id"`
	TraceID            string                   `json:"trace_id,omitempty"`
	LifecycleRequestID string                   `json:"lifecycle_request_id,omitempty"`
	Attempt            int                      `json:"attempt"`
	State              string                   `json:"state"` // pending|evaluating|selected|executing|success|failed|skipped|exhausted|cooldown|fallback
	Model              string                   `json:"model,omitempty"`
	Credential         *quotaDiagnosticIdentity `json:"credential,omitempty"`
	Score              *float64                 `json:"score,omitempty"`
	QuotaState         string                   `json:"quota_state,omitempty"`
	Reason             string                   `json:"reason,omitempty"`
	RetryAt            *time.Time               `json:"retry_at,omitempty"`
}

const routingEventCapacity = 512

type routingEventLog struct {
	mu  sync.Mutex
	now func() time.Time
	seq uint64
	buf []routingEvent
}

func newRoutingEventLog(now func() time.Time) *routingEventLog {
	if now == nil {
		now = time.Now
	}
	return &routingEventLog{now: now}
}

// clone detaches pointer fields so readers cannot mutate ring state.
func (e routingEvent) clone() routingEvent {
	if e.Credential != nil {
		c := *e.Credential
		e.Credential = &c
	}
	if e.Score != nil {
		s := *e.Score
		e.Score = &s
	}
	if e.RetryAt != nil {
		r := *e.RetryAt
		e.RetryAt = &r
	}
	return e
}

func (l *routingEventLog) add(e routingEvent) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	e = e.clone()
	e.Seq = l.seq
	e.At = l.now()
	if len(l.buf) >= routingEventCapacity {
		copy(l.buf, l.buf[1:])
		l.buf[len(l.buf)-1] = e
	} else {
		l.buf = append(l.buf, e)
	}
}

// since returns events with Seq > cursor and the latest Seq. truncated is true only when a
// positive cursor has a real gap: events between cursor and the oldest retained Seq were dropped.
func (l *routingEventLog) since(cursor uint64) ([]routingEvent, uint64, bool) {
	events := []routingEvent{}
	if l == nil {
		return events, 0, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) == 0 {
		return events, l.seq, false
	}
	oldest := l.buf[0].Seq
	truncated := cursor > 0 && cursor < oldest && oldest-cursor > 1
	for _, e := range l.buf {
		if e.Seq > cursor {
			events = append(events, e.clone())
		}
	}
	return events, l.seq, truncated
}

func (p *modelRouterPlugin) routingEvent(route modelRoute, identity correlationIdentity, routerRequestID string, attempt int, state, model string, credential accountCredential, set func(*routingEvent)) {
	if p == nil || p.events == nil {
		return
	}
	ev := routingEvent{
		Route:              route.Alias,
		RouterRequestID:    routerRequestID,
		TraceID:            identity.TraceID,
		LifecycleRequestID: identity.LifecycleRequestID,
		Attempt:            attempt,
		State:              state,
		Model:              model,
	}
	if credential.ID != "" {
		ev.Credential = &quotaDiagnosticIdentity{ID: credential.ID, Provider: credential.Provider, AuthIndex: credential.AuthIndex, Label: credential.Label}
	}
	if set != nil {
		set(&ev)
	}
	p.events.add(ev)
}

type emitFunc func(attempt int, state, model string, credential accountCredential, set func(*routingEvent))

func (p *modelRouterPlugin) emitter(route modelRoute, identity correlationIdentity, routerRequestID string) emitFunc {
	return func(attempt int, state, model string, credential accountCredential, set func(*routingEvent)) {
		p.routingEvent(route, identity, routerRequestID, attempt, state, model, credential, set)
	}
}

// emitPreflight records the route evaluation and every cooldown that Preflight skipped.
func emitPreflight(emit emitFunc, skips []routingSkip) {
	emit(0, "evaluating", "", accountCredential{}, nil)
	for _, skip := range skips {
		retryAt := skip.RetryAt
		emit(0, "cooldown", skip.Model, skip.Credential, func(e *routingEvent) { e.RetryAt = &retryAt; e.Reason = "cooldown" })
	}
}

// emitCooldown mirrors the cooldown Mark*Failure just applied; it never takes runtime.mu while the caller holds it.
func (p *modelRouterPlugin) emitCooldown(emit emitFunc, route modelRoute, attempt int, model string, credential accountCredential, reason string) {
	if p == nil || p.runtime == nil {
		return
	}
	retryAt := p.runtime.now().Add(cooldownFor(route))
	emit(attempt, "cooldown", model, credential, func(e *routingEvent) { e.RetryAt = &retryAt; e.Reason = reason })
}

func withScore(score float64, state, reason string) func(*routingEvent) {
	return func(e *routingEvent) { e.Score = &score; e.QuotaState = state; e.Reason = reason }
}
