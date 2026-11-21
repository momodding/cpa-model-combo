package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type fakeCatalogHost struct {
	fakeModelHost
	models    []pluginapi.HostAuthModels
	files     []pluginapi.HostAuthFileEntry
	modelsErr error
	listErr   error
}

func (host *fakeCatalogHost) AuthModels() ([]pluginapi.HostAuthModels, error) {
	return host.models, host.modelsErr
}

func (host *fakeCatalogHost) AuthList() ([]pluginapi.HostAuthFileEntry, error) {
	return host.files, host.listErr
}

func TestAccountCatalogJoinsSafeFieldsAndExcludesRouterAliases(t *testing.T) {
	plugin := testRouterPlugin(testModelRoute("smart", routeStrategyPriority, 30, "gpt-5"))
	plugin.credentials = &credentialSnapshots{}
	host := &fakeCatalogHost{
		models: []pluginapi.HostAuthModels{
			{ID: "b.json", AuthIndex: "i2", Provider: "claude", Models: []string{"claude-x", "smart"}},
			{ID: "a.json", AuthIndex: "i1", Provider: "codex", Models: []string{"gpt-5", "gpt-5", " "}},
			{ID: "registry-only", Provider: "codex", Models: []string{"gpt-5"}},
			{ID: "route", Provider: pluginID, Models: []string{"smart"}},
		},
		files: []pluginapi.HostAuthFileEntry{
			{ID: "a.json", Label: "Work", Disabled: true, Path: "/secret/a.json", Source: "file", Note: "sk-secret", ProjectID: "project", Account: "account"},
			{ID: "b.json", Email: "user@example.com"},
		},
	}
	catalog, err := plugin.accountCatalog(host)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 3 || catalog[0].ID != "a.json" || catalog[1].ID != "b.json" || catalog[2].ID != "registry-only" {
		t.Fatalf("catalog = %#v", catalog)
	}
	a, b, registryOnly := catalog[0], catalog[1], catalog[2]
	if a.Label != "Work" || !a.Disabled || a.AuthIndex != "i1" || a.Provider != "codex" || len(a.Models) != 1 || a.Models[0] != (accountModel{ID: "gpt-5", Entitlement: "unknown"}) {
		t.Fatalf("a = %#v", a)
	}
	if a.ProjectID != "project" || a.Account != "account" {
		t.Fatalf("metadata missing: %#v", a)
	}
	if b.Label != "user@example.com" || len(b.Models) != 1 || b.Models[0].ID != "claude-x" {
		t.Fatalf("b = %#v", b)
	}
	if registryOnly.Label != "" || registryOnly.Disabled {
		t.Fatalf("registry-only = %#v", registryOnly)
	}
	if got := plugin.credentials.label("a.json", ""); got != "Work" {
		t.Fatalf("refreshed snapshot label = %q", got)
	}
	if !a.supportsModel("gpt-5(high)") || a.supportsModel("gpt-4") {
		t.Fatal("supportsModel must match exact IDs with only the thinking suffix removed")
	}
}

func TestAccountCatalogUnknownSupportBeforeFiltering(t *testing.T) {
	plugin := testRouterPlugin(testModelRoute("smart", routeStrategyPriority, 30, "gpt-5"))
	plugin.credentials = &credentialSnapshots{}
	host := &fakeCatalogHost{models: []pluginapi.HostAuthModels{
		{ID: "unknown", Provider: "codex"},
		{ID: "filtered", Provider: "codex", Models: []string{"smart", " "}},
	}}
	catalog, err := plugin.accountCatalog(host)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 2 || catalog[0].ModelSupportUnknown || catalog[0].supportsModel("gpt-5") || !catalog[1].ModelSupportUnknown || !catalog[1].supportsModel("gpt-5(high)") {
		t.Fatalf("catalog = %#v", catalog)
	}
}

func TestAccountCatalogFailsClosed(t *testing.T) {
	plugin := testRouterPlugin(testModelRoute("smart", routeStrategyPriority, 30, "gpt-5"))
	for name, host := range map[string]modelHost{
		"no catalog capability": &fakeModelHost{},
		"models failure":        &fakeCatalogHost{modelsErr: errors.New("boom")},
		"list failure":          &fakeCatalogHost{listErr: errors.New("boom")},
	} {
		_, err := plugin.accountCatalog(host)
		if !errors.Is(err, errAccountCatalogUnavailable) {
			t.Fatalf("%s: err = %v", name, err)
		}
		if strings.Contains(err.Error(), "sk-") {
			t.Fatalf("%s: leaked detail: %v", name, err)
		}
	}
}

func TestAccountCatalogAvailability(t *testing.T) {
	next := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	host := &fakeCatalogHost{
		models: []pluginapi.HostAuthModels{{ID: "unknown"}},
		files:  []pluginapi.HostAuthFileEntry{{ID: "unknown", Unavailable: true, NextRetryAfter: next, StatusMessage: "secret"}},
	}
	plugin := testRouterPlugin(testModelRoute("smart", routeStrategyPriority, 30, "a"))
	catalog, err := plugin.accountCatalog(host)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 1 || !catalog[0].Unavailable || !catalog[0].NextRetryAfter.Equal(next) {
		t.Fatalf("catalog = %#v", catalog)
	}
}
