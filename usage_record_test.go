package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestHandleUsagePreservesCallbackAttribution(t *testing.T) {
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
	plugin.HandleUsage(t.Context(), pluginapi.UsageRecord{Provider: "p", Model: "routed/model", APIKey: "client-secret", RequestedAt: now})
	plugin.HandleUsage(t.Context(), pluginapi.UsageRecord{Provider: "p", Model: "direct/model", RequestedAt: now.Add(time.Second)})

	records, err := store.referenceUsageRecords(usageFilter{})
	if err != nil || len(records) != 2 {
		t.Fatalf("records = %#v, %v", records, err)
	}
	routed, direct := records[0], records[1]
	if routed.Attribution != attributionRouted || routed.RouterModel != "smart" || routed.RouterRequestID != "router-loop" || routed.RouterAttempt != 1 {
		t.Fatalf("routed record = %#v", routed)
	}
	if direct.Attribution == attributionRouted || direct.RequestID != "" || direct.RequestEndpoint != "" {
		t.Fatalf("direct record = %#v", direct)
	}
	raw, err := json.Marshal(direct)
	if err != nil || strings.Contains(string(raw), "request_id") || strings.Contains(string(raw), "request_endpoint") {
		t.Fatalf("empty identity must be omitted from JSON: %s, %v", raw, err)
	}
}
