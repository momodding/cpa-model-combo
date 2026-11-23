package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestRoutingEventLogWrapAndCursor(t *testing.T) {
	var nilLog *routingEventLog
	nilLog.add(routingEvent{})
	if ev, latest, trunc := nilLog.since(0); len(ev) != 0 || latest != 0 || trunc {
		t.Fatal("nil log must be empty")
	}

	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := newRoutingEventLog(func() time.Time { return stamp })
	for range routingEventCapacity + 8 {
		l.add(routingEvent{At: stamp.Add(-time.Hour), State: "pending", Credential: &quotaDiagnosticIdentity{ID: "a"}})
	}
	// 520 added, oldest retained Seq = 9
	all, latest, trunc := l.since(0)
	if len(all) != routingEventCapacity || latest != 520 || trunc || all[0].Seq != 9 || !all[0].At.Equal(stamp) {
		t.Fatalf("len=%d latest=%d trunc=%v first=%+v", len(all), latest, trunc, all[0])
	}
	for cursor, want := range map[uint64]bool{8: false, 7: true, 1: true, 520: false, ^uint64(0): false} {
		if _, _, got := l.since(cursor); got != want {
			t.Fatalf("cursor %d truncated=%v want %v", cursor, got, want)
		}
	}
	if ev, _, _ := l.since(520); len(ev) != 0 {
		t.Fatalf("since(latest) = %d events", len(ev))
	}

	all[0].Credential.ID = "mutated"
	again, _, _ := l.since(0)
	if again[0].Credential.ID != "a" {
		t.Fatal("returned credential aliases ring state")
	}
}

func TestHeadroomEmitsOrderedRoutingEvents(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "execute", true: "stream"}[stream], func(t *testing.T) {
			route := testModelRoute("smart", routeStrategyHeadroom, 30, "model-a")
			route.UnknownQuota = "priority"
			route.Targets[0].AllowedAccounts = []string{"a", "b", "c"}
			p := testRouterPlugin(route)
			p.attribution = newAttributionTracker(nil)
			p.events = newRoutingEventLog(nil)
			p.config.MaxAttempts = 2
			h := &headroomTestHost{routingCatalogHost: routingTestHost(), binding: pluginapi.HostAffinityLookupResponse{Status: pluginapi.HostAffinityStatusBound, AuthIndex: "b-index"}}
			h.models[0].AuthIndex, h.models[1].AuthIndex, h.models[2].AuthIndex = "a-index", "b-index", "c-index"
			p.runtime.MarkAccountFailure(route, "model-a", "c")
			h.execute = func(r pluginapi.HostModelExecutionRequest) (pluginapi.HostModelExecutionResponse, error) {
				if r.AuthID == "b" {
					return pluginapi.HostModelExecutionResponse{StatusCode: 429}, nil
				}
				return pluginapi.HostModelExecutionResponse{StatusCode: 200, Body: []byte(`{}`)}, nil
			}
			h.start = func(r pluginapi.HostModelExecutionRequest) (pluginapi.HostModelStreamResponse, error) {
				if r.AuthID == "b" {
					return pluginapi.HostModelStreamResponse{StatusCode: 429}, nil
				}
				return pluginapi.HostModelStreamResponse{StatusCode: 200, StreamID: "upstream"}, nil
			}
			h.reads = map[string][]pluginapi.HostModelStreamReadResponse{"upstream": {{Done: true}}}
			request := pluginapi.ExecutorRequest{Model: "smart", Metadata: map[string]any{"session_id": "session"}}
			var err error
			if stream {
				err = p.executeStreamWithHost(context.Background(), request, "out", h, nil)
			} else {
				_, err = p.executeWithHost(request, h)
			}
			if err != nil {
				t.Fatal(err)
			}
			events, _, _ := p.events.since(0)
			var got []string
			for _, e := range events {
				got = append(got, e.State+":"+credentialID(e))
				if e.RouterRequestID == "" || e.Route != "smart" {
					t.Fatalf("event missing correlation: %+v", e)
				}
			}
			want := "evaluating: cooldown:c pending:b pending:a selected:b executing:b failed:b cooldown:b fallback:b selected:a executing:a success:a"
			if strings.Join(got, " ") != want {
				t.Fatalf("events\n got %v\nwant %s", got, want)
			}
		})
	}
}

func credentialID(e routingEvent) string {
	if e.Credential == nil {
		return ""
	}
	return e.Credential.ID
}

func TestQuotaDiagnosticsEventsAndCooldowns(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	route := testModelRoute("smart", routeStrategyPriority, 30, "model-a")
	p := testRouterPlugin(route)
	p.events = newRoutingEventLog(func() time.Time { return now })
	p.runtime.now = func() time.Time { return now }
	p.runtime.MarkCooldown(route, "model-a", now.Add(time.Minute))
	p.runtime.MarkAccountCooldown(route, "model-a", "live", now.Add(time.Minute))
	p.runtime.MarkAccountCooldown(route, "model-a", "expired", now.Add(-time.Minute))
	for range 3 {
		p.events.add(routingEvent{State: "pending"})
	}

	get := func(query url.Values) (int, quotaDiagnosticResponse) {
		response := handleModelRouterManagement(p, pluginapi.ManagementRequest{Method: http.MethodGet, Path: modelRouterQuotaDiagnosticsPath, Query: query})
		var body quotaDiagnosticResponse
		_ = json.Unmarshal(response.Body, &body)
		return response.StatusCode, body
	}
	status, body := get(url.Values{"events_since": {"2"}})
	if status != http.StatusOK || len(body.Events) != 1 || body.Events[0].Seq != 3 || body.EventCursor != 3 || body.EventsTruncated {
		t.Fatalf("status=%d body=%+v", status, body)
	}
	if len(body.Cooldowns) != 2 || body.Cooldowns[0].CredentialID != "" || body.Cooldowns[1].CredentialID != "live" {
		t.Fatalf("cooldowns=%+v", body.Cooldowns)
	}
	if status, _ := get(url.Values{"events_since": {"x"}}); status != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d", status)
	}
	if status, body := get(nil); status != http.StatusOK || len(body.Events) != 3 {
		t.Fatalf("default cursor: status=%d events=%d", status, len(body.Events))
	}
}

func TestUnrestrictedModelFailureEmitsCooldownBeforeFallback(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "execute", true: "stream"}[stream], func(t *testing.T) {
			route := testModelRoute("smart", routeStrategyPriority, 30, "model-a", "model-b")
			p := testRouterPlugin(route)
			p.attribution = newAttributionTracker(nil)
			p.events = newRoutingEventLog(nil)
			p.config.MaxAttempts = 2
			h := routingTestHost()
			h.execute = func(r pluginapi.HostModelExecutionRequest) (pluginapi.HostModelExecutionResponse, error) {
				if r.Model == "model-a" {
					return pluginapi.HostModelExecutionResponse{StatusCode: 429}, nil
				}
				return pluginapi.HostModelExecutionResponse{StatusCode: 200, Body: []byte(`{}`)}, nil
			}
			h.start = func(r pluginapi.HostModelExecutionRequest) (pluginapi.HostModelStreamResponse, error) {
				if r.Model == "model-a" {
					return pluginapi.HostModelStreamResponse{StatusCode: 429}, nil
				}
				return pluginapi.HostModelStreamResponse{StatusCode: 200, StreamID: "upstream"}, nil
			}
			h.reads = map[string][]pluginapi.HostModelStreamReadResponse{"upstream": {{Done: true}}}
			request := pluginapi.ExecutorRequest{Model: "smart"}
			var err error
			if stream {
				err = p.executeStreamWithHost(context.Background(), request, "out", h, nil)
			} else {
				_, err = p.executeWithHost(request, h)
			}
			if err != nil {
				t.Fatal(err)
			}
			events, _, _ := p.events.since(0)
			var got []string
			for _, e := range events {
				if e.Model == "model-a" {
					got = append(got, e.State)
				}
			}
			want := "selected executing failed cooldown fallback"
			if strings.Join(got, " ") != want {
				t.Fatalf("model-a events %v, want %s", got, want)
			}
		})
	}
}
