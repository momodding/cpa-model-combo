package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHeadroomScopesAndStates(t *testing.T) {
	cases := []struct {
		name, model string
		snapshot    quotaSnapshot
		score       float64
		state       quotaState
	}{
		{"unknown", "claude-opus", quotaSnapshot{}, 0, quotaUnknown},
		{"unlimited", "gpt-5", quotaSnapshot{State: quotaUnlimited}, 1, quotaUnlimited},
		{"scoped", "claude-opus", quotaSnapshot{State: quotaKnown, Windows: []quotaWindow{{Remaining: .8}, {Scope: "opus", Remaining: .3}, {Scope: "sonnet", Remaining: 0}}}, .3, quotaKnown},
		{"bucket opus matches", "claude-3-opus", quotaSnapshot{State: quotaKnown, Windows: []quotaWindow{{Scope: "claude-opus", Remaining: .3}}}, .3, quotaKnown},
		{"bucket opus excludes sonnet", "claude-3-sonnet", quotaSnapshot{State: quotaKnown, Windows: []quotaWindow{{Scope: "claude-opus", Remaining: 0}}}, 0, quotaUnknown},
		{"bucket sonnet excludes opus", "claude-3-opus", quotaSnapshot{State: quotaKnown, Windows: []quotaWindow{{Scope: "claude-sonnet", Remaining: 0}}}, 0, quotaUnknown},
		{"generic claude bucket", "claude-3-sonnet", quotaSnapshot{State: quotaKnown, Windows: []quotaWindow{{Scope: "claude-default", Remaining: .6}}}, .6, quotaKnown},
		{"unrelated", "gpt-5", quotaSnapshot{State: quotaKnown, Windows: []quotaWindow{{Scope: "gemini-pro", Remaining: .5}}}, 0, quotaUnknown},
		{"partial", "gemini-2", quotaSnapshot{State: quotaPartial, Windows: []quotaWindow{{Scope: "gemini-pro", Remaining: .4}}}, .4, quotaPartial},
		{"exhausted", "gpt-5", quotaSnapshot{State: quotaKnown, Windows: []quotaWindow{{Remaining: 0}}}, 0, quotaExhausted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			score, state := headroom(tc.snapshot, tc.model)
			if score != tc.score || state != tc.state {
				t.Fatalf("got %v %v", score, state)
			}
		})
	}
}

func TestQuotaParsers(t *testing.T) {
	claude := parseClaudeQuota([]byte(`{"five_hour":{"utilization":20,"resets_at":"2026-10-01T00:00:00Z"},"seven_day_opus":{"utilization":70},"seven_day_sonnet":{"utilization":"bad"}}`))
	if score, state := headroom(claude, "claude-opus"); state != quotaPartial || score < .299 || score > .301 {
		t.Fatalf("claude: %v %v", score, state)
	}
	codex := parseCodexQuota([]byte(`{"rate_limit":{"primary_window":{"used_percent":25,"reset_at":1790812800},"secondary_window":{"used_percent":100}}}`))
	if _, state := headroom(codex, "gpt-5"); state != quotaExhausted {
		t.Fatalf("codex: %v", state)
	}
	if got := parseCodexQuota([]byte(`{"credits":{"unlimited":true}}`)); got.State != quotaUnlimited {
		t.Fatalf("credits: %+v", got)
	}
	bucket := parseBucketQuota([]byte(`{"groups":[{"buckets":[{"id":"gemini-pro","window":"weekly","remainingFraction":0.7,"resetTime":"2026-10-01T00:00:00Z"},{"id":"gpt-pro","remainingFraction":0}]}]}`))
	if score, state := headroom(bucket, "gemini-2"); score != .7 || state != quotaKnown {
		t.Fatalf("bucket: %v %v", score, state)
	}
	if got := parseBucketQuota([]byte(`{"groups":[{"buckets":[{"window":"daily"}]}]}`)); got.State != quotaUnknown {
		t.Fatalf("missing fraction: %+v", got)
	}
}

func TestQuotaManagementSecurityAndAcquisition(t *testing.T) {
	for _, raw := range []string{"http://example.com:8317", "http://localhost.evil", "http://user@localhost", "file://localhost", "http://127.0.0.1/path"} {
		if _, err := managementURL(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	var seen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = true
		if r.URL.Path != "/v0/management/api-call" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["url"] != claudeQuotaURL || body["auth_index"] != "idx" || body["header"].(map[string]any)["Authorization"] != "Bearer $TOKEN$" {
			t.Errorf("unexpected request body: %v", body)
		}
		_, _ = w.Write([]byte(`{"status_code":200,"body":"{\"five_hour\":{\"utilization\":20}}"}`))
	}))
	defer server.Close()
	t.Setenv("MODEL_COMBO_MANAGEMENT_URL", server.URL)
	t.Setenv("MODEL_COMBO_MANAGEMENT_KEY", "secret")
	result := fetchQuota(context.Background(), accountCredential{Provider: "claude", AuthIndex: "idx"})
	if !seen || result.Snapshot.State != quotaKnown || strings.Contains(result.Snapshot.Err, "secret") {
		t.Fatalf("result: %+v", result)
	}
	t.Setenv("MODEL_COMBO_MANAGEMENT_KEY", "")
	if result := fetchQuota(context.Background(), accountCredential{Provider: "claude"}); result.Snapshot.Err != "management_key_missing" {
		t.Fatalf("missing key: %+v", result)
	}
}

func TestQuotaProviderMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL    string            `json:"url"`
			Method string            `json:"method"`
			Data   string            `json:"data"`
			Header map[string]string `json:"header"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		switch body.URL {
		case codexQuotaURL:
			if body.Header["Chatgpt-Account-Id"] != "account-id" {
				t.Error("missing account ID")
			}
			_, _ = w.Write([]byte(`{"status_code":200,"body":"{\"rate_limit\":{\"primary_window\":{\"used_percent\":20}}}"}`))
		case antigravityQuotaURLs[0]:
			if body.Method != "POST" || body.Data != `{"project":"project-id"}` {
				t.Errorf("unexpected project request: %+v", body)
			}
			_, _ = w.Write([]byte(`{"status_code":200,"body":"{\"groups\":[{\"buckets\":[{\"id\":\"gemini-pro\",\"remainingFraction\":0.8}]}]}"}`))
		default:
			t.Errorf("unexpected upstream %q", body.URL)
		}
	}))
	defer server.Close()
	t.Setenv("MODEL_COMBO_MANAGEMENT_URL", server.URL)
	t.Setenv("MODEL_COMBO_MANAGEMENT_KEY", "secret")
	for _, credential := range []accountCredential{{Provider: "codex", AuthIndex: "i", Account: "account-id"}, {Provider: "antigravity", AuthIndex: "i", ProjectID: "project-id"}} {
		if got := fetchQuota(context.Background(), credential); got.Snapshot.State != quotaKnown {
			t.Fatalf("%s: %+v", credential.Provider, got)
		}
	}
}
