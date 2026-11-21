package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// These endpoints and headers are from Management Center e5eb4fe.
const claudeQuotaURL = "https://api.anthropic.com/api/oauth/usage"
const codexQuotaURL = "https://chatgpt.com/backend-api/wham/usage"

var antigravityQuotaURLs = [...]string{
	"https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:retrieveUserQuotaSummary",
	"https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
}

type quotaFetchResult struct {
	Snapshot  quotaSnapshot
	Retryable bool
}

func quotaFailure(source, reason string, retryable bool) quotaFetchResult {
	return quotaFetchResult{Snapshot: quotaSnapshot{State: quotaUnknown, Source: source, FetchedAt: time.Now(), Err: reason}, Retryable: retryable}
}

func managementURL(raw string) (*url.URL, error) {
	if raw == "" {
		raw = "http://127.0.0.1:8317"
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid management url")
	}
	host := strings.ToLower(u.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return nil, fmt.Errorf("invalid management url")
	}
	return u, nil
}

var quotaHTTPClient = &http.Client{
	Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(host, "localhost") {
			host = "127.0.0.1"
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("invalid management address")
		}
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(host, port))
	}},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func fetchQuota(ctx context.Context, credential accountCredential) quotaFetchResult {
	provider := strings.ToLower(strings.TrimSpace(credential.Provider))
	source := provider
	key := os.Getenv("MODEL_COMBO_MANAGEMENT_KEY")
	if strings.TrimSpace(key) == "" {
		return quotaFailure(source, "management_key_missing", false)
	}
	base, err := managementURL(os.Getenv("MODEL_COMBO_MANAGEMENT_URL"))
	if err != nil {
		return quotaFailure(source, "management_url_invalid", false)
	}
	if credential.AuthIndex == "" {
		return quotaFailure(source, "missing_auth_index", false)
	}
	var endpoints []string
	method, data := "GET", ""
	headers := map[string]string{"Authorization": "Bearer $TOKEN$", "Content-Type": "application/json"}
	switch provider {
	case "claude":
		endpoints = []string{claudeQuotaURL}
		headers["User-Agent"] = "claude-cli/2.1.280 (external, cli)"
		headers["anthropic-beta"] = "oauth-2025-04-20"
	case "codex":
		endpoints = []string{codexQuotaURL}
		headers["User-Agent"] = "codex-tui/0.149.1 (Mac OS 26.5.2; arm64) iTerm.app/3.6.11 (codex-tui; 0.149.1)"
		if credential.Account != "" {
			headers["Chatgpt-Account-Id"] = credential.Account
		}
	case "antigravity":
		if credential.ProjectID == "" {
			return quotaFailure(source, "missing_project_id", false)
		}
		endpoints = antigravityQuotaURLs[:]
		method = "POST"
		project, _ := json.Marshal(map[string]string{"project": credential.ProjectID})
		data = string(project)
		headers["User-Agent"] = "antigravity/hub/2.23.0 darwin/arm64"
	default:
		// The typed auth catalog does not expose plugin quota capabilities or IDs.
		return quotaFailure(source, "unsupported_provider", false)
	}
	base.Path = "/v0/management/api-call"
	var result quotaFetchResult
	for _, upstream := range endpoints {
		payload, _ := json.Marshal(map[string]any{"auth_index": credential.AuthIndex, "method": method, "url": upstream, "header": headers, "data": data})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(payload))
		if err != nil {
			return quotaFailure(source, "management_request_invalid", false)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		response, err := quotaHTTPClient.Do(req)
		if err != nil {
			result = quotaFailure(source, "management_request_failed", true)
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			result = quotaFailure(source, fmt.Sprintf("management_http_%d", response.StatusCode), response.StatusCode == 429 || response.StatusCode >= 500)
			continue
		}
		var wrapped struct {
			StatusCode int             `json:"status_code"`
			Body       json.RawMessage `json:"body"`
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&wrapped)
		response.Body.Close()
		if err != nil {
			result = quotaFailure(source, "parse_failure", false)
			continue
		}
		if wrapped.StatusCode < 200 || wrapped.StatusCode >= 300 {
			result = quotaFailure(source, fmt.Sprintf("upstream_http_%d", wrapped.StatusCode), wrapped.StatusCode == 429 || wrapped.StatusCode >= 500)
			continue
		}
		body := []byte(wrapped.Body)
		if len(body) > 0 && body[0] == '"' {
			var text string
			if json.Unmarshal(body, &text) != nil {
				result = quotaFailure(source, "parse_failure", false)
				continue
			}
			body = []byte(text)
		}
		var snapshot quotaSnapshot
		switch provider {
		case "claude":
			snapshot = parseClaudeQuota(body)
		case "codex":
			snapshot = parseCodexQuota(body)
		case "antigravity":
			snapshot = parseBucketQuota(body)
		}
		snapshot.Source, snapshot.FetchedAt = source, time.Now()
		result = quotaFetchResult{Snapshot: snapshot}
		if snapshot.State != quotaUnknown {
			return result
		}
	}
	return result
}

func quotaNumber(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}

func quotaReset(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, true
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		t, err := time.Parse(time.RFC3339, text)
		return t, err == nil
	}
	var seconds json.Number
	if json.Unmarshal(raw, &seconds) == nil {
		n, err := strconv.ParseInt(string(seconds), 10, 64)
		if err == nil {
			return time.Unix(n, 0), true
		}
	}
	return time.Time{}, false
}

func parseUsageWindows(data []byte, names map[string]string, percentField, resetField string) quotaSnapshot {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return quotaSnapshot{State: quotaUnknown, Err: "parse_failure"}
	}
	snapshot := quotaSnapshot{State: quotaKnown}
	bad := false
	for name, scope := range names {
		raw, present := fields[name]
		if !present || string(raw) == "null" {
			continue
		}
		var window map[string]json.RawMessage
		if json.Unmarshal(raw, &window) != nil || window == nil {
			bad = true
			continue
		}
		used, ok := quotaNumber(window[percentField])
		reset, resetOK := quotaReset(window[resetField])
		if !ok || used < 0 || used > 100 || !resetOK {
			bad = true
			continue
		}
		snapshot.Windows = append(snapshot.Windows, quotaWindow{Name: name, Scope: scope, Remaining: 1 - used/100, ResetAt: reset})
	}
	if len(snapshot.Windows) == 0 {
		snapshot.State = quotaUnknown
		snapshot.Err = "parse_failure"
	} else if bad {
		snapshot.State = quotaPartial
	}
	return snapshot
}

func parseClaudeQuota(data []byte) quotaSnapshot {
	return parseUsageWindows(data, map[string]string{"five_hour": "", "seven_day": "", "seven_day_opus": "opus", "seven_day_sonnet": "sonnet"}, "utilization", "resets_at")
}

func parseCodexQuota(data []byte) quotaSnapshot {
	var payload struct {
		RateLimit json.RawMessage `json:"rate_limit"`
		Credits   struct {
			Unlimited  bool `json:"unlimited"`
			HasCredits bool `json:"has_credits"`
		} `json:"credits"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return quotaSnapshot{State: quotaUnknown, Err: "parse_failure"}
	}
	snapshot := parseUsageWindows(payload.RateLimit, map[string]string{"primary_window": "", "secondary_window": ""}, "used_percent", "reset_at")
	if snapshot.State == quotaUnknown && (payload.Credits.Unlimited || payload.Credits.HasCredits) {
		return quotaSnapshot{State: quotaUnlimited}
	}
	return snapshot
}

func parseBucketQuota(data []byte) quotaSnapshot {
	var payload struct {
		Groups []struct {
			Buckets []struct {
				ID        string          `json:"id"`
				Window    string          `json:"window"`
				Remaining json.RawMessage `json:"remainingFraction"`
				Reset     json.RawMessage `json:"resetTime"`
			} `json:"buckets"`
		} `json:"groups"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Groups == nil {
		return quotaSnapshot{State: quotaUnknown, Err: "parse_failure"}
	}
	snapshot := quotaSnapshot{State: quotaKnown}
	bad := false
	for _, group := range payload.Groups {
		for _, bucket := range group.Buckets {
			remaining, ok := quotaNumber(bucket.Remaining)
			reset, resetOK := quotaReset(bucket.Reset)
			if !ok || remaining < 0 || remaining > 1 || !resetOK {
				bad = true
				continue
			}
			name := bucket.Window
			if name == "" {
				name = bucket.ID
			}
			snapshot.Windows = append(snapshot.Windows, quotaWindow{Name: name, Scope: bucket.ID, Remaining: remaining, ResetAt: reset})
		}
	}
	if len(snapshot.Windows) == 0 {
		snapshot.State = quotaUnknown
		snapshot.Err = "parse_failure"
	} else if bad {
		snapshot.State = quotaPartial
	}
	return snapshot
}
