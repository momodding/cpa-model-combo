package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestRequestSessionTrackerPrunesAndCaps(t *testing.T) {
	now := time.Now().UTC()
	tracker := newRequestSessionTracker(func() time.Time { return now })
	tracker.Stage(stagedRequestSession{requestID: "expired"})
	now = now.Add(requestSessionRetention + time.Nanosecond)
	if _, ok := tracker.Consume("expired"); ok {
		t.Fatal("expired session was retained")
	}
	for i := 0; i <= maxStagedRequestSessions; i++ {
		tracker.Stage(stagedRequestSession{requestID: fmt.Sprint(i)})
	}
	if _, ok := tracker.Consume("0"); ok {
		t.Fatal("oldest session was not evicted at capacity")
	}
	if _, ok := tracker.Consume(fmt.Sprint(maxStagedRequestSessions)); !ok {
		t.Fatal("newest session was evicted")
	}
	if _, ok := tracker.Consume(fmt.Sprint(maxStagedRequestSessions)); ok {
		t.Fatal("session was consumed twice")
	}
}

func TestRequestSessionLifecycle(t *testing.T) {
	store, err := openUsageStore(filepath.Join(t.TempDir(), "usage.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	plugin := &modelRouterPlugin{
		config: routerConfig{Enabled: true, Routes: []modelRoute{{Alias: "auto"}}},
		store:  store, attribution: newAttributionTracker(nil), requestSessions: newRequestSessionTracker(nil), requestIdentityEnabled: true,
	}
	ctx := context.Background()
	started := time.Now().UTC().Add(-time.Second)
	request := pluginapi.RequestInterceptRequest{RequestID: "execution", TraceID: "1234", RequestedModel: "auto", Stream: true,
		Metadata: map[string]any{"request_path": "/v1/chat/completions"}}
	if _, err := plugin.InterceptRequestBeforeAuth(ctx, request); err != nil {
		t.Fatal(err)
	}
	completion := pluginapi.RequestCompletion{RequestID: "execution", TraceID: "1234", Outcome: pluginapi.RequestCompletionSucceeded,
		StatusCode: 200, StartedAt: started, CompletedAt: started.Add(time.Second), Error: "secret provider payload"}
	if err := plugin.HandleRequestComplete(ctx, completion); err != nil {
		t.Fatal(err)
	}
	if err := plugin.HandleRequestComplete(ctx, completion); err != nil {
		t.Fatal(err)
	}
	var sessions []storedRequestSession
	read := func() {
		sessions = nil
		if err := store.readSnapshot(func(conn *sql.Conn, _ int) error {
			return scanRequestSessions(conn, func(session storedRequestSession) { sessions = append(sessions, session) })
		}); err != nil {
			t.Fatal(err)
		}
	}
	read()
	if len(sessions) != 1 || sessions[0].RequestID != "1234" || sessions[0].Endpoint != "/v1/chat/completions" ||
		sessions[0].RequestedModel != "auto" || sessions[0].RouterModel != "auto" || sessions[0].Attribution != attributionRouted ||
		!sessions[0].Stream || sessions[0].Outcome != "succeeded" || sessions[0].StatusCode != 200 ||
		!sessions[0].StartedAt.Equal(started) || !sessions[0].CompletedAt.Equal(started.Add(time.Second)) || sessions[0].LatencyNS != uint64(time.Second) {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
	var payload string
	if err := store.db.QueryRow(`SELECT payload FROM request_sessions WHERE request_id = ?`, "1234").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, completion.Error) {
		t.Fatal("completion error persisted")
	}

	request.RequestID, request.TraceID = "mismatch", "original"
	_, _ = plugin.InterceptRequestBeforeAuth(ctx, request)
	completion.RequestID, completion.TraceID = "mismatch", "different"
	if err := plugin.HandleRequestComplete(ctx, completion); !errors.Is(err, errRequestSessionTraceMismatch) {
		t.Fatalf("mismatch error = %v", err)
	}
	if strings.Contains(store.LastError(), "original") || strings.Contains(store.LastError(), "different") || store.LastError() == "" {
		t.Fatalf("unsafe or missing storage error: %q", store.LastError())
	}
	request.RequestID, request.TraceID = "nested", "nested-trace"
	request.Metadata["source"] = hostModelCallbackSource
	_, _ = plugin.InterceptRequestBeforeAuth(ctx, request)
	completion.RequestID, completion.TraceID = "nested", "nested-trace"
	if err := plugin.HandleRequestComplete(ctx, completion); err != nil {
		t.Fatal(err)
	}
	read()
	if len(sessions) != 1 {
		t.Fatalf("mismatched or nested session persisted: %#v", sessions)
	}
	completion.StartedAt, completion.CompletedAt = started.Add(time.Millisecond), started.Add(time.Second)
	request.Metadata = nil
	request.RequestID, request.TraceID = "internal", ""
	request.RequestedModel, request.Stream = "direct", false
	_, _ = plugin.InterceptRequestBeforeAuth(ctx, request)
	completion.RequestID, completion.TraceID = "internal", ""
	if err := plugin.HandleRequestComplete(ctx, completion); err != nil {
		t.Fatal(err)
	}
	read()
	if len(sessions) != 2 || sessions[1].RequestID != "internal" || sessions[1].Attribution != attributionDirect {
		t.Fatalf("non-HTTP fallback sessions = %#v", sessions)
	}

	request.RequestID = "missing-http-trace"
	request.Metadata = map[string]any{"request_path": "/v1/chat/completions"}
	_, _ = plugin.InterceptRequestBeforeAuth(ctx, request)
	completion.RequestID = request.RequestID
	if err := plugin.HandleRequestComplete(ctx, completion); !errors.Is(err, errRequestSessionMissingTrace) {
		t.Fatalf("missing HTTP trace error = %v", err)
	}
	plugin.requestIdentityEnabled = false
	request.RequestID, request.TraceID = "legacy", "legacy-trace"
	_, _ = plugin.InterceptRequestBeforeAuth(ctx, request)
	plugin.requestIdentityEnabled = true
	completion.RequestID, completion.TraceID = request.RequestID, request.TraceID
	if err := plugin.HandleRequestComplete(ctx, completion); err != nil {
		t.Fatal(err)
	}
	read()
	if len(sessions) != 2 {
		t.Fatalf("missing HTTP identity or legacy request persisted: %#v", sessions)
	}
}
