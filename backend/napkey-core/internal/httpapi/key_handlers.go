package httpapi

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"napkey-core/internal/auth"
	"napkey-core/internal/kiro"
	"napkey-core/internal/logger"
	"napkey-core/internal/store"
)

// maxKeyNameLength bounds the label a user attaches to a key.
const maxKeyNameLength = 60

// keyView is the API shape for a key. The cleartext value is absent by design;
// it appears only in the create response.
type keyView struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	KeyMasked   string     `json:"keyMasked"`
	Prefix      string     `json:"prefix"`
	LastFour    string     `json:"lastFour"`
	TestMode    bool       `json:"testMode"`
	Enabled     bool       `json:"enabled"`
	Status      string     `json:"status"`
	TokenLimit  int64      `json:"tokenLimit"`
	CreditLimit float64    `json:"creditLimit"`
	TokensUsed  int64      `json:"tokensUsed"`
	CreditsUsed float64    `json:"creditsUsed"`
	Requests    int64      `json:"requestsCount"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
	RevokedAt   *time.Time `json:"revokedAt"`
	// SyncState tells the console whether the data plane has the key yet. Hiding
	// it would leave a user staring at a key that returns 401 with no explanation.
	SyncState string `json:"syncState"`
	SyncError string `json:"syncError,omitempty"`
}

func toKeyView(k *store.APIKey) keyView {
	status := "active"
	switch {
	case k.RevokedAt != nil:
		status = "revoked"
	case !k.Enabled:
		status = "disabled"
	case k.SyncState != store.SyncSynced:
		status = "provisioning"
	}
	return keyView{
		ID:          k.ID,
		Name:        k.Name,
		KeyMasked:   auth.DisplayKey(k.KeyPrefix, k.LastFour),
		Prefix:      k.KeyPrefix,
		LastFour:    k.LastFour,
		TestMode:    k.IsTestMode(),
		Enabled:     k.Enabled,
		Status:      status,
		TokenLimit:  k.TokenLimit,
		CreditLimit: k.CreditLimit,
		TokensUsed:  k.TokensUsed,
		CreditsUsed: k.CreditsUsed,
		Requests:    k.RequestsCount,
		CreatedAt:   k.CreatedAt,
		LastUsedAt:  k.LastUsedAt,
		RevokedAt:   k.RevokedAt,
		SyncState:   k.SyncState,
		SyncError:   k.SyncError,
	}
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	keys, err := s.store.ListAPIKeys(r.Context(), su.User.ID)
	if err != nil {
		writeStoreError(w, err, "listing api keys")
		return
	}
	out := make([]keyView, 0, len(keys))
	for i := range keys {
		out = append(out, toKeyView(&keys[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out})
}

func (s *Server) handleGetKey(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	key, err := s.store.GetAPIKey(r.Context(), su.User.ID, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "loading api key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": toKeyView(key)})
}

type createKeyRequest struct {
	Name     string `json:"name,omitempty"`
	TestMode bool   `json:"testMode,omitempty"`
}

// handleCreateKey mints a key, pushes it to kiro-go, and returns the cleartext once.
//
// The push is synchronous on purpose. napkey-core stores only the hash, so the
// cleartext exists solely for the duration of this request; if the push were
// deferred there would be nothing left to retry with. A user therefore either gets
// a key that works in the data plane, or an error and no key at all.
func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	var req createKeyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(name) > maxKeyNameLength {
		writeFieldErrors(w, map[string]string{"name": "name is too long"})
		return
	}
	if strings.ContainsAny(name, "\r\n\x00") {
		writeFieldErrors(w, map[string]string{"name": "name contains invalid characters"})
		return
	}

	generated, err := auth.GenerateKey(req.TestMode)
	if err != nil {
		logger.Errorf("generating api key failed: %v", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "something went wrong on our side")
		return
	}

	key, err := s.store.CreateAPIKey(r.Context(), store.CreateAPIKeyParams{
		UserID:      su.User.ID,
		Name:        name,
		KeyPrefix:   generated.Prefix,
		KeyHash:     generated.Hash,
		LastFour:    generated.LastFour,
		TokenLimit:  s.cfg.DefaultTokenLimit,
		CreditLimit: s.cfg.DefaultCreditLimit,
		MaxPerUser:  s.cfg.MaxKeysPerUser,
	})
	if err != nil {
		writeStoreError(w, err, "creating api key")
		return
	}

	// Push to the data plane while the cleartext is still in hand.
	enabled := true
	remoteID, pushErr := s.kiro.CreateKey(r.Context(), kiro.CreateKeyRequest{
		Name:        keyRemoteName(su.User.Email, key.ID),
		Key:         generated.Value,
		Enabled:     &enabled,
		RPMLimit:    intValue(key.RPMLimit),
		TPMLimit:    intValue(key.TPMLimit),
		TokenLimit:  key.TokenLimit,
		CreditLimit: key.CreditLimit,
	})
	if pushErr != nil {
		logger.Errorf("pushing new key %s to the data plane failed: %v", key.ID, pushErr)
		// Roll the row back so the user is not left with a key that cannot work and
		// cannot be repaired. Nothing was shown to them yet, so this is clean.
		if delErr := s.store.DeleteAPIKeyRow(r.Context(), key.ID); delErr != nil {
			logger.Errorf("rolling back key %s after a failed push failed: %v", key.ID, delErr)
			if markErr := s.store.MarkKeyUnusable(r.Context(), key.ID,
				"could not be registered in the data plane"); markErr != nil {
				logger.Errorf("marking key %s unusable failed: %v", key.ID, markErr)
			}
		}
		writeError(w, http.StatusServiceUnavailable, codeUpstreamFailure,
			"could not provision the key right now, please try again in a moment")
		return
	}

	if err := s.store.MarkKeySynced(r.Context(), key.ID, remoteID); err != nil {
		// The key works; only the bookkeeping lagged. The reconciler settles it.
		logger.Warnf("marking key %s synced failed: %v", key.ID, err)
	}

	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "user", ActorID: su.User.ID, Action: "api_key.create",
		TargetType: "api_key", TargetID: key.ID,
		Metadata: map[string]any{"testMode": req.TestMode, "prefix": key.KeyPrefix},
		IP:       clientIP(r, s.trustProxy),
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}

	fresh, err := s.store.GetAPIKey(r.Context(), su.User.ID, key.ID)
	if err != nil {
		fresh = key
	}
	view := toKeyView(fresh)
	writeJSON(w, http.StatusCreated, map[string]any{
		// The one and only time the cleartext key is returned.
		"key":     generated.Value,
		"warning": "copy this key now, it will not be shown again",
		"details": view,
	})
}

type updateKeyRequest struct {
	Name    *string `json:"name,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

func (s *Server) handleUpdateKey(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	keyID := r.PathValue("id")
	var req updateKeyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil && req.Enabled == nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "nothing to update")
		return
	}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if utf8.RuneCountInString(trimmed) > maxKeyNameLength {
			writeFieldErrors(w, map[string]string{"name": "name is too long"})
			return
		}
		if strings.ContainsAny(trimmed, "\r\n\x00") {
			writeFieldErrors(w, map[string]string{"name": "name contains invalid characters"})
			return
		}
		req.Name = &trimmed
	}

	// Load first so the ownership check happens before anything is written, and so
	// the remote id is known.
	existing, err := s.store.GetAPIKey(r.Context(), su.User.ID, keyID)
	if err != nil {
		writeStoreError(w, err, "loading api key")
		return
	}
	if existing.RevokedAt != nil {
		writeError(w, http.StatusConflict, codeConflict, "this key has been revoked")
		return
	}

	updated, err := s.store.UpdateAPIKey(r.Context(), su.User.ID, keyID, store.UpdateAPIKeyParams{
		Name:    req.Name,
		Enabled: req.Enabled,
	})
	if err != nil {
		writeStoreError(w, err, "updating api key")
		return
	}

	// Best-effort immediate push. Unlike create, this is retryable, so a failure
	// leaves the row queued rather than failing the request.
	if updated.RemoteID != "" {
		enabled := updated.Enabled
		tokenLimit := updated.TokenLimit
		creditLimit := updated.CreditLimit
		rpmLimit := intValue(updated.RPMLimit)
		tpmLimit := intValue(updated.TPMLimit)
		remoteName := keyRemoteName(su.User.Email, updated.ID)
		if err := s.kiro.UpdateKey(r.Context(), updated.RemoteID, kiro.UpdateKeyRequest{
			Name:        &remoteName,
			Enabled:     &enabled,
			RPMLimit:    &rpmLimit,
			TPMLimit:    &tpmLimit,
			TokenLimit:  &tokenLimit,
			CreditLimit: &creditLimit,
		}); err != nil {
			logger.Warnf("pushing key %s update to the data plane failed, queued for retry: %v", updated.ID, err)
			if markErr := s.store.MarkKeySyncFailed(r.Context(), updated.ID, err.Error(), false); markErr != nil {
				logger.Errorf("recording sync failure for key %s: %v", updated.ID, markErr)
			}
		} else if err := s.store.MarkKeySynced(r.Context(), updated.ID, updated.RemoteID); err != nil {
			logger.Warnf("marking key %s synced failed: %v", updated.ID, err)
		}
	}

	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "user", ActorID: su.User.ID, Action: "api_key.update",
		TargetType: "api_key", TargetID: keyID,
		Metadata: auditMetadataForUpdate(req),
		IP:       clientIP(r, s.trustProxy),
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}

	fresh, err := s.store.GetAPIKey(r.Context(), su.User.ID, keyID)
	if err != nil {
		fresh = updated
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": toKeyView(fresh)})
}

func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	keyID := r.PathValue("id")

	existing, err := s.store.GetAPIKey(r.Context(), su.User.ID, keyID)
	if err != nil {
		writeStoreError(w, err, "loading api key")
		return
	}

	if err := s.store.RevokeAPIKey(r.Context(), su.User.ID, keyID); err != nil {
		writeStoreError(w, err, "revoking api key")
		return
	}

	// Try to remove it from the data plane now. Revocation has to take effect
	// quickly, since the whole point is usually that the key leaked.
	if existing.RemoteID != "" {
		if err := s.kiro.DeleteKey(r.Context(), existing.RemoteID); err != nil {
			logger.Warnf("deleting key %s from the data plane failed, queued for retry: %v", keyID, err)
			if markErr := s.store.MarkKeySyncFailed(r.Context(), keyID, err.Error(), true); markErr != nil {
				logger.Errorf("recording delete failure for key %s: %v", keyID, markErr)
			}
		} else if err := s.store.DeleteSyncedKey(r.Context(), keyID); err != nil {
			logger.Warnf("finalizing deletion of key %s failed: %v", keyID, err)
		}
	} else if err := s.store.DeleteSyncedKey(r.Context(), keyID); err != nil {
		logger.Warnf("finalizing deletion of key %s failed: %v", keyID, err)
	}

	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "user", ActorID: su.User.ID, Action: "api_key.revoke",
		TargetType: "api_key", TargetID: keyID, IP: clientIP(r, s.trustProxy),
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked"})
}

// keyRemoteName is the label kiro-go stores. It embeds the napkey-core key id so
// an operator looking at the data plane can trace a key back to its owner, and so
// usage reports can name the right row.
func keyRemoteName(email, keyID string) string {
	return "napkey:" + keyID + " (" + email + ")"
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func auditMetadataForUpdate(req updateKeyRequest) map[string]any {
	out := map[string]any{}
	if req.Name != nil {
		out["name"] = *req.Name
	}
	if req.Enabled != nil {
		out["enabled"] = *req.Enabled
	}
	return out
}
