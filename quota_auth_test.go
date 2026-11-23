package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	for _, provider := range []string{"nosuch"} {
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
			body, _ := json.Marshal(quotaDiagnostics(&modelRouterPlugin{quota: m}, 0))
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
	got := quotaDiagnostics(&modelRouterPlugin{quota: m}, 0)
	m.mu.Lock()
	if got.Polling.Authentication != "rejected" {
		t.Fatalf("auth = %q", got.Polling.Authentication)
	}
}

func TestDevinQuotaRequestAndParse(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"status_code":200,"body":{"userStatus":{"planStatus":{"dailyQuotaRemainingPercent":75,"weeklyQuotaRemainingPercent":"0","dailyQuotaResetAtUnix":"1900000000","weeklyQuotaResetAtUnix":-1}}}}`))
	}))
	defer server.Close()
	t.Setenv("MODEL_COMBO_MANAGEMENT_URL", server.URL)
	got := fetchQuota(context.Background(), accountCredential{Provider: "devin", AuthIndex: "d1"}, "k").Snapshot
	if gotBody["auth_index"] != "d1" || gotBody["url"] != devinQuotaURL || !strings.Contains(gotBody["data"].(string), `"$TOKEN$"`) {
		t.Fatalf("%v", gotBody)
	}
	if got.State != quotaKnown || len(got.Windows) != 2 || got.Windows[0].Remaining != .75 || got.Windows[1].Remaining != 0 || got.Windows[1].ResetAt.IsZero() == false || got.Windows[0].ResetAt.IsZero() {
		t.Fatalf("%+v", got)
	}
	for body, want := range map[string]quotaState{`{"userStatus":{"planStatus":{"dailyQuotaRemainingPercent":101,"weeklyQuotaRemainingPercent":null,"dailyQuotaResetAtUnix":5}}}`: quotaUnknown, `{"userStatus":{"planStatus":{"dailyQuotaRemainingPercent":5}}}`: quotaPartial, `{`: quotaUnknown} {
		if s := parseDevinQuota([]byte(body)); s.State != want {
			t.Fatalf("%s: %+v", body, s)
		}
	}
}

func TestAgyQuotaDelegatesPerAccount(t *testing.T) {
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/v0/management/quota/fetch" || len(req) != 1 {
			t.Errorf("%s %v", r.URL.Path, req)
		}
		w.WriteHeader(status)
		w.Write([]byte(`{"groups":[{"display_name":"Pro","buckets":[{"window":"5h","remaining_fraction":0,"reset_time":"` + map[string]string{"a": "2030-01-01T00:00:00Z", "b": "bad"}[req["auth_index"]] + `"},{"window":"7d"}]}]}`))
	}))
	defer server.Close()
	t.Setenv("MODEL_COMBO_MANAGEMENT_URL", server.URL)
	a := fetchQuota(context.Background(), accountCredential{Provider: "agy", AuthIndex: "a"}, "k").Snapshot
	b := fetchQuota(context.Background(), accountCredential{Provider: "agy", AuthIndex: "b"}, "k").Snapshot
	if a.State != quotaPartial || len(a.Windows) != 1 || a.Windows[0].Name != "Pro 5h" || a.Windows[0].Remaining != 0 || a.Windows[0].ResetAt.IsZero() || !b.Windows[0].ResetAt.IsZero() {
		t.Fatalf("%+v %+v", a, b)
	}
	for code, want := range map[int]string{501: "quota_provider_unavailable", 404: "quota_endpoint_or_account_unavailable", 429: "management_http_429"} {
		status = code
		if got := fetchQuota(context.Background(), accountCredential{Provider: "agy", AuthIndex: "a"}, "k"); got.Snapshot.Err != want || got.Retryable != (code == 429) {
			t.Fatalf("%d: %+v", code, got)
		}
	}
}

func TestDevinInnerStatusAndHeaders(t *testing.T) {
	status := 401
	var headers map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		headers, _ = req["header"].(map[string]any)
		w.Write([]byte(`{"status_code":` + strconv.Itoa(status) + `,"body":"secret-body"}`))
	}))
	defer server.Close()
	t.Setenv("MODEL_COMBO_MANAGEMENT_URL", server.URL)
	for code, want := range map[int]string{401: "provider_auth_rejected", 403: "provider_access_denied", 429: "provider_rate_limited", 503: "provider_unavailable", 400: "provider_request_failed"} {
		status = code
		got := fetchQuota(context.Background(), accountCredential{Provider: "devin", AuthIndex: "d"}, "k")
		if got.Snapshot.Err != want || got.Retryable != (code == 429 || code >= 500) {
			t.Fatalf("%d: %+v", code, got)
		}
	}
	if _, ok := headers["Authorization"]; ok || headers["Connect-Protocol-Version"] != "1" {
		t.Fatalf("%v", headers)
	}
}
