package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type headroomTestHost struct {
	*routingCatalogHost
	binding pluginapi.HostAffinityLookupResponse
}

func (h *headroomTestHost) AffinityLookup(pluginapi.HostAffinityLookupRequest) (pluginapi.HostAffinityLookupResponse, error) {
	return h.binding, nil
}

func TestRankHeadroomPolicies(t *testing.T) {
	route := testModelRoute("smart", routeStrategyHeadroom, 30, "model-a", "model-b")
	a := accountCredential{ID: "a", Provider: "p", AuthIndex: "a"}
	b := accountCredential{ID: "b", Provider: "p", AuthIndex: "b"}
	c := accountCredential{ID: "c", Provider: "p", AuthIndex: "c"}
	d := accountCredential{ID: "d", Provider: "p", AuthIndex: "d"}
	accounts := map[string][]accountCredential{"model-a": {a, b}, "model-b": {c, d}}
	snapshots := map[string]quotaSnapshot{
		quotaKey(a): {State: quotaKnown, Windows: []quotaWindow{{Remaining: 0.5}}},
		quotaKey(b): {State: quotaUnlimited},
		quotaKey(c): {State: quotaKnown, Windows: []quotaWindow{{Remaining: 0}}},
	}
	for _, policy := range []string{"priority", "last", "exclude"} {
		route.UnknownQuota = policy
		got, skips := rankHeadroom(route, accounts, snapshots, "smart", 0.5)
		want := 3
		if policy == "exclude" {
			want = 2
		}
		if len(got) != want || got[0].credential.ID != "b" || got[1].credential.ID != "a" || len(skips) != 4-want || skips[0].Reason != "quota_exhausted" {
			t.Fatalf("%s: candidates=%+v skips=%+v", policy, got, skips)
		}
		if want == 3 && got[2].credential.ID != "d" {
			t.Fatalf("unknown ordering: %+v", got)
		}
	}
}

func TestRankHeadroomTiesAndUnknownPolicies(t *testing.T) {
	route := testModelRoute("smart", routeStrategyHeadroom, 30, "model-a", "model-b")
	a := accountCredential{ID: "a", Provider: "p", AuthIndex: "a"}
	b := accountCredential{ID: "b", Provider: "p", AuthIndex: "b"}
	c := accountCredential{ID: "c", Provider: "p", AuthIndex: "c"}
	d := accountCredential{ID: "d", Provider: "p", AuthIndex: "d"}
	e := accountCredential{ID: "e", Provider: "p", AuthIndex: "e"}
	accounts := map[string][]accountCredential{"model-a": {b, a, d}, "model-b": {c, e}}
	snapshots := map[string]quotaSnapshot{
		quotaKey(a): {State: quotaKnown, Windows: []quotaWindow{{Remaining: 0.5}}},
		quotaKey(b): {State: quotaKnown, Windows: []quotaWindow{{Remaining: 0.5}}},
		quotaKey(c): {State: quotaKnown, Windows: []quotaWindow{{Remaining: 0.5}}},
	}
	for _, policy := range []string{"priority", "last", "exclude"} {
		t.Run(policy, func(t *testing.T) {
			route.UnknownQuota = policy
			got, skips := rankHeadroom(route, accounts, snapshots, "smart", 0.5)
			want := []string{"model-a/b", "model-a/a", "model-b/c"}
			if policy != "exclude" {
				want = append(want, "model-a/d", "model-b/e")
			}
			if len(got) != len(want) {
				t.Fatalf("candidates=%+v, want %v", got, want)
			}
			for i, candidate := range got {
				if candidate.target+"/"+candidate.credential.ID != want[i] {
					t.Fatalf("candidates=%+v, want %v", got, want)
				}
			}
			if policy == "exclude" {
				if len(skips) != 2 || skips[0].Credential.ID != "d" || skips[1].Credential.ID != "e" || skips[0].Reason != "quota_unknown" || skips[1].Reason != "quota_unknown" {
					t.Fatalf("skips=%+v", skips)
				}
			} else if len(skips) != 0 {
				t.Fatalf("unexpected skips: %+v", skips)
			}
		})
	}
}

func TestHeadroomAllowedAccountsAndIneligibleAffinity(t *testing.T) {
	route := testModelRoute("smart", routeStrategyHeadroom, 30, "model-a")
	route.Targets[0].AllowedAccounts = []string{"c", "a"}
	p := testRouterPlugin(route)
	h := &headroomTestHost{routingCatalogHost: routingTestHost(), binding: pluginapi.HostAffinityLookupResponse{Status: pluginapi.HostAffinityStatusBound, AuthIndex: "b-index"}}
	h.models[0].AuthIndex, h.models[1].AuthIndex, h.models[2].AuthIndex = "a-index", "b-index", "c-index"
	accounts, _, err := p.routeAccounts(route, "smart", h)
	if err != nil {
		t.Fatal(err)
	}
	candidates := p.headroomCandidates(context.Background(), pluginapi.ExecutorRequest{Model: "smart", Metadata: map[string]any{"session_id": "s"}}, h, route, accounts, p.emitter(route, correlationIdentity{}, ""))
	if len(candidates) != 2 || candidates[0].credential.ID != "a" || candidates[1].credential.ID != "c" {
		t.Fatalf("candidates=%+v", candidates)
	}
}

func TestHeadroomSelectionRedactsCredentialSecrets(t *testing.T) {
	credential := accountCredential{ID: "a", AuthIndex: "index", Provider: "p", ProjectID: "project-secret", Account: "account-secret"}
	route := testModelRoute("smart", routeStrategyHeadroom, 30, "model-a")
	p := testRouterPlugin(route)
	p.recordHeadroom(route, headroomCandidate{target: "model-a", credential: credential, score: 0.5, state: quotaKnown}, "")
	p.runtime.mu.Lock()
	selection := p.runtime.stateLocked(route).lastHeadroom
	p.runtime.mu.Unlock()
	data, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "project-secret") || strings.Contains(string(data), "account-secret") || !strings.Contains(string(data), `"auth_index":"index"`) {
		t.Fatalf("selection=%s", data)
	}
}

func TestHeadroomExecutionAndStream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "execute", true: "stream"}[stream], func(t *testing.T) {
			route := testModelRoute("smart", routeStrategyHeadroom, 30, "model-a")
			route.UnknownQuota = "priority"
			route.Targets[0].AllowedAccounts = []string{"a", "b", "c"}
			p := testRouterPlugin(route)
			p.attribution = newAttributionTracker(nil)
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
			if stream {
				if err := p.executeStreamWithHost(context.Background(), request, "out", h, nil); err != nil {
					t.Fatal(err)
				}
				if len(h.startCalls) != 2 || h.startCalls[0].AuthID != "b" || h.startCalls[0].ForcedProvider != "two" || h.startCalls[1].AuthID != "a" {
					t.Fatalf("calls: %+v", h.startCalls)
				}
			} else {
				response, err := p.executeWithHost(request, h)
				if err != nil {
					t.Fatal(err)
				}
				if len(h.executeCalls) != 2 || h.executeCalls[0].AuthID != "b" || h.executeCalls[0].ForcedProvider != "two" || h.executeCalls[1].AuthID != "a" || response.Metadata["quota_state"] != "unknown" {
					t.Fatalf("calls=%+v metadata=%+v", h.executeCalls, response.Metadata)
				}
			}
			if len(p.attribution.markers) != 2 || p.attribution.markers[1].attempt != 1 {
				t.Fatalf("attribution: %+v", p.attribution.markers)
			}
		})
	}
}

func TestHeadroomLastExecutesScoredAcrossTargetsBeforeUnknown(t *testing.T) {
	route := testModelRoute("smart", routeStrategyHeadroom, 30, "model-a", "model-b")
	route.UnknownQuota = "last"
	p := testRouterPlugin(route)
	p.attribution = newAttributionTracker(nil)
	p.config.MaxAttempts = 4
	p.quota = newQuotaPollingManager(pollerTestConfig())
	defer p.quota.Reconcile(routerConfig{})
	h := &headroomTestHost{routingCatalogHost: routingTestHost()}
	h.models = []pluginapi.HostAuthModels{
		{ID: "unknown-a", AuthIndex: "unknown-a", Provider: "one", Models: []string{"model-a"}},
		{ID: "scored-a", AuthIndex: "scored-a", Provider: "one", Models: []string{"model-a"}},
		{ID: "unknown-b", AuthIndex: "unknown-b", Provider: "two", Models: []string{"model-b"}},
		{ID: "scored-b", AuthIndex: "scored-b", Provider: "two", Models: []string{"model-b"}},
	}
	now := time.Now()
	for _, entry := range []struct {
		provider, id string
		state        quotaState
		remaining    float64
	}{
		{"one", "unknown-a", quotaUnknown, 0},
		{"one", "scored-a", quotaKnown, 0.8},
		{"two", "unknown-b", quotaUnknown, 0},
		{"two", "scored-b", quotaKnown, 0.6},
	} {
		credential := accountCredential{Provider: entry.provider, AuthIndex: entry.id}
		p.quota.entries[quotaKey(credential)] = &quotaPollEntry{hasSnapshot: true, snapshot: quotaSnapshot{
			State: entry.state, FetchedAt: now, Windows: []quotaWindow{{Remaining: entry.remaining}},
		}}
	}
	h.execute = func(r pluginapi.HostModelExecutionRequest) (pluginapi.HostModelExecutionResponse, error) {
		if r.AuthID != "unknown-b" {
			return pluginapi.HostModelExecutionResponse{StatusCode: http.StatusTooManyRequests}, nil
		}
		return pluginapi.HostModelExecutionResponse{StatusCode: http.StatusOK, Body: []byte(`{}`)}, nil
	}
	if _, err := p.executeWithHost(pluginapi.ExecutorRequest{Model: "smart"}, h); err != nil {
		t.Fatal(err)
	}
	want := []struct{ model, id, provider string }{
		{"model-a", "scored-a", "one"},
		{"model-b", "scored-b", "two"},
		{"model-a", "unknown-a", "one"},
		{"model-b", "unknown-b", "two"},
	}
	if len(h.executeCalls) != len(want) {
		t.Fatalf("calls=%+v", h.executeCalls)
	}
	for i, call := range h.executeCalls {
		if call.Model != want[i].model || call.AuthID != want[i].id || call.ForcedProvider != want[i].provider {
			t.Fatalf("call %d = %+v; want %+v", i, call, want[i])
		}
	}
}

func TestHeadroomPreflightCooldownAndLimit(t *testing.T) {
	route := testModelRoute("smart", routeStrategyHeadroom, 30, "model-a")
	route.Targets[0].AllowedAccounts = []string{"a", "b"}
	p := testRouterPlugin(route)
	p.attribution = newAttributionTracker(nil)
	p.config.MaxAttempts = 1
	h := &headroomTestHost{routingCatalogHost: routingTestHost(), binding: pluginapi.HostAffinityLookupResponse{Status: pluginapi.HostAffinityStatusBound, AuthIndex: "a-index"}}
	h.models[0].AuthIndex = "a-index"
	p.runtime.MarkAccountCooldown(route, "model-a", "a", time.Now().Add(time.Minute))
	h.execute = func(pluginapi.HostModelExecutionRequest) (pluginapi.HostModelExecutionResponse, error) {
		return pluginapi.HostModelExecutionResponse{StatusCode: http.StatusTooManyRequests}, nil
	}
	_, err := p.executeWithHost(pluginapi.ExecutorRequest{Model: "smart", Metadata: map[string]any{"session_id": "s"}}, h)
	if err == nil || len(h.executeCalls) != 1 || h.executeCalls[0].AuthID != "b" {
		t.Fatalf("err=%v calls=%+v", err, h.executeCalls)
	}
}

func TestHeadroomUnavailableCredentialsHonorRetryTime(t *testing.T) {
	route := testModelRoute("smart", routeStrategyHeadroom, 30, "model-a")
	p := testRouterPlugin(route)
	p.attribution = newAttributionTracker(nil)
	h := &headroomTestHost{routingCatalogHost: routingTestHost()}
	h.entries = []pluginapi.HostAuthFileEntry{
		{ID: "a", Unavailable: true},
		{ID: "b", Unavailable: true, NextRetryAfter: time.Now().Add(time.Minute)},
		{ID: "c", Unavailable: true, NextRetryAfter: time.Now().Add(-time.Minute)},
	}
	h.execute = func(pluginapi.HostModelExecutionRequest) (pluginapi.HostModelExecutionResponse, error) {
		return pluginapi.HostModelExecutionResponse{StatusCode: http.StatusOK}, nil
	}
	if _, err := p.executeWithHost(pluginapi.ExecutorRequest{Model: "smart"}, h); err != nil {
		t.Fatal(err)
	}
	if len(h.executeCalls) != 1 || h.executeCalls[0].AuthID != "c" {
		t.Fatalf("calls=%+v; want only the credential whose retry time has elapsed", h.executeCalls)
	}
}

func TestRankHeadroomScoresUnknownWithConfiguredPercent(t *testing.T) {
	known := accountCredential{ID: "known", Provider: "p", AuthIndex: "known"}
	unknown := accountCredential{ID: "unknown", Provider: "p", AuthIndex: "unknown"}
	route := modelRoute{Targets: []modelTarget{{Model: "m"}}, UnknownQuota: "priority"}
	accounts := map[string][]accountCredential{"m": {unknown, known}}
	snapshots := map[string]quotaSnapshot{quotaKey(known): {State: quotaKnown, Windows: []quotaWindow{{Remaining: 0.4}}}}
	for _, tc := range []struct {
		score float64
		first string
	}{{0.35, "known"}, {0.5, "unknown"}} {
		got, _ := rankHeadroom(route, accounts, snapshots, "m", tc.score)
		if got[0].credential.ID != tc.first {
			t.Fatalf("unknown score %v: first = %s", tc.score, got[0].credential.ID)
		}
	}
	route.UnknownQuota = "last"
	if got, _ := rankHeadroom(route, accounts, snapshots, "m", 1); got[0].credential.ID != "known" {
		t.Fatalf("last policy must rank unknown after known, got %s first", got[0].credential.ID)
	}
}
