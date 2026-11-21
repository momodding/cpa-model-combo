package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDecodeRouterConfigCanonicalAndLegacyKeys(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "canonical",
			raw: `routes:
  - alias: smart
    strategy: round-robin
    cooldown_seconds: 15
    targets:
      - model: provider-a
        weight: 3
      - model: provider-b
`,
		},
		{
			name: "legacy",
			raw: `model-routes:
  - alias: smart
    strategy: round-robin
    cooldown-seconds: 15
    models: [provider-a, provider-b]
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := decodeRouterConfig([]byte(test.raw))
			if err != nil {
				t.Fatalf("decodeRouterConfig() error = %v", err)
			}
			if !config.Enabled || len(config.Routes) != 1 {
				t.Fatalf("decodeRouterConfig() = %#v", config)
			}
			route := config.Routes[0]
			if route.Alias != "smart" || route.Strategy != routeStrategyRoundRobin || route.CooldownSeconds != 15 || len(route.Targets) != 2 {
				t.Fatalf("route = %#v", route)
			}
			wantTargets := []modelTarget{{Model: "provider-a", Weight: 1}, {Model: "provider-b", Weight: 1}}
			if test.name == "canonical" {
				wantTargets[0].Weight = 3
			}
			if !slices.EqualFunc(route.Targets, wantTargets, func(a, b modelTarget) bool {
				return a.Model == b.Model && a.Weight == b.Weight && slices.Equal(a.AllowedAccounts, b.AllowedAccounts)
			}) {
				t.Fatalf("targets = %#v", route.Targets)
			}
		})
	}
}

func TestDecodeRouterConfigDefaults(t *testing.T) {
	previousResolver := defaultDataPathResolver
	wantPath := filepath.Join(t.TempDir(), "default.db")
	defaultDataPathResolver = func() string { return wantPath }
	t.Cleanup(func() { defaultDataPathResolver = previousResolver })
	config, err := decodeRouterConfig([]byte(`routes:
  - alias: smart
    models: [provider-a]
`))
	if err != nil {
		t.Fatalf("decodeRouterConfig() error = %v", err)
	}
	route := config.Routes[0]
	if route.Strategy != routeStrategyPriority || route.CooldownSeconds != defaultCooldownSeconds {
		t.Fatalf("route defaults = %#v", route)
	}
	if config.DataPath != wantPath || config.RetentionDays != defaultRetentionDays {
		t.Fatalf("storage defaults = %#v", config)
	}
}

func TestDecodeRouterConfigStorageOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "usage.db")
	config, err := decodeRouterConfig([]byte("data_path: " + filepath.ToSlash(path) + "\nretention_days: 45\nroutes: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if config.DataPath != path || config.RetentionDays != 45 {
		t.Fatalf("storage config = %#v", config)
	}
	for _, days := range []int{0, maxRetentionDays + 1} {
		if _, err := decodeRouterConfig([]byte("retention_days: " + itoa(days) + "\nroutes: []\n")); err == nil || !strings.Contains(err.Error(), "retention_days") {
			t.Fatalf("retention_days=%d error = %v", days, err)
		}
	}
}

func TestDecodeRouterConfigDisabledClearsRoutes(t *testing.T) {
	config, err := decodeRouterConfig([]byte(`enabled: false
routes:
  - alias: smart
    models: [provider-a]
`))
	if err != nil {
		t.Fatalf("decodeRouterConfig() error = %v", err)
	}
	if config.Enabled || config.Routes != nil {
		t.Fatalf("disabled config = %#v", config)
	}
}

func TestDecodeRouterConfigRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		message string
	}{
		{
			name: "both route keys",
			raw: `routes: []
model-routes: []
`,
			message: "both routes and model-routes",
		},
		{
			name: "both cooldown keys",
			raw: `routes:
  - alias: smart
    cooldown_seconds: 10
    cooldown-seconds: 20
    models: [provider-a]
`,
			message: "both cooldown_seconds and cooldown-seconds",
		},
		{
			name: "unknown field",
			raw: `routes: []
surprise: true
`,
			message: "field surprise not found",
		},
		{
			name: "legacy account pools",
			raw: `account_pools: []
`,
			message: unsupportedAccountAwareSchemaError,
		},
		{
			name: "legacy combos",
			raw: `combos: []
`,
			message: unsupportedAccountAwareSchemaError,
		},
		{
			name: "duplicate alias",
			raw: `routes:
  - alias: Smart
    models: [provider-a]
  - alias: smart
    models: [provider-b]
`,
			message: "duplicate alias",
		},
		{
			name: "alias suffix",
			raw: `routes:
  - alias: smart(high)
    models: [provider-a]
`,
			message: "alias must not include a thinking suffix",
		},
		{
			name: "nested alias",
			raw: `routes:
  - alias: smart
    models: [backup]
  - alias: backup
    models: [provider-b]
`,
			message: "target must not reference route alias",
		},
		{
			name: "negative cooldown",
			raw: `routes:
  - alias: smart
    cooldown_seconds: -1
    models: [provider-a]
`,
			message: "cooldown_seconds must be >= 0",
		},
		{
			name: "both model schemas",
			raw: `routes:
  - alias: smart
    models: [provider-a]
    targets:
      - model: provider-b
`,
			message: "both models and targets",
		},
		{
			name: "zero weight",
			raw: `routes:
  - alias: smart
    targets:
      - model: provider-a
        weight: 0
`,
			message: "weight must be between",
		},
		{
			name: "weight too large",
			raw: `routes:
  - alias: smart
    targets:
      - model: provider-a
        weight: 1000001
`,
			message: "weight must be between",
		},
		{
			name: "empty canonical model",
			raw: `routes:
  - alias: smart
    targets:
      - model: ""
`,
			message: "model is required",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeRouterConfig([]byte(test.raw))
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("decodeRouterConfig() error = %v, want substring %q", err, test.message)
			}
		})
	}
}

func TestRouterConfigFallbackPolicyDefaultsWhenEmpty(t *testing.T) {
	policy := routerConfig{}.fallbackPolicy()
	for _, status := range defaultFallbackOnStatus {
		if !policy.shouldFallback(status) {
			t.Fatalf("default policy must fall back on %d", status)
		}
	}
}

func TestDecodeRouterConfigFallback(t *testing.T) {
	raw := []byte(`
fallback:
  fallback_on_status: [400, 429]
  no_fallback_on_status: []
routes:
  - alias: demo
    targets:
      - model: target-a
`)
	cfg, err := decodeRouterConfig(raw)
	if err != nil {
		t.Fatalf("decodeRouterConfig() error = %v", err)
	}
	policy := cfg.fallbackPolicy()
	if !policy.shouldFallback(400) || !policy.shouldFallback(429) {
		t.Fatal("configured statuses must fall back")
	}
	if policy.shouldFallback(404) {
		t.Fatal("404 is not in the configured list and must not fall back")
	}
	if !policy.shouldFallback(503) {
		t.Fatal("503 must still fall back through the 5xx rule")
	}
}

func TestDecodeRouterConfigRejectsInvalidFallback(t *testing.T) {
	cases := map[string]string{
		"overlap": `
fallback:
  fallback_on_status: [429]
  no_fallback_on_status: [429]
routes:
  - alias: demo
    targets:
      - model: target-a
`,
		"out of range": `
fallback:
  fallback_on_status: [99]
routes:
  - alias: demo
    targets:
      - model: target-a
`,
		"stream splice": `
	fallback:
	  stream_fallback_before_payload: false
	routes:
	  - alias: demo
	    targets:
	      - model: target-a
`,
		"unknown key": `
fallback:
  fallback_on_500: true
routes:
  - alias: demo
    targets:
      - model: target-a
`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeRouterConfig([]byte(raw)); err == nil {
				t.Fatalf("decodeRouterConfig() accepted invalid fallback config for %q", name)
			}
		})
	}
}

func TestDecodeRouterConfigExclusionOverridesDefaults(t *testing.T) {
	raw := []byte(`
fallback:
  no_fallback_on_status: [400, 429, 503]
routes:
  - alias: demo
    targets:
      - model: target-a
`)
	cfg, err := decodeRouterConfig(raw)
	if err != nil {
		t.Fatalf("decodeRouterConfig() error = %v", err)
	}
	policy := cfg.fallbackPolicy()
	for _, status := range []int{400, 429, 503} {
		if policy.shouldFallback(status) {
			t.Fatalf("excluded status %d must not fall back", status)
		}
	}
	for _, status := range []int{402, 404, 504} {
		if !policy.shouldFallback(status) {
			t.Fatalf("status %d is not excluded and must keep the default", status)
		}
	}
}

func TestDecodeRouterConfigFallbackListFormsAgree(t *testing.T) {
	routes := `
routes:
  - alias: demo
    targets:
      - model: target-a
`
	cases := map[string]string{
		"omitted":        "fallback:\n  no_fallback_on_status: [400, 503]\n" + routes,
		"empty explicit": "fallback:\n  fallback_on_status: []\n  no_fallback_on_status: [400, 503]\n" + routes,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := decodeRouterConfig([]byte(raw))
			if err != nil {
				t.Fatalf("decodeRouterConfig() error = %v", err)
			}
			policy := cfg.fallbackPolicy()
			if policy.shouldFallback(400) || policy.shouldFallback(503) {
				t.Fatal("an exclusion must remove a status from the inherited default set")
			}
			if !policy.shouldFallback(404) || !policy.shouldFallback(500) {
				t.Fatal("an omitted or empty fallback_on_status must keep the default set")
			}
		})
	}
}

func TestDecodeRouterConfigAcceptsRandomAndLastKnownGoodStrategies(t *testing.T) {
	for _, strategy := range []string{routeStrategyRandom, routeStrategyLastKnownGood} {
		t.Run(strategy, func(t *testing.T) {
			raw := "routes:\n  - alias: smart\n    strategy: " + strategy + "\n    models: [provider-a, provider-b]\n"
			cfg, err := decodeRouterConfig([]byte(raw))
			if err != nil {
				t.Fatalf("decodeRouterConfig() error = %v", err)
			}
			if got := cfg.Routes[0].Strategy; got != strategy {
				t.Fatalf("strategy = %q, want %q", got, strategy)
			}
		})
	}
}

func TestDecodeRouterConfigRejectsUnknownStrategy(t *testing.T) {
	raw := "routes:\n  - alias: smart\n    strategy: lottery\n    models: [provider-a]\n"
	if _, err := decodeRouterConfig([]byte(raw)); err == nil || !strings.Contains(err.Error(), "random") || !strings.Contains(err.Error(), "last_known_good") {
		t.Fatalf("decodeRouterConfig() error = %v, want strategy list including random and last_known_good", err)
	}
}

func TestDecodeRouterConfigRootLastKnownGoodPolicy(t *testing.T) {
	cfg, err := decodeRouterConfig([]byte(`routes:
  - alias: smart
    strategy: last_known_good
    success_rate_threshold: 0.90
    minimum_samples: 20
    observation_window_minutes: 60
    models: [provider-a]
`))
	if err != nil {
		t.Fatal(err)
	}
	policy := cfg.Routes[0].LastKnownGood
	if policy.SuccessRateThreshold != 0.90 || policy.MinimumSamples != 20 || policy.ObservationWindowMinutes != 60 {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestDecodeRouterConfigLastKnownGoodDefaults(t *testing.T) {
	cfg, err := decodeRouterConfig([]byte("routes:\n  - alias: smart\n    strategy: last_known_good\n    models: [provider-a]\n"))
	if err != nil {
		t.Fatal(err)
	}
	policy := cfg.Routes[0].LastKnownGood
	if policy.SuccessRateThreshold != 0.90 || policy.MinimumSamples != 20 || policy.ObservationWindowMinutes != 60 {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestDecodeRouterConfigRejectsInvalidLastKnownGoodPolicy(t *testing.T) {
	for _, raw := range []string{"success_rate_threshold: 0", "success_rate_threshold: -0.1", "minimum_samples: 0", "observation_window_minutes: 0"} {
		_, err := decodeRouterConfig([]byte("routes:\n  - alias: smart\n    strategy: last_known_good\n    " + raw + "\n    models: [provider-a]\n"))
		if err == nil {
			t.Fatalf("expected error for %s", raw)
		}
	}
}

func TestDecodeRouterConfigRejectsNestedLastKnownGoodPolicy(t *testing.T) {
	raw := `routes:
  - alias: smart
    strategy: last_known_good
    last_known_good:
      success_rate_threshold: 0.9
    models: [provider-a]
`
	if _, err := decodeRouterConfig([]byte(raw)); err == nil {
		t.Fatal("decodeRouterConfig() accepted nested last_known_good policy")
	}
}

func TestRouterConfigAttemptLimit(t *testing.T) {
	tests := []struct {
		name string
		cfg  routerConfig
		want int
	}{
		{name: "default uses all targets", cfg: routerConfig{}, want: 3},
		{name: "disabled caps to one", cfg: routerConfig{FallbackDisabled: true}, want: 1},
		{name: "max below targets", cfg: routerConfig{MaxAttempts: 2}, want: 2},
		{name: "max above targets", cfg: routerConfig{MaxAttempts: 5}, want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.attemptLimit(3); got != tt.want {
				t.Fatalf("attemptLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDecodeRouterConfigFallbackDisabledAndAttempts(t *testing.T) {
	cfg, err := decodeRouterConfig([]byte(`
fallback:
  enabled: false
  max_attempts: 2
routes:
  - alias: demo
    models: [target-a, target-b, target-c]
`))
	if err != nil {
		t.Fatalf("decodeRouterConfig() error = %v", err)
	}
	if !cfg.FallbackDisabled || cfg.MaxAttempts != 2 {
		t.Fatalf("fallback fields = disabled %v, max %d", cfg.FallbackDisabled, cfg.MaxAttempts)
	}
	if got := cfg.attemptLimit(3); got != 1 {
		t.Fatalf("disabled attemptLimit() = %d, want 1", got)
	}
}

func TestFallbackPolicyNormalizesErrorCodes(t *testing.T) {
	cfg, err := decodeRouterConfig([]byte(`
fallback:
  retry_codes: ["  Model_Overloaded "]
  no_retry_codes: [Blocked_Model]
routes:
  - alias: demo
    models: [target-a]
`))
	if err != nil {
		t.Fatalf("decodeRouterConfig() error = %v", err)
	}
	policy := cfg.fallbackPolicy()
	if !policy.retryableCode("MODEL_overloaded") {
		t.Fatal("retry code must match case-insensitively")
	}
	if policy.retryableCode("blocked_model") {
		t.Fatal("no_retry_codes must take precedence and match case-insensitively")
	}
}

func TestAllowedAccountsConfiguration(t *testing.T) {
	raw := `routes:
  - alias: smart
    allowed_accounts: [' b ', A, a]
    targets:
      - model: inherited
      - model: unrestricted
        allowed_accounts: []
      - model: overridden
        allowed_accounts: [' c ', d]
  - alias: legacy
    models: [old-a, old-b]
    allowed_accounts: [legacy-account]
`
	config, err := decodeRouterConfig([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	route := config.Routes[0]
	for index, want := range [][]string{{"b", "A", "a"}, {}, {"c", "d"}} {
		if !slices.Equal(route.Targets[index].AllowedAccounts, want) {
			t.Fatalf("target %d accounts = %#v, want %#v", index, route.Targets[index].AllowedAccounts, want)
		}
	}
	for _, target := range config.Routes[1].Targets {
		if !slices.Equal(target.AllowedAccounts, []string{"legacy-account"}) {
			t.Fatalf("legacy target accounts = %#v", target.AllowedAccounts)
		}
	}
	config.Routes[1].Targets[0].AllowedAccounts[0] = "changed"
	if config.Routes[1].Targets[1].AllowedAccounts[0] != "legacy-account" {
		t.Fatal("legacy targets share inherited account slices")
	}
	signature := routeSignature(route)
	route.Targets[0].AllowedAccounts = []string{"A", "b", "a"}
	if routeSignature(route) == signature {
		t.Fatal("account order must change route signature")
	}
	for _, list := range []string{"[' ']", "[a, ' a ']", "[123]", "[true]", "[null]", "not-an-array", "null"} {
		for _, input := range []string{
			"routes:\n  - alias: smart\n    models: [model-a]\n    allowed_accounts: " + list + "\n",
			"routes:\n  - alias: smart\n    targets:\n      - model: model-a\n        allowed_accounts: " + list + "\n",
		} {
			_, err := decodeRouterConfig([]byte(input))
			if err == nil || !strings.Contains(err.Error(), "allowed_accounts") {
				t.Fatalf("%s: expected allowed_accounts error, got %v", list, err)
			}
		}
	}
}

func TestHeadroomConfig(t *testing.T) {
	for _, tc := range []struct {
		name, strategy, policy, want string
		invalid                      bool
	}{
		{name: "default", strategy: "headroom", want: "priority"},
		{name: "priority", strategy: "headroom", policy: "priority", want: "priority"},
		{name: "exclude", strategy: "headroom", policy: "exclude", want: "exclude"},
		{name: "last", strategy: "headroom", policy: "last", want: "last"},
		{name: "invalid", strategy: "headroom", policy: "invalid", invalid: true},
		{name: "empty", strategy: "headroom", policy: "''", invalid: true},
		{name: "other strategy", strategy: "priority", policy: "priority", invalid: true},
		{name: "legacy route", strategy: "priority"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := "routes:\n  - alias: smart\n    strategy: " + tc.strategy + "\n    models: [provider-a]\n"
			if tc.policy != "" {
				raw += "    unknown_quota: " + tc.policy + "\n"
			}
			cfg, err := decodeRouterConfig([]byte(raw))
			if tc.invalid {
				if err == nil || !strings.Contains(err.Error(), `routes[0] "smart": invalid unknown_quota policy`) {
					t.Fatalf("expected invalid policy error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Routes[0].UnknownQuota; got != tc.want {
				t.Fatalf("unknown_quota = %q, want %q", got, tc.want)
			}
		})
	}
	cfg, err := decodeRouterConfig([]byte("routes:\n  - alias: smart\n    strategy: headroom\n    models: [provider-a]\n"))
	if err != nil {
		t.Fatal(err)
	}
	route := cfg.Routes[0]
	signature := routeSignature(route)
	route.UnknownQuota = "exclude"
	if routeSignature(route) == signature {
		t.Fatal("unknown_quota must change route signature")
	}
}

func TestQuotaPollingConfig(t *testing.T) {
	want := quotaPollingConfig{RefreshSeconds: 60, IdleSeconds: 600, TTLSeconds: 60, MaxStaleSeconds: 300, FetchTimeoutSeconds: 5, MaxConcurrent: 3, ColdWaitMS: 1500, DefaultUnknownQuotaPercent: 50}
	cfg, err := decodeRouterConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuotaPolling != want {
		t.Fatalf("default quota_polling = %#v, want %#v", cfg.QuotaPolling, want)
	}
	cfg, err = decodeRouterConfig([]byte("quota_polling:\n  refresh_seconds: 12\n  idle_seconds: 24\n  ttl_seconds: 8\n  max_stale_seconds: 8\n  fetch_timeout_seconds: 2\n  max_concurrent: 4\n  cold_wait_ms: 20\n"))
	if err != nil {
		t.Fatal(err)
	}
	want = quotaPollingConfig{RefreshSeconds: 12, IdleSeconds: 24, TTLSeconds: 8, MaxStaleSeconds: 8, FetchTimeoutSeconds: 2, MaxConcurrent: 4, ColdWaitMS: 20, DefaultUnknownQuotaPercent: 50}
	if cfg.QuotaPolling != want {
		t.Fatalf("quota_polling = %#v, want %#v", cfg.QuotaPolling, want)
	}
	cfg, err = decodeRouterConfig([]byte("quota_polling:\n  max_concurrent: 7\n"))
	if err != nil || cfg.QuotaPolling.MaxConcurrent != 7 || cfg.QuotaPolling.RefreshSeconds != 60 {
		t.Fatalf("partial quota_polling = %#v, error = %v", cfg.QuotaPolling, err)
	}
	for _, field := range []string{"refresh_seconds", "idle_seconds", "ttl_seconds", "max_stale_seconds", "fetch_timeout_seconds", "max_concurrent", "cold_wait_ms"} {
		for _, value := range []string{"0", "-1"} {
			_, err := decodeRouterConfig([]byte("quota_polling:\n  " + field + ": " + value + "\n"))
			if err == nil || !strings.Contains(err.Error(), "quota_polling") {
				t.Fatalf("%s: %s: expected quota_polling error, got %v", field, value, err)
			}
		}
	}
	for _, value := range []string{"-1", "100.5", ".nan"} {
		if _, err := decodeRouterConfig([]byte("quota_polling:\n  default_unknown_quota_percent: " + value + "\n")); err == nil {
			t.Fatalf("accepted default_unknown_quota_percent %s", value)
		}
	}
	if cfg, err := decodeRouterConfig([]byte("quota_polling:\n  default_unknown_quota_percent: 0\n")); err != nil || cfg.QuotaPolling.DefaultUnknownQuotaPercent != 0 {
		t.Fatalf("explicit zero = %v, %v", cfg.QuotaPolling.DefaultUnknownQuotaPercent, err)
	}
	for _, raw := range []string{"quota_polling:\n  ttl_seconds: 301\n", "quota_polling:\n  unrecognized: 1\n"} {
		if _, err := decodeRouterConfig([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid config %q", raw)
		}
	}
}
