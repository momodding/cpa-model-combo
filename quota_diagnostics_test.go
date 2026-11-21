package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestQuotaDiagnosticsManagement(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	route := modelRoute{Alias: "smart", Strategy: routeStrategyHeadroom}
	cfg := pollerTestConfig()
	cfg.Routes = []modelRoute{route}
	m := newQuotaPollingManager(cfg)
	m.now = func() time.Time { return now }
	m.running = true
	m.lastTick = now
	credential := accountCredential{ID: "credential", Provider: "claude", AuthIndex: "index", Label: "label", ProjectID: "secret-project", Account: "secret-account"}
	m.entries[quotaKey(credential)] = &quotaPollEntry{credential: credential, hasSnapshot: true, snapshot: quotaSnapshot{State: quotaKnown, Source: "claude", FetchedAt: now.Add(-time.Second), Windows: []quotaWindow{{Name: "five_hour", Remaining: 0.8}}}}
	runtime := newRouteRuntime(nil)
	runtime.Sync(cfg.Routes)
	runtime.states[routeKey(route.Alias)].lastHeadroom = headroomSelection{Model: "model", Credential: credential, Score: 0.8, State: "known", Reason: "fallback"}
	runtime.states[routeKey(route.Alias)].headroomSkips = []headroomSelection{{Model: "other", Credential: credential, State: "exhausted", Reason: "quota_exhausted"}}
	p := &modelRouterPlugin{config: cfg, quota: m, runtime: runtime}
	t.Setenv("MODEL_COMBO_MANAGEMENT_KEY", "secret-management-key")
	response := handleModelRouterManagement(p, pluginapi.ManagementRequest{Method: http.MethodGet, Path: modelRouterQuotaDiagnosticsPath})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	for _, secret := range []string{"secret-project", "secret-account", "secret-management-key"} {
		if strings.Contains(string(response.Body), secret) {
			t.Fatalf("diagnostics exposed secret %q", secret)
		}
	}
	var got quotaDiagnosticResponse
	if err := json.Unmarshal(response.Body, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Polling.Running || got.Polling.ActiveKeys != 1 || got.Polling.LastTick == nil || len(got.Credentials) != 1 {
		t.Fatalf("polling = %+v credentials = %+v", got.Polling, got.Credentials)
	}
	c := got.Credentials[0]
	if c.State != "known" || c.Label != "label" || c.AgeSeconds == nil || *c.AgeSeconds != 1 || len(c.Windows) != 1 || c.Windows[0].Remaining != 0.8 {
		t.Fatalf("credential = %+v", c)
	}
	if len(got.Routes) != 1 || got.Routes[0].Selection == nil || got.Routes[0].Selection.Reason != "fallback" || len(got.Routes[0].Skips) != 1 || got.Routes[0].Skips[0].Reason != "quota_exhausted" {
		t.Fatalf("routes = %+v", got.Routes)
	}
	response = handleModelRouterManagement(p, pluginapi.ManagementRequest{Method: http.MethodPost, Path: modelRouterQuotaDiagnosticsPath})
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", response.StatusCode)
	}
}

func TestQuotaDiagnosticsPendingAndStale(t *testing.T) {
	now := time.Now()
	m := newQuotaPollingManager(pollerTestConfig())
	m.now = func() time.Time { return now }
	m.entries["pending"] = &quotaPollEntry{credential: accountCredential{AuthIndex: "pending"}}
	m.entries["stale"] = &quotaPollEntry{credential: accountCredential{AuthIndex: "stale"}, hasSnapshot: true, snapshot: quotaSnapshot{State: quotaKnown, FetchedAt: now.Add(-9 * time.Second), Windows: []quotaWindow{{Remaining: 1}}}}
	got := quotaDiagnostics(&modelRouterPlugin{quota: m})
	if len(got.Credentials) != 2 || got.Credentials[0].Error != "pending" || got.Credentials[0].State != "unknown" || got.Credentials[1].Error != "stale" || got.Credentials[1].State != "unknown" || len(got.Credentials[1].Windows) != 0 {
		t.Fatalf("credentials = %+v", got.Credentials)
	}
}
