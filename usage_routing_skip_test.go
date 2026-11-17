package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSchemaThreeMigrationPreservesUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	store, err := openUsageStore(path, 365)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	if err := store.Record(storedUsageRecord{RequestedAt: now, ProviderModel: "preserved", TotalTokens: 7}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSession(storedRequestSession{TraceID: "trace", LifecycleRequestID: "life", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavePriceBook(saveModelPricesRequest{Prices: map[string]modelPrice{"preserved": {tokenRates: tokenRates{Input: 1}}}}, now); err != nil {
		t.Fatal(err)
	}
	prefs := defaultDashboardPreferences()
	prefs.RequestPageSize = 50
	if _, err := store.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	var request, aggregate, session, prices, preferences []byte
	var sequence int
	for _, q := range []struct {
		sql string
		dst *[]byte
	}{
		{`SELECT payload FROM requests`, &request},
		{`SELECT payload FROM minute_aggregates`, &aggregate},
		{`SELECT payload FROM request_sessions`, &session},
		{`SELECT prices_json FROM store_state WHERE id=1`, &prices},
		{`SELECT preferences_json FROM store_state WHERE id=1`, &preferences},
	} {
		if err := store.db.QueryRow(q.sql).Scan(q.dst); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.db.QueryRow(`SELECT next_sequence FROM store_state WHERE id=1`).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DROP TABLE routing_skips`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE store_state SET storage_schema_version=3 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = openUsageStore(path, 365)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var version, gotSequence int
	if err := store.db.QueryRow(`SELECT storage_schema_version, next_sequence FROM store_state WHERE id=1`).Scan(&version, &gotSequence); err != nil {
		t.Fatal(err)
	}
	if version != 4 || gotSequence != sequence {
		t.Fatalf("state = %d/%d, want 4/%d", version, gotSequence, sequence)
	}
	for _, q := range []struct {
		sql  string
		want []byte
	}{
		{`SELECT payload FROM requests`, request}, {`SELECT payload FROM minute_aggregates`, aggregate},
		{`SELECT payload FROM request_sessions`, session}, {`SELECT prices_json FROM store_state WHERE id=1`, prices},
		{`SELECT preferences_json FROM store_state WHERE id=1`, preferences},
	} {
		var got []byte
		if err := store.db.QueryRow(q.sql).Scan(&got); err != nil || !bytes.Equal(got, q.want) {
			t.Fatalf("%s: got %s, %v; want %s", q.sql, got, err, q.want)
		}
	}
	var indexes int
	if err := store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND tbl_name='routing_skips' AND sql LIKE '%requested_at_ns%'`).Scan(&indexes); err != nil || indexes != 1 {
		t.Fatalf("retention index = %d, %v", indexes, err)
	}
	if err := store.Record(storedUsageRecord{RequestedAt: now, ProviderModel: "next"}); err != nil {
		t.Fatal(err)
	}
	var next int
	if err := store.db.QueryRow(`SELECT max(sequence) FROM requests`).Scan(&next); err != nil || next != sequence+1 {
		t.Fatalf("next sequence = %d, %v", next, err)
	}
}

func TestRoutingSkipPersistenceAccountingRetentionAndReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	store, err := openUsageStore(path, 365)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	if err := store.Record(storedUsageRecord{RequestedAt: now, ProviderModel: "executed", TotalTokens: 7, Failed: true}); err != nil {
		t.Fatal(err)
	}
	for _, session := range []storedRequestSession{
		{TraceID: "shared", LifecycleRequestID: "one", StartedAt: now, Outcome: "rejected", HTTPStatus: 429},
		{TraceID: "shared", LifecycleRequestID: "two", StartedAt: now, Outcome: "succeeded", HTTPStatus: 200},
		{TraceID: "other", LifecycleRequestID: "one", StartedAt: now, Outcome: "succeeded", HTTPStatus: 200},
	} {
		if err := store.RecordSession(session); err != nil {
			t.Fatal(err)
		}
	}
	book, err := store.SavePriceBook(saveModelPricesRequest{Prices: map[string]modelPrice{"executed": {tokenRates: tokenRates{Input: 1}}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	prefs := defaultDashboardPreferences()
	prefs.RequestPageSize = 50
	if _, err := store.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	before, err := store.Overview(usageFilter{}, "hour")
	if err != nil {
		t.Fatal(err)
	}
	groupsBefore, err := store.Groups(usageFilter{}, "provider_model", "total_tokens", "desc", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, skip := range []storedRoutingSkip{
		{TraceID: "shared", LifecycleRequestID: "one", Attempt: 1, RequestedAt: now, Model: "cool-one", CredentialID: "account", CredentialLabel: "safe", RetryAt: now.Add(time.Minute)},
		{TraceID: "shared", LifecycleRequestID: "two", Attempt: 1, RequestedAt: now, Model: "cool-two", RetryAt: now.Add(time.Minute)},
		{TraceID: "other", LifecycleRequestID: "one", Attempt: 1, RequestedAt: now, Model: "cool-three", RetryAt: now.Add(time.Minute)},
		{TraceID: "old", LifecycleRequestID: "old", Attempt: 1, RequestedAt: now.AddDate(0, 0, -2), Model: "expired"},
	} {
		if err := store.RecordRoutingSkip(skip); err != nil {
			t.Fatal(err)
		}
	}
	after, err := store.Overview(usageFilter{}, "hour")
	if err != nil {
		t.Fatal(err)
	}
	groupsAfter, err := store.Groups(usageFilter{}, "provider_model", "total_tokens", "desc", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Summary, after.Summary) || !reflect.DeepEqual(before.Costs, after.Costs) || !reflect.DeepEqual(before.ProviderModels, after.ProviderModels) || !reflect.DeepEqual(before.RouterModels, after.RouterModels) || !reflect.DeepEqual(groupsBefore.Items, groupsAfter.Items) {
		t.Fatalf("skips changed accounting: before=%#v %#v, after=%#v %#v", before, groupsBefore, after, groupsAfter)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = openUsageStore(path, 365)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	page, err := store.Requests(usageFilter{}, "time", "asc", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"shared/one": "cool-one", "shared/two": "cool-two", "other/one": "cool-three"}
	for _, parent := range page.Items {
		key := parent.TraceID + "/" + parent.LifecycleRequestID
		model, ok := want[key]
		if !ok {
			continue
		}
		if len(parent.Attempts) != 1 || parent.Attempts[0].ProviderModel != model {
			t.Fatalf("%s attempts = %#v", key, parent.Attempts)
		}
		data, err := json.Marshal(parent.Attempts[0])
		if err != nil {
			t.Fatal(err)
		}
		var detail map[string]any
		if err := json.Unmarshal(data, &detail); err != nil {
			t.Fatal(err)
		}
		if detail["status"] != "cooling_down" || detail["dispatched"] != false || detail["request_id"] != nil {
			t.Fatalf("synthetic detail = %s", data)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing parents: %v", want)
	}
	if err := pruneSQLiteDatabase(store.db, 1, now, true); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM routing_skips`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("retained skips = %d, %v", count, err)
	}
	if err := store.ResetUsage(); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM routing_skips`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("skips after reset = %d, %v", count, err)
	}
	loadedBook, err := store.QueryPriceBook()
	if err != nil || loadedBook.Revision != book.Revision {
		t.Fatalf("prices after reset = %#v, %v", loadedBook, err)
	}
	loadedPrefs, err := store.QueryPreferences()
	if err != nil || loadedPrefs.RequestPageSize != 50 {
		t.Fatalf("preferences after reset = %#v, %v", loadedPrefs, err)
	}
}
