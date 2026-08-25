package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPreviewTokenExpires(t *testing.T) {
	prepared, err := prepareReplay(
		context.Background(),
		replayReader(),
		testProjectID,
		"889",
		replayScopeID,
		expectedReplayIdentity(),
		nil,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	store := NewPreviewStore()
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	store.ttl = time.Minute
	token, expiresAt, err := store.issue(prepared, replayScopeID)
	if err != nil {
		t.Fatal(err)
	}
	if !expiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("expiresAt = %v", expiresAt)
	}
	now = now.Add(time.Minute)
	err = store.consume(token, prepared, replayScopeID)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestPreviewTokenBindsScopeAndConsumesOnMismatch(t *testing.T) {
	prepared, err := prepareReplay(
		context.Background(),
		replayReader(),
		testProjectID,
		"889",
		replayScopeID,
		expectedReplayIdentity(),
		nil,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	store := NewPreviewStore()
	token, _, err := store.issue(prepared, replayScopeID)
	if err != nil {
		t.Fatal(err)
	}
	err = store.consume(token, prepared, "different-scope")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("scope mismatch error = %v", err)
	}
	err = store.consume(token, prepared, replayScopeID)
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("mismatched token was not consumed: %v", err)
	}
}
