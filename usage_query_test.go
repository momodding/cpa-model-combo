package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageRequestSelectionRoutingSkipsExactSession(t *testing.T) {
	now := time.Now().UTC()
	selection := newUsageRequestSelection(usagePageOptions{Sort: "time", Order: "asc", Limit: 10}, usageFilter{})
	selection.addSession(storedRequestSession{TraceID: "shared", LifecycleRequestID: "first", StartedAt: now, Outcome: "rejected"})
	selection.addSession(storedRequestSession{TraceID: "shared", LifecycleRequestID: "second", StartedAt: now})
	selection.addSession(storedRequestSession{TraceID: "other", LifecycleRequestID: "first", StartedAt: now})
	selection.addSkip(storedRoutingSkip{TraceID: "shared", LifecycleRequestID: "first", Attempt: 2, RequestedAt: now, Model: "cooling", RetryAt: now.Add(time.Minute)})
	selection.addSkip(storedRoutingSkip{TraceID: "other", LifecycleRequestID: "first", Attempt: 1, RequestedAt: now, Model: "other-model"})
	selection.addSkip(storedRoutingSkip{TraceID: "missing", LifecycleRequestID: "first", Attempt: 1, RequestedAt: now, Model: "orphan"})
	page := selection.finish(0, now)
	if page.Total != 3 {
		t.Fatalf("total = %d, want 3", page.Total)
	}
	for _, parent := range page.Items {
		switch {
		case parent.TraceID == "shared" && parent.LifecycleRequestID == "first":
			if parent.CorrelationWarning == "" || parent.Outcome != "rejected" || parent.TotalTokens != 0 || parent.AttemptCount != 1 {
				t.Fatalf("first parent = %#v", parent)
			}
			child := parent.Attempts[0]
			if child.TraceID != parent.TraceID || child.LifecycleRequestID != parent.LifecycleRequestID || child.ProviderModel != "cooling" || child.Status != "cooling_down" || child.Dispatched == nil || *child.Dispatched || child.Order != 3 || child.RetryAt == nil || !child.RetryAt.Equal(now.Add(time.Minute)) || child.EstimatedCost != nil || child.RequestID != "" {
				t.Fatalf("skip child = %#v", child)
			}
		case parent.TraceID == "shared" && parent.LifecycleRequestID == "second":
			if parent.CorrelationWarning == "" || len(parent.Attempts) != 0 {
				t.Fatalf("second parent = %#v", parent)
			}
		case parent.TraceID == "other" && parent.LifecycleRequestID == "first":
			if len(parent.Attempts) != 1 || parent.Attempts[0].ProviderModel != "other-model" || parent.Attempts[0].RetryAt != nil {
				t.Fatalf("other parent = %#v", parent)
			}
		default:
			t.Fatalf("unexpected parent = %#v", parent)
		}
	}
}

func TestRoutingSkipsShareSessionSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	store, err := openUsageStore(path, 365)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	writer, err := openUsageStore(path, 365)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	now := time.Now().UTC()
	if err := writer.RecordSession(storedRequestSession{TraceID: "trace", LifecycleRequestID: "request", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	err = store.readSnapshot(func(connection *sql.Conn, _ int) error {
		selection := newUsageRequestSelection(usagePageOptions{Limit: 10}, usageFilter{})
		if err := scanRequestSessions(connection, selection.addSession); err != nil {
			return err
		}
		if err := writer.RecordRoutingSkip(storedRoutingSkip{TraceID: "trace", LifecycleRequestID: "request", Attempt: 0, RequestedAt: now, Model: "cooling"}); err != nil {
			return err
		}
		if err := scanRoutingSkips(connection, selection.addSkip); err != nil {
			return err
		}
		page := selection.finish(0, now)
		if len(page.Items) != 1 || len(page.Items[0].Attempts) != 0 {
			return fmt.Errorf("snapshot saw concurrent skip: %#v", page)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.Requests(usageFilter{}, "time", "asc", 0, 10)
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Attempts) != 1 || page.Items[0].Attempts[0].ProviderModel != "cooling" {
		t.Fatalf("next snapshot = %#v, %v", page, err)
	}
}
