package main

import (
	"container/list"
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const (
	requestSessionRetention  = 24 * time.Hour
	maxStagedRequestSessions = 50_000

	hostModelCallbackSource = "plugin_host_model_callback"
)

// errRequestSessionTraceMismatch is deliberately free of request IDs and payloads.
var errRequestSessionTraceMismatch = errors.New("request lifecycle trace ID differs from the staged inbound trace ID; session dropped")

var errRequestSessionMissingTrace = errors.New("request lifecycle completion has no usable inbound trace ID; session dropped")

type stagedRequestSession struct {
	requestID      string
	traceID        string
	endpoint       string
	requestedModel string
	routerModel    string // empty when the requested model is not a configured route
	stream         bool
	startedAt      time.Time
}

// requestSessionTracker stages external requests by lifecycle request ID until completion.
// The list is ordered by staging time, so pruning only touches the oldest entries.
type requestSessionTracker struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List
	now     func() time.Time
}

func newRequestSessionTracker(now func() time.Time) *requestSessionTracker {
	if now == nil {
		now = time.Now
	}
	return &requestSessionTracker{entries: map[string]*list.Element{}, order: list.New(), now: now}
}

func (tracker *requestSessionTracker) Stage(session stagedRequestSession) {
	if tracker == nil || session.requestID == "" {
		return
	}
	session.startedAt = tracker.now().UTC()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.pruneLocked(session.startedAt)
	if existing := tracker.entries[session.requestID]; existing != nil {
		tracker.order.Remove(existing)
	}
	tracker.entries[session.requestID] = tracker.order.PushBack(session)
	for tracker.order.Len() > maxStagedRequestSessions {
		tracker.removeLocked(tracker.order.Front())
	}
}

// Consume removes and returns the staged request; ok is false for unknown, expired or already consumed IDs.
func (tracker *requestSessionTracker) Consume(requestID string) (stagedRequestSession, bool) {
	if tracker == nil || requestID == "" {
		return stagedRequestSession{}, false
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.pruneLocked(tracker.now().UTC())
	element := tracker.entries[requestID]
	if element == nil {
		return stagedRequestSession{}, false
	}
	session := element.Value.(stagedRequestSession)
	tracker.removeLocked(element)
	return session, true
}

func (tracker *requestSessionTracker) pruneLocked(now time.Time) {
	cutoff := now.Add(-requestSessionRetention)
	for element := tracker.order.Front(); element != nil && element.Value.(stagedRequestSession).startedAt.Before(cutoff); element = tracker.order.Front() {
		tracker.removeLocked(element)
	}
}

func (tracker *requestSessionTracker) removeLocked(element *list.Element) {
	delete(tracker.entries, element.Value.(stagedRequestSession).requestID)
	tracker.order.Remove(element)
}

// stageRequestSession records an external inbound request; host-internal model callbacks are children, not parents.
func (p *modelRouterPlugin) stageRequestSession(request pluginapi.RequestInterceptRequest, routerModel string) {
	if !p.requestIdentityEnabled || p.requestSessions == nil {
		return
	}
	if source, _ := request.Metadata["source"].(string); source == hostModelCallbackSource {
		return
	}
	endpoint, _ := request.Metadata["request_path"].(string)
	p.requestSessions.Stage(stagedRequestSession{
		requestID:      strings.TrimSpace(request.RequestID),
		traceID:        strings.TrimSpace(request.TraceID),
		endpoint:       strings.TrimSpace(endpoint),
		requestedModel: strings.TrimSpace(request.RequestedModel),
		routerModel:    routerModel,
		stream:         request.Stream,
	})
}

// HandleRequestComplete persists the final outcome of a staged external request. Completion.Error is never stored.
// Parent key is the staged inbound TraceID. Only a non-HTTP call with no trace on either side (no staged
// request_path) falls back to the execution ID; an HTTP request without a trace is rejected rather than misgrouped.
func (p *modelRouterPlugin) HandleRequestComplete(_ context.Context, completion pluginapi.RequestCompletion) error {
	if p == nil || !p.config.Enabled || !p.requestIdentityEnabled || p.store == nil {
		return nil
	}
	staged, ok := p.requestSessions.Consume(strings.TrimSpace(completion.RequestID))
	if !ok {
		return nil
	}
	completionTraceID := strings.TrimSpace(completion.TraceID)
	if staged.traceID != "" && completionTraceID != "" && staged.traceID != completionTraceID {
		p.store.recordError(errRequestSessionTraceMismatch)
		return errRequestSessionTraceMismatch
	}
	parentID := staged.traceID
	if parentID == "" {
		if staged.endpoint != "" || completionTraceID != "" {
			p.store.recordError(errRequestSessionMissingTrace)
			return errRequestSessionMissingTrace
		}
		parentID = strings.TrimSpace(completion.RequestID)
	}
	startedAt := completion.StartedAt.UTC()
	if completion.StartedAt.IsZero() {
		startedAt = staged.startedAt
	}
	completedAt := completion.CompletedAt.UTC()
	if completion.CompletedAt.IsZero() {
		completedAt = p.requestSessions.now().UTC()
	}
	var latency uint64
	if completedAt.After(startedAt) {
		latency = uint64(completedAt.Sub(startedAt))
	}
	attribution := attributionDirect
	if staged.routerModel != "" {
		attribution = attributionRouted
	}
	return p.store.RecordSession(storedRequestSession{
		RequestID:      parentID,
		Endpoint:       staged.endpoint,
		RequestedModel: firstNonEmpty(staged.requestedModel, completion.RequestedModel),
		RouterModel:    staged.routerModel,
		Attribution:    attribution,
		Stream:         staged.stream || completion.Stream,
		Outcome:        string(completion.Outcome),
		StatusCode:     completion.StatusCode,
		StartedAt:      startedAt,
		CompletedAt:    completedAt,
		LatencyNS:      latency,
	})
}
