package main

import (
	"database/sql"
	"encoding/json"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPluginMetadataPublicIdentity(t *testing.T) {
	resetModelRouterABIState(t)
	request, err := json.Marshal(lifecycleRequest{ConfigYAML: []byte("routes: []\n")})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := handleModelRouterABIMethod(t.Context(), pluginabi.MethodPluginRegister, request)
	if err != nil {
		t.Fatalf("plugin.register error = %v", err)
	}
	var envelope abiEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode registration envelope: %v", err)
	}
	var registration abiRegistration
	if err := json.Unmarshal(envelope.Result, &registration); err != nil {
		t.Fatalf("decode registration result: %v", err)
	}
	if registration.Metadata.Name != "Model Combo" || registration.Metadata.GitHubRepository != "https://github.com/momodding/cpa-model-combo" {
		t.Fatalf("public metadata = %+v", registration.Metadata)
	}
}

func TestABIRegistrationRoutingAndCountTokens(t *testing.T) {
	resetModelRouterABIState(t)
	config := []byte(`routes:
  - alias: smart
    models: [provider-a]
`)
	request, err := json.Marshal(lifecycleRequest{ConfigYAML: config})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := handleModelRouterABIMethod(t.Context(), pluginabi.MethodPluginRegister, request)
	if err != nil {
		t.Fatalf("plugin.register error = %v", err)
	}
	var registrationEnvelope abiEnvelope
	if err := json.Unmarshal(raw, &registrationEnvelope); err != nil {
		t.Fatalf("decode registration envelope: %v", err)
	}
	var registration abiRegistration
	if err := json.Unmarshal(registrationEnvelope.Result, &registration); err != nil {
		t.Fatalf("decode registration result: %v", err)
	}
	if !registrationEnvelope.OK || registration.SchemaVersion != registrationSchemaVersion || registration.Metadata.Name != pluginName || registration.Metadata.Version != "0.1.0" || !registration.Capabilities.ModelRegistrar || !registration.Capabilities.ModelRouter || !registration.Capabilities.Executor || !registration.Capabilities.RequestInterceptor || registration.Capabilities.RequestLifecycle || registration.Capabilities.ResponseInterceptor || registration.Capabilities.StreamInterceptor || !registration.Capabilities.UsagePlugin || !registration.Capabilities.ManagementAPI {
		t.Fatalf("registration = %#v", registration)
	}

	routeRequest, err := json.Marshal(modelRouteRPCRequest{ModelRouteRequest: pluginapi.ModelRouteRequest{RequestedModel: "smart(high)"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = handleModelRouterABIMethod(t.Context(), pluginabi.MethodModelRoute, routeRequest)
	if err != nil {
		t.Fatalf("model.route error = %v", err)
	}
	var routeEnvelope abiEnvelope
	if err := json.Unmarshal(raw, &routeEnvelope); err != nil {
		t.Fatalf("decode route envelope: %v", err)
	}
	var route pluginapi.ModelRouteResponse
	if err := json.Unmarshal(routeEnvelope.Result, &route); err != nil {
		t.Fatalf("decode route result: %v", err)
	}
	if !route.Handled || route.TargetKind != pluginapi.ModelRouteTargetSelf {
		t.Fatalf("route = %#v", route)
	}

	usageRequest, err := json.Marshal(pluginapi.UsageRecord{
		Provider: "openai", Model: "provider-a", RequestedAt: time.Now().UTC(), Detail: pluginapi.UsageDetail{InputTokens: 10, OutputTokens: 1, CacheCreationTokens: 2, TotalTokens: 11},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handleModelRouterABIMethod(t.Context(), pluginabi.MethodUsageHandle, usageRequest); err != nil {
		t.Fatalf("usage.handle error = %v", err)
	}
	modelRouterABIState.Lock()
	activePlugin := modelRouterABIState.plugin
	modelRouterABIState.Unlock()
	page, err := activePlugin.store.Requests(usageFilter{From: time.Now().UTC().Add(-time.Hour), To: time.Now().UTC().Add(time.Hour)}, "time", "desc", 0, 10)
	if err != nil || page.Total != 1 || page.Items[0].TotalTokens != 11 || page.Items[0].CacheCreationTokens != 2 || page.Items[0].EffectiveCacheReadTokens != 0 {
		t.Fatalf("usage records = %#v, %v", page, err)
	}

	raw, err = handleModelRouterABIMethod(t.Context(), pluginabi.MethodExecutorCountTokens, nil)
	if err != nil {
		t.Fatalf("executor.count_tokens error = %v", err)
	}
	var countEnvelope abiEnvelope
	if err := json.Unmarshal(raw, &countEnvelope); err != nil {
		t.Fatalf("decode count envelope: %v", err)
	}
	if countEnvelope.OK || countEnvelope.Error == nil || countEnvelope.Error.HTTPStatus != 501 || countEnvelope.Error.Code != "model_route_count_tokens_unsupported" {
		t.Fatalf("count envelope = %#v", countEnvelope)
	}
}

func TestABIRegistrationUsesStreamBodyOmissionWhenHostSupportsIt(t *testing.T) {
	resetModelRouterABIState(t)
	request, err := json.Marshal(lifecycleRequest{ConfigYAML: []byte("routes: []\n"), SchemaVersion: streamBodyOmissionSchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := handleModelRouterABIMethod(t.Context(), pluginabi.MethodPluginRegister, request)
	if err != nil {
		t.Fatal(err)
	}
	var envelope abiEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	var registration abiRegistration
	if err := json.Unmarshal(envelope.Result, &registration); err != nil {
		t.Fatal(err)
	}
	if registration.SchemaVersion != streamBodyOmissionSchemaVersion {
		t.Fatalf("schema version = %d, want %d", registration.SchemaVersion, streamBodyOmissionSchemaVersion)
	}
}

func TestABIManagementDispatchBoundaryPayloads(t *testing.T) {
	resetModelRouterABIState(t)
	registrationRequest, err := json.Marshal(lifecycleRequest{ConfigYAML: []byte("routes:\n  - alias: auto\n    models: [target-a]\n")})
	if err != nil {
		t.Fatal(err)
	}
	registrationRaw, err := handleModelRouterABIMethod(t.Context(), pluginabi.MethodPluginRegister, registrationRequest)
	if err != nil {
		t.Fatalf("register method: %v", err)
	}
	var registrationEnvelope abiEnvelope
	if err := json.Unmarshal(registrationRaw, &registrationEnvelope); err != nil {
		t.Fatalf("decode registration response: %v", err)
	}
	if !registrationEnvelope.OK {
		t.Fatalf("registration response not OK: %s", registrationRaw)
	}
	canonicalRequest, err := json.Marshal(managementRPCRequest{ManagementRequest: pluginapi.ManagementRequest{
		Method: "GET",
		Path:   modelRouterDashboardPath,
	}})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		method     string
		request    []byte
		wantErr    string
		wantOK     bool
		wantStatus int
	}{
		{name: "unknown method", method: "unknown.method", wantOK: false},
		{name: "malformed management request", method: pluginabi.MethodManagementHandle, request: []byte(`{`), wantErr: "decode management.handle request"},
		{name: "empty management request", method: pluginabi.MethodManagementHandle, wantErr: "decode management.handle request"},
		{name: "null management request", method: pluginabi.MethodManagementHandle, request: []byte(`null`), wantOK: true, wantStatus: 404},
		{name: "canonical management request", method: pluginabi.MethodManagementHandle, request: canonicalRequest, wantOK: true, wantStatus: 200},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := handleModelRouterABIMethod(t.Context(), test.method, test.request)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var envelope abiEnvelope
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if envelope.OK != test.wantOK {
				t.Fatalf("envelope OK = %t, want %t; raw=%s", envelope.OK, test.wantOK, raw)
			}
			if test.method == "unknown.method" {
				if envelope.Error == nil || envelope.Error.Code != "unknown_method" {
					t.Fatalf("unknown method envelope = %#v", envelope)
				}
				return
			}
			var response pluginapi.ManagementResponse
			if err := json.Unmarshal(envelope.Result, &response); err != nil {
				t.Fatalf("decode management response: %v", err)
			}
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.wantStatus)
			}
			if test.wantStatus == 200 && len(response.Body) == 0 {
				t.Fatal("canonical dashboard body is empty")
			}
		})
	}
}
func resetModelRouterABIState(t *testing.T) {
	t.Helper()
	previousResolver := defaultDataPathResolver
	testDataPath := filepath.Join(t.TempDir(), defaultDataFileName)
	defaultDataPathResolver = func() string { return testDataPath }
	modelRouterABIState.Lock()
	previousPlugin := modelRouterABIState.plugin
	modelRouterABIState.plugin = nil
	modelRouterABIState.metadata = pluginapi.Metadata{}
	modelRouterABIState.shuttingDown = false
	modelRouterABIState.Unlock()
	if previousPlugin != nil && previousPlugin.store != nil {
		_ = previousPlugin.store.Close()
	}
	t.Cleanup(func() {
		modelRouterABIState.Lock()
		plugin := modelRouterABIState.plugin
		modelRouterABIState.plugin = nil
		modelRouterABIState.metadata = pluginapi.Metadata{}
		modelRouterABIState.shuttingDown = false
		modelRouterABIState.Unlock()
		if plugin != nil && plugin.store != nil {
			_ = plugin.store.Close()
		}
		defaultDataPathResolver = previousResolver
	})
}

func TestABIReconfigureRetainsInstalledPluginAfterFailure(t *testing.T) {
	resetModelRouterABIState(t)
	configure := func(method, config string) error {
		request, err := json.Marshal(lifecycleRequest{ConfigYAML: []byte(config)})
		if err != nil {
			return err
		}
		_, err = handleModelRouterABIMethod(t.Context(), method, request)
		return err
	}
	if err := configure(pluginabi.MethodPluginRegister, "routes:\n  - alias: smart\n    targets:\n      - model: provider-a\n"); err != nil {
		t.Fatalf("initial registration error = %v", err)
	}
	modelRouterABIState.Lock()
	initial := modelRouterABIState.plugin
	modelRouterABIState.Unlock()
	if initial == nil {
		t.Fatal("initial plugin = nil")
	}
	if err := configure(pluginabi.MethodPluginReconfigure, "routes:\n  - alias: smart\n    targets:\n      - model: provider-b\n"); err != nil {
		t.Fatalf("successful reconfigure error = %v", err)
	}
	modelRouterABIState.Lock()
	installed := modelRouterABIState.plugin
	modelRouterABIState.Unlock()
	if installed == initial {
		t.Fatal("successful reconfigure retained previous plugin")
	}
	if err := configure(pluginabi.MethodPluginReconfigure, "routes:\n  - alias: smart\n    strategy: unknown\n    targets:\n      - model: provider-c\n"); err == nil {
		t.Fatal("invalid reconfigure error = nil")
	}
	modelRouterABIState.Lock()
	afterFailure := modelRouterABIState.plugin
	modelRouterABIState.Unlock()
	if afterFailure != installed {
		t.Fatal("failed reconfigure replaced installed plugin")
	}
	if err := configure(pluginabi.MethodPluginReconfigure, "routes:\n  - alias: smart\n    targets:\n      - model: provider-c\n"); err != nil {
		t.Fatalf("recovery reconfigure error = %v", err)
	}
	modelRouterABIState.Lock()
	recovered := modelRouterABIState.plugin
	modelRouterABIState.Unlock()
	if recovered == installed {
		t.Fatal("recovery reconfigure retained failed generation")
	}
}

func TestABIRequestIdentityNegotiationAndDispatch(t *testing.T) {
	for _, tc := range []struct {
		name         string
		hostSchema   uint32
		pluginSchema uint32
		identity     bool
	}{
		{name: "legacy schema 6", hostSchema: 6, pluginSchema: 3},
		{name: "identity schema 7", hostSchema: 7, pluginSchema: 7, identity: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetModelRouterABIState(t)
			request, err := json.Marshal(lifecycleRequest{ConfigYAML: []byte("routes: []\n"), SchemaVersion: tc.hostSchema})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := handleModelRouterABIMethod(t.Context(), pluginabi.MethodPluginRegister, request)
			if err != nil {
				t.Fatal(err)
			}
			var envelope abiEnvelope
			var registration abiRegistration
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(envelope.Result, &registration); err != nil {
				t.Fatal(err)
			}
			if !envelope.OK || registration.SchemaVersion != tc.pluginSchema || registration.Capabilities.RequestLifecycle != tc.identity {
				t.Fatalf("registration = %#v, envelope = %#v", registration, envelope)
			}

			intercept, err := json.Marshal(pluginapi.RequestInterceptRequest{
				RequestID: "execution", TraceID: "1234", RequestedModel: "direct",
				Metadata: map[string]any{"request_path": "/v1/chat/completions"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := handleModelRouterABIMethod(t.Context(), pluginabi.MethodRequestInterceptBefore, intercept); err != nil {
				t.Fatal(err)
			}
			completion, err := json.Marshal(pluginapi.RequestCompletion{
				RequestID: "execution", TraceID: "1234", Outcome: pluginapi.RequestCompletionSucceeded, StatusCode: 200,
			})
			if err != nil {
				t.Fatal(err)
			}
			raw, err = handleModelRouterABIMethod(t.Context(), pluginabi.MethodRequestComplete, completion)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.OK != tc.identity || (!tc.identity && (envelope.Error == nil || envelope.Error.Code != "unknown_method")) {
				t.Fatalf("completion envelope = %#v", envelope)
			}

			usage := []byte(`{"Provider":"openai","Model":"provider-a","TraceID":"1234","RequestEndpoint":"POST /v1/chat/completions","Detail":{"TotalTokens":11}}`)
			raw, err = handleModelRouterABIMethod(t.Context(), pluginabi.MethodUsageHandle, usage)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &envelope); err != nil || !envelope.OK {
				t.Fatalf("usage envelope = %#v, %v", envelope, err)
			}
			modelRouterABIState.Lock()
			plugin := modelRouterABIState.plugin
			modelRouterABIState.Unlock()
			records, err := plugin.store.referenceUsageRecords(usageFilter{})
			if err != nil || len(records) != 1 {
				t.Fatalf("usage records = %#v, %v", records, err)
			}
			wantID, wantEndpoint := "", ""
			if tc.identity {
				wantID, wantEndpoint = "1234", "POST /v1/chat/completions"
			}
			if records[0].RequestID != wantID || records[0].RequestEndpoint != wantEndpoint || records[0].ProviderModel != "provider-a" || records[0].TotalTokens != 11 {
				t.Fatalf("usage record = %#v", records[0])
			}
			var sessions []storedRequestSession
			if err := plugin.store.readSnapshot(func(connection *sql.Conn, _ int) error {
				return scanRequestSessions(connection, func(session storedRequestSession) { sessions = append(sessions, session) })
			}); err != nil {
				t.Fatal(err)
			}
			if !tc.identity && len(sessions) != 0 {
				t.Fatalf("legacy sessions = %#v", sessions)
			}
			if tc.identity && (len(sessions) != 1 || sessions[0].RequestID != "1234" || sessions[0].StatusCode != 200 || sessions[0].Outcome != string(pluginapi.RequestCompletionSucceeded)) {
				t.Fatalf("identity sessions = %#v", sessions)
			}
		})
	}
}
