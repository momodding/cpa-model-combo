package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestFetchQuotaProviderChecksPrecedeKey(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	t.Setenv("MODEL_COMBO_MANAGEMENT_URL", server.URL)
	for _, provider := range []string{"agy", "devin"} {
		for _, key := range []string{"", "k"} {
			if got := fetchQuota(context.Background(), accountCredential{Provider: provider, AuthIndex: "i"}, key); got.Snapshot.Err != "unsupported_provider" {
				t.Fatalf("%s/%q: %+v", provider, key, got.Snapshot)
			}
		}
	}
	if got := fetchQuota(context.Background(), accountCredential{Provider: "antigravity", AuthIndex: "i"}, ""); got.Snapshot.Err != "missing_project_id" {
		t.Fatalf("%+v", got.Snapshot)
	}
	if calls.Load() != 0 {
		t.Fatal("unexpected HTTP")
	}
}

func TestManagementKeyCaptureAndRecovery(t *testing.T) {
	t.Setenv("MODEL_COMBO_MANAGEMENT_KEY", "")
	var seenAuth atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"status_code":200,"body":"{\"five_hour\":{\"utilization\":20}}"}`))
	}))
	defer server.Close()
	t.Setenv("MODEL_COMBO_MANAGEMENT_URL", server.URL)
	m := newQuotaPollingManager(pollerTestConfig())
	defer m.Reconcile(routerConfig{})
	credential := accountCredential{Provider: "claude", AuthIndex: "a"}
	if got := m.Get(context.Background(), []accountCredential{credential})[quotaKey(credential)]; got.Err != "management_key_missing" {
		t.Fatalf("before capture: %+v", got)
	}
	p := &modelRouterPlugin{config: pollerTestConfig(), quota: m}
	cases := []struct {
		method, path string
		headers      http.Header
		captured     bool
	}{
		{http.MethodGet, modelRouterQuotaDiagnosticsPath, http.Header{"Authorization": {"Bearer test-key"}}, true},
		{http.MethodPost, modelRouterQuotaDiagnosticsPath, http.Header{"Authorization": {"Bearer wrong-method"}}, false},
		{http.MethodGet, modelRouterDashboardPath, http.Header{"Authorization": {"Bearer resource"}}, false},
		{http.MethodGet, "/v0/management/plugins/model-combo/unknown", http.Header{"Authorization": {"Bearer unknown"}}, false},
		{http.MethodGet, modelRouterAccountsPath, http.Header{"X-Management-Key": {"test-key"}}, true},
		{http.MethodGet, modelRouterAccountsPath, http.Header{"Authorization": {"test-key"}}, true},
	}
	for _, c := range cases {
		m.mu.Lock()
		m.managementKey = ""
		m.mu.Unlock()
		captureManagementKey(p, c.path, pluginapi.ManagementRequest{Method: c.method, Path: c.path, Headers: c.headers})
		m.mu.Lock()
		got := m.managementKey
		m.mu.Unlock()
		if (got != "") != c.captured {
			t.Fatalf("%s %s captured=%q", c.method, c.path, got)
		}
	}
	m.SetManagementKey("test-key")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := m.Get(context.Background(), []accountCredential{credential})[quotaKey(credential)]; got.State == quotaKnown {
			if seenAuth.Load() != "Bearer test-key" || got.Windows[0].Remaining != 0.8 {
				t.Fatalf("auth=%v windows=%+v", seenAuth.Load(), got.Windows)
			}
			body, _ := json.Marshal(quotaDiagnostics(&modelRouterPlugin{quota: m}))
			if strings.Contains(string(body), "test-key") || !strings.Contains(string(body), `"authentication":"available"`) {
				t.Fatalf("diagnostics: %s", body)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("blocked entry did not recover after key capture")
}

func TestManagementKeyPrecedenceAndRejection(t *testing.T) {
	t.Setenv("MODEL_COMBO_MANAGEMENT_KEY", "env-key")
	m := newQuotaPollingManager(pollerTestConfig())
	defer m.Reconcile(routerConfig{})
	var keys []string
	m.fetch = func(_ context.Context, _ accountCredential, key string) quotaFetchResult {
		keys = append(keys, key)
		return quotaFailure("test", "management_http_401", false)
	}
	credential := accountCredential{Provider: "claude", AuthIndex: "a"}
	m.Get(context.Background(), []accountCredential{credential})
	m.SetManagementKey("session-key")
	m.SetManagementKey("session-key")
	time.Sleep(100 * time.Millisecond)
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(keys) != 2 || keys[0] != "env-key" || keys[1] != "session-key" {
		t.Fatalf("keys = %v", keys)
	}
	m.mu.Unlock()
	got := quotaDiagnostics(&modelRouterPlugin{quota: m})
	m.mu.Lock()
	if got.Polling.Authentication != "rejected" {
		t.Fatalf("auth = %q", got.Polling.Authentication)
	}
}
