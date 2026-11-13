package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestHandleUsageWithIdentityStoresRequestIdentityIndependentOfAttribution(t *testing.T) {
	store, err := openUsageStore(filepath.Join(t.TempDir(), "usage.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	plugin := &modelRouterPlugin{
		config:      routerConfig{Enabled: true},
		store:       store,
		attribution: newAttributionTracker(func() time.Time { return now }),
	}
	plugin.attribution.MarkRouted("smart", "routed/model", mapHeader("Authorization", "Bearer client-secret"), "router-loop", 1)
	// schema-7 host: identity populated from CPA callback
	plugin.handleUsageWithIdentity(t.Context(), pluginapi.UsageRecord{Provider: "p", Model: "routed/model", APIKey: "client-secret", RequestedAt: now}, "1234", "POST /v1/chat/completions")
	// schema-7 unattributed call also carries identity
	plugin.handleUsageWithIdentity(t.Context(), pluginapi.UsageRecord{Provider: "p", Model: "direct/model", RequestedAt: now.Add(time.Second)}, "1234", "POST /v1/chat/completions")
	// legacy host (schema < 7): no identity
	plugin.handleUsageWithIdentity(t.Context(), pluginapi.UsageRecord{Provider: "p", Model: "legacy/model", RequestedAt: now.Add(2 * time.Second)}, "", "")

	records, err := store.referenceUsageRecords(usageFilter{})
	if err != nil || len(records) != 3 {
		t.Fatalf("records = %#v, %v", records, err)
	}
	routed, direct, legacy := records[0], records[1], records[2]
	if routed.Attribution != attributionRouted || routed.RouterModel != "smart" || routed.RouterRequestID != "router-loop" || routed.RouterAttempt != 1 ||
		routed.RequestID != "1234" || routed.RequestEndpoint != "POST /v1/chat/completions" {
		t.Fatalf("routed record = %#v", routed)
	}
	if direct.Attribution == attributionRouted || direct.RequestID != "1234" || direct.RequestEndpoint != "POST /v1/chat/completions" {
		t.Fatalf("unattributed+identity record = %#v", direct)
	}
	if legacy.RequestID != "" || legacy.RequestEndpoint != "" {
		t.Fatalf("legacy record should have empty identity: %#v", legacy)
	}
	// empty identity fields must be omitted from JSON payload
	raw, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(raw), "request_id") || strings.Contains(string(raw), "request_endpoint") {
		t.Fatalf("empty identity must be omitted from JSON: %s, %v", raw, err)
	}
	// non-empty identity fields must appear in JSON payload
	raw, err = json.Marshal(direct)
	if err != nil || !strings.Contains(string(raw), `"request_id":"1234"`) || !strings.Contains(string(raw), `"request_endpoint":"POST /v1/chat/completions"`) {
		t.Fatalf("identity wire fields missing from JSON: %s, %v", raw, err)
	}
}
