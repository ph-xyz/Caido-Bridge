package tools

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"
)

const previewTokenTTL = 2 * time.Minute

type previewGrant struct {
	ProjectID           string
	RequestID           string
	ScopeID             string
	SourceFingerprint   string
	PreparedFingerprint string
	ExpiresAt           time.Time
}

// PreviewStore holds short-lived, one-use grants created by
// caido_preview_replay. It is shared with the active Replay tools so a preview
// authorizes only the exact project, History row, scope, and prepared request
// that the caller reviewed.
type PreviewStore struct {
	mu     sync.Mutex
	grants map[string]previewGrant
	now    func() time.Time
	ttl    time.Duration
}

func NewPreviewStore() *PreviewStore {
	return &PreviewStore{
		grants: make(map[string]previewGrant),
		now:    time.Now,
		ttl:    previewTokenTTL,
	}
}

func (s *PreviewStore) issue(
	prepared preparedReplay,
	scopeID string,
) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, fmt.Errorf("preview store is not configured")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", time.Time{}, fmt.Errorf("create preview token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random[:])
	now := s.now().UTC()
	expiresAt := now.Add(s.ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(now)
	s.grants[token] = previewGrant{
		ProjectID:           prepared.Project.ID,
		RequestID:           prepared.Record.ID,
		ScopeID:             strings.TrimSpace(scopeID),
		SourceFingerprint:   prepared.SourceView.SourceFingerprint,
		PreparedFingerprint: prepared.PreparedView.PreparedRequestFingerprint,
		ExpiresAt:           expiresAt,
	}
	return token, expiresAt, nil
}

func (s *PreviewStore) consume(
	token string,
	prepared preparedReplay,
	scopeID string,
) error {
	if s == nil {
		return fmt.Errorf("preview store is not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("previewToken from caido_preview_replay is required")
	}

	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, ok := s.grants[token]
	if ok {
		delete(s.grants, token)
	}
	s.purgeExpiredLocked(now)
	if !ok {
		return fmt.Errorf("preview token is invalid, expired, or already used")
	}
	if !now.Before(grant.ExpiresAt) {
		return fmt.Errorf("preview token is invalid, expired, or already used")
	}
	if grant.ProjectID != prepared.Project.ID ||
		grant.RequestID != prepared.Record.ID ||
		grant.ScopeID != strings.TrimSpace(scopeID) ||
		grant.SourceFingerprint != prepared.SourceView.SourceFingerprint ||
		grant.PreparedFingerprint != prepared.PreparedView.PreparedRequestFingerprint {
		return fmt.Errorf("preview token does not match the validated request; preview the exact request again")
	}
	return nil
}

func (s *PreviewStore) purgeExpiredLocked(now time.Time) {
	for token, grant := range s.grants {
		if !now.Before(grant.ExpiresAt) {
			delete(s.grants, token)
		}
	}
}
