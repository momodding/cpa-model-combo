package main

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestAttributionTrackerRoutedDirectAndConflict(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	tracker := newAttributionTracker(func() time.Time { return now })
	headers := http.Header{"Authorization": {"Bearer sk-client-ab"}}

	tracker.MarkRouted("auto", "openai/gpt-5.4(high)", headers, "router-request", 7)
	routed := tracker.Match(pluginapi.UsageRecord{Model: "openai/gpt-5.4", Alias: "gpt-5.4(high)", APIKey: "sk-client-ab", RequestedAt: now})
	if routed.Kind != attributionRouted || routed.RouterModel != "auto" || routed.RouterRequestID != "router-request" || routed.Attempt != 7 {
		t.Fatalf("routed attribution = %#v", routed)
	}

	tracker.MarkDirect(pluginapi.ModelRouteRequest{RequestedModel: "gpt-5.4", Headers: headers})
	direct := tracker.Match(pluginapi.UsageRecord{Model: "gpt-5.4", APIKey: "sk-client-ab", RequestedAt: now})
	if direct.Kind != attributionDirect || direct.RouterModel != "" || direct.RouterRequestID != "" || direct.Attempt != 0 {
		t.Fatalf("direct attribution = %#v", direct)
	}

	tracker.MarkRouted("auto", "gpt-5.4", headers, "conflict", 0)
	tracker.MarkDirect(pluginapi.ModelRouteRequest{RequestedModel: "gpt-5.4", Headers: headers})
	conflict := tracker.Match(pluginapi.UsageRecord{Model: "gpt-5.4", APIKey: "sk-client-ab", RequestedAt: now})
	if conflict.Kind != attributionUnresolved {
		t.Fatalf("conflicting attribution = %#v", conflict)
	}
}

func TestAttributionTrackerRequiresCredentialAndUnanimousRoute(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	tracker := newAttributionTracker(func() time.Time { return now })
	tracker.MarkRouted("auto", "gpt-5.4", http.Header{"X-Api-Key": {"key-one"}}, "mismatch", 0)
	if got := tracker.Match(pluginapi.UsageRecord{Model: "gpt-5.4", APIKey: "key-two", RequestedAt: now}); got.Kind != attributionUnresolved {
		t.Fatalf("credential mismatch = %#v", got)
	}
	tracker.MarkRouted("auto", "gpt-5.4", nil, "route-one", 0)
	tracker.MarkRouted("cheap", "gpt-5.4", nil, "route-two", 0)
	if got := tracker.Match(pluginapi.UsageRecord{Model: "gpt-5.4", RequestedAt: now}); got.Kind != attributionUnresolved {
		t.Fatalf("route disagreement = %#v", got)
	}
}

func TestAttributionTrackerMatchesTimestampLessUsageAfterWindow(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	tracker := newAttributionTracker(func() time.Time { return now })
	tracker.MarkRouted("auto", "gpt-5.4", http.Header{"X-Api-Key": {"key-one"}}, "late", 0)
	now = now.Add(attributionWindow + time.Second)
	got := tracker.Match(pluginapi.UsageRecord{Model: "gpt-5.4", APIKey: "key-one"})
	if got.Kind != attributionRouted || got.RouterModel != "auto" {
		t.Fatalf("timestamp-less attribution = %#v", got)
	}
}

func TestAttributionTrackerLeavesAmbiguousTimestampLessUsageUnattributed(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	tracker := newAttributionTracker(func() time.Time { return now })
	headers := http.Header{"X-Api-Key": {"key-one"}}
	tracker.MarkRouted("first", "gpt-5.4", headers, "first", 0)
	now = now.Add(time.Second)
	tracker.MarkRouted("second", "gpt-5.4", headers, "second", 0)
	if got := tracker.Match(pluginapi.UsageRecord{Model: "gpt-5.4", APIKey: "key-one"}); got.Kind != attributionUnresolved {
		t.Fatalf("ambiguous timestamp-less attribution = %#v", got)
	}
	if len(tracker.markers) != 2 {
		t.Fatalf("active markers after ambiguous match = %#v", tracker.markers)
	}
}

func TestAttributionTrackerExpiresAndCapsMarkers(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	tracker := newAttributionTracker(func() time.Time { return now })
	tracker.MarkRouted("old", "model", nil, "old", 0)
	now = now.Add(attributionRetention + time.Second)
	tracker.MarkRouted("new", "model", nil, "new", 0)
	if len(tracker.markers) != 1 || tracker.markers[0].routerModel != "new" {
		t.Fatalf("expired markers = %#v", tracker.markers)
	}
	for index := 0; index < maxAttributionMarks+10; index++ {
		tracker.MarkDirect(pluginapi.ModelRouteRequest{RequestedModel: "model"})
	}
	if len(tracker.markers) != maxAttributionMarks {
		t.Fatalf("marker count = %d", len(tracker.markers))
	}
}

func TestMaskAPIKey(t *testing.T) {
	for input, want := range map[string]string{"sk-client-ab": "sk******ab", "abcde": "******", "abcd": "******", "": ""} {
		if got := maskAPIKey(input); got != want {
			t.Fatalf("maskAPIKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCorrelationLifecycleAndSpoofRejection(t *testing.T) {
	now := time.Now().UTC()
	store, err := openUsageStore(filepath.Join(t.TempDir(), "usage.db"), 365)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tracker := newAttributionTracker(func() time.Time { return now })
	p := &modelRouterPlugin{config: routerConfig{Enabled: true, Routes: []modelRoute{{Alias: "auto"}}}, attribution: tracker, store: store}
	request := pluginapi.RequestInterceptRequest{RequestID: "one", TraceID: "trace-one", RequestedModel: "auto", Headers: http.Header{correlationHeader: {"spoof"}}}
	first, err := p.InterceptRequestBeforeAuth(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	token := first.Headers.Get(correlationHeader)
	if token == "" || token == "spoof" || len(first.ClearHeaders) != 1 || first.ClearHeaders[0] != correlationHeader {
		t.Fatalf("interception = %#v", first)
	}
	request.RequestID, request.TraceID = "two", "trace-two"
	second, err := p.InterceptRequestBeforeAuth(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Headers.Get(correlationHeader) == token {
		t.Fatal("correlation tokens reused")
	}
	identity := p.resolveCorrelation(first.Headers)
	if identity.TraceID != "trace-one" || identity.LifecycleRequestID != "one" {
		t.Fatalf("identity = %#v", identity)
	}
	if got := p.resolveCorrelation(http.Header{correlationHeader: {"spoof"}}); got != (correlationIdentity{}) {
		t.Fatalf("spoof accepted: %#v", got)
	}
	p.recordRoutingSkip(identity, routingSkip{Model: "first"})
	if got := p.nextCorrelationAttempt(identity); got != 1 {
		t.Fatalf("dispatch ordinal = %d", got)
	}
	p.recordRoutingSkip(p.resolveCorrelation(first.Headers), routingSkip{Model: "second"})
	if events := tracker.correlations[token].skips; len(events) != 2 || events[0].Attempt != 0 || events[1].Attempt != 2 {
		t.Fatalf("events = %#v", events)
	}
	var persisted int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM routing_skips WHERE trace_id = ? AND lifecycle_request_id = ?`, "trace-one", "one").Scan(&persisted); err != nil || persisted != 2 {
		t.Fatalf("persisted skips = %d, %v", persisted, err)
	}
	if err := p.HandleRequestComplete(context.Background(), pluginapi.RequestCompletion{RequestID: "one", TraceID: "wrong"}); err != nil {
		t.Fatal(err)
	}
	if p.resolveCorrelation(first.Headers) != identity {
		t.Fatal("mismatched completion removed correlation")
	}
	if err := p.HandleRequestComplete(context.Background(), pluginapi.RequestCompletion{RequestID: "one", TraceID: "trace-one"}); err != nil {
		t.Fatal(err)
	}
	if got := p.resolveCorrelation(first.Headers); got != (correlationIdentity{}) {
		t.Fatal("completed correlation retained")
	}
	if got := p.resolveCorrelation(second.Headers); got.TraceID != "trace-two" {
		t.Fatalf("other correlation lost: %#v", got)
	}
	now = now.Add(attributionRetention)
	if got := p.resolveCorrelation(second.Headers); got != (correlationIdentity{}) {
		t.Fatal("expired correlation retained")
	}
	request.RequestedModel = "direct"
	direct, err := p.InterceptRequestBeforeAuth(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if direct.Headers.Get(correlationHeader) != "" || len(direct.ClearHeaders) != 1 || direct.ClearHeaders[0] != correlationHeader {
		t.Fatalf("direct interception = %#v", direct)
	}
}

func TestBeforeAuthCorrelationHeaderReplacement(t *testing.T) {
	p := &modelRouterPlugin{config: routerConfig{Enabled: true, Routes: []modelRoute{{Alias: "auto"}}}, attribution: newAttributionTracker(nil)}
	for _, model := range []string{"direct", "auto"} {
		t.Run(model, func(t *testing.T) {
			request := pluginapi.RequestInterceptRequest{RequestID: "request", TraceID: "trace", RequestedModel: model, Headers: http.Header{correlationHeader: {"spoof"}}}
			response, err := p.InterceptRequestBeforeAuth(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			// CPA's before-auth adapter applies clear/updates to a non-nil header map.
			headers := request.Headers.Clone()
			for _, key := range response.ClearHeaders {
				headers.Del(key)
			}
			for key, values := range response.Headers {
				headers[key] = append([]string(nil), values...)
			}
			if model == "direct" && len(headers) != 0 {
				t.Fatalf("private header not removed: %#v", headers)
			}
			if model == "auto" && (headers.Get(correlationHeader) == "" || headers.Get(correlationHeader) == "spoof") {
				t.Fatalf("spoof not replaced: %#v", headers)
			}
		})
	}
}
