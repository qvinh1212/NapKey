package httpapi

import (
	"context"
	"net/http"
	"time"

	"napkey-core/internal/kiro"
	"napkey-core/internal/logger"
	"napkey-core/internal/reliability"
	"napkey-core/internal/store"
)

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r, 50, 200)
	users, err := s.store.ListUsers(r.Context(), limit, offset)
	if err != nil {
		writeStoreError(w, err, "listing users")
		return
	}
	total, err := s.store.CountUsers(r.Context())
	if err != nil {
		writeStoreError(w, err, "counting users")
		return
	}
	out := make([]map[string]any, 0, len(users))
	for i := range users {
		view := userView(&users[i].User, s.cfg.IsAdmin(users[i].Email))
		view["activeKeys"] = users[i].KeyCount
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users":  out,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

type setUserStatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// handleAdminSetUserStatus suspends or reactivates an account.
//
// Suspension also drops sessions and disables keys inside the same transaction, so
// a suspended user stops spending immediately rather than at session expiry.
func (s *Server) handleAdminSetUserStatus(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	targetID := r.PathValue("id")
	var req setUserStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Status != "active" && req.Status != "suspended" {
		writeFieldErrors(w, map[string]string{"status": `status must be "active" or "suspended"`})
		return
	}
	// Locking yourself out would leave the allowlist as the only way back in.
	if targetID == su.User.ID && req.Status == "suspended" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "you cannot suspend your own account")
		return
	}

	if err := s.store.SetUserStatus(r.Context(), targetID, req.Status); err != nil {
		writeStoreError(w, err, "setting user status")
		return
	}

	// Push the key changes to the data plane so a suspended user's keys stop
	// working now, not on the reconciler's next sweep.
	if req.Status == "suspended" {
		s.pushUserKeyState(r, targetID)
	}

	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "admin", ActorID: su.User.ID, Action: "admin.user_status",
		TargetType: "user", TargetID: targetID,
		Metadata: map[string]any{"status": req.Status, "reason": req.Reason},
		IP:       clientIP(r, s.trustProxy),
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": req.Status})
}

type setQuotaRequest struct {
	KeyID       string   `json:"keyId"`
	RPMLimit    *int     `json:"rpmLimit,omitempty"`
	TPMLimit    *int     `json:"tpmLimit,omitempty"`
	TokenLimit  *int64   `json:"tokenLimit,omitempty"`
	CreditLimit *float64 `json:"creditLimit,omitempty"`
}

// handleAdminSetQuota grants quota by hand, which is how Stage 2 sells access
// before the wallet exists (DESIGN.md section 9).
func (s *Server) handleAdminSetQuota(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	targetUserID := r.PathValue("id")
	var req setQuotaRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.KeyID == "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "keyId is required")
		return
	}
	if req.RPMLimit == nil && req.TPMLimit == nil && req.TokenLimit == nil && req.CreditLimit == nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "nothing to update")
		return
	}
	if req.TokenLimit != nil && *req.TokenLimit < 0 {
		writeFieldErrors(w, map[string]string{"tokenLimit": "must not be negative"})
		return
	}
	if req.RPMLimit != nil && *req.RPMLimit < 0 {
		writeFieldErrors(w, map[string]string{"rpmLimit": "must not be negative"})
		return
	}
	if req.TPMLimit != nil && *req.TPMLimit < 0 {
		writeFieldErrors(w, map[string]string{"tpmLimit": "must not be negative"})
		return
	}
	if req.CreditLimit != nil && *req.CreditLimit < 0 {
		writeFieldErrors(w, map[string]string{"creditLimit": "must not be negative"})
		return
	}

	key, err := s.store.GetAPIKeyByID(r.Context(), req.KeyID)
	if err != nil {
		writeStoreError(w, err, "loading key for quota change")
		return
	}
	// The path carries the user id, so verify the key actually belongs to them.
	// Without this an admin could raise quota on the wrong account by typo.
	if key.UserID != targetUserID {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "that key does not belong to this user")
		return
	}

	updated, err := s.store.UpdateAPIKey(r.Context(), targetUserID, req.KeyID, store.UpdateAPIKeyParams{
		RPMLimit:    req.RPMLimit,
		TPMLimit:    req.TPMLimit,
		TokenLimit:  req.TokenLimit,
		CreditLimit: req.CreditLimit,
	})
	if err != nil {
		writeStoreError(w, err, "updating quota")
		return
	}

	if updated.RemoteID != "" {
		tokenLimit := updated.TokenLimit
		creditLimit := updated.CreditLimit
		rpmLimit := intValue(updated.RPMLimit)
		tpmLimit := intValue(updated.TPMLimit)
		if err := s.kiro.UpdateKey(r.Context(), updated.RemoteID, kiro.UpdateKeyRequest{
			RPMLimit:    &rpmLimit,
			TPMLimit:    &tpmLimit,
			TokenLimit:  &tokenLimit,
			CreditLimit: &creditLimit,
		}); err != nil {
			logger.Warnf("pushing quota for key %s failed, queued for retry: %v", updated.ID, err)
			if markErr := s.store.MarkKeySyncFailed(r.Context(), updated.ID, err.Error(), false); markErr != nil {
				logger.Errorf("recording sync failure for key %s: %v", updated.ID, markErr)
			}
		} else if err := s.store.MarkKeySynced(r.Context(), updated.ID, updated.RemoteID); err != nil {
			logger.Warnf("marking key %s synced failed: %v", updated.ID, err)
		}
	}

	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "admin", ActorID: su.User.ID, Action: "admin.set_quota",
		TargetType: "api_key", TargetID: req.KeyID,
		Metadata: map[string]any{
			"rpmLimit":    updated.RPMLimit,
			"tpmLimit":    updated.TPMLimit,
			"tokenLimit":  updated.TokenLimit,
			"creditLimit": updated.CreditLimit,
			"userId":      targetUserID,
		},
		IP: clientIP(r, s.trustProxy),
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": toKeyView(updated)})
}

func (s *Server) handleAdminAuditLog(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r, 50, 200)
	entries, err := s.store.ListAuditLogs(r.Context(), r.URL.Query().Get("actorId"), limit, offset)
	if err != nil {
		writeStoreError(w, err, "listing audit logs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"limit":   limit,
		"offset":  offset,
	})
}

// handleAdminSyncDrift lists keys present in the data plane with no owning row.
//
// Those are unattributable: they authenticate, spend upstream quota, and there is
// nobody to bill. Nothing is deleted automatically, because a wrong automatic
// delete would cut off a paying customer.
func (s *Server) handleAdminSyncDrift(w http.ResponseWriter, r *http.Request) {
	syncer := kiro.NewSyncer(s.store, s.kiro, time.Minute)
	orphans, err := syncer.DetectDrift(r.Context())
	if err != nil {
		logger.Errorf("detecting sync drift failed: %v", err)
		writeError(w, http.StatusBadGateway, codeUpstreamFailure, "could not reach the data plane")
		return
	}
	pending, err := s.store.CountPendingSyncs(r.Context())
	if err != nil {
		writeStoreError(w, err, "counting pending syncs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"orphansInDataPlane": orphans,
		"pendingSyncs":       pending,
		"note":               "orphan keys authenticate but have no owner in napkey-core; investigate before deleting",
	})
}

// pushUserKeyState pushes the current enabled state of a user's keys to kiro-go.
func (s *Server) pushUserKeyState(r *http.Request, userID string) {
	keys, err := s.store.ListAPIKeys(r.Context(), userID)
	if err != nil {
		logger.Errorf("listing keys for user %s failed: %v", userID, err)
		return
	}
	for i := range keys {
		k := &keys[i]
		if k.RemoteID == "" || k.RevokedAt != nil {
			continue
		}
		enabled := k.Enabled
		if err := s.kiro.UpdateKey(r.Context(), k.RemoteID, kiro.UpdateKeyRequest{Enabled: &enabled}); err != nil {
			logger.Warnf("pushing state for key %s failed, queued for retry: %v", k.ID, err)
			if markErr := s.store.MarkKeySyncFailed(r.Context(), k.ID, err.Error(), false); markErr != nil {
				logger.Errorf("recording sync failure for key %s: %v", k.ID, markErr)
			}
			continue
		}
		if err := s.store.MarkKeySynced(r.Context(), k.ID, k.RemoteID); err != nil {
			logger.Warnf("marking key %s synced failed: %v", k.ID, err)
		}
	}
}

// handleHealth is a liveness probe: is the process up.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"uptime": time.Since(s.startedAt).Round(time.Second).String(),
	})
}

// handleReady is a readiness probe: can the process actually serve.
//
// Postgres being reachable is a hard requirement, so a failure here is 503 and the
// orchestrator should stop sending traffic. The data plane being unreachable is
// reported but does not fail readiness: existing keys keep working, and only new
// key creation is affected.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	checks := map[string]any{}
	ready := true

	if err := s.store.DB().PingContext(ctx); err != nil {
		checks["postgres"] = "unreachable: " + err.Error()
		ready = false
	} else {
		checks["postgres"] = "ok"
	}

	if err := s.kiro.Health(ctx); err != nil {
		checks["dataPlane"] = "unreachable: " + err.Error()
	} else {
		checks["dataPlane"] = "ok"
	}

	if pending, err := s.store.CountPendingSyncs(ctx); err == nil {
		checks["pendingKeySyncs"] = pending
	}

	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"ready": ready, "checks": checks})
}

func (s *Server) handlePublicStatus(w http.ResponseWriter, r *http.Request) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if time.Now().Before(s.publicStatusUntil) && s.publicStatusCache != nil {
		writeJSON(w, http.StatusOK, s.publicStatusCache)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	postgresOK := s.store.DB().PingContext(ctx) == nil
	dataPlane, dataPlaneErr := s.kiro.OperationsStatus(ctx)
	var snapshot *reliability.DataPlaneSnapshot
	if dataPlane != nil {
		snapshot = &reliability.DataPlaneSnapshot{
			Accounts: dataPlane.Accounts, Available: dataPlane.Available,
			RecentRequests: dataPlane.RecentRequests, RecentFailures: dataPlane.RecentFailures,
			UsageHealthy: dataPlane.UsageReporting.Healthy,
			UsagePending: dataPlane.UsageReporting.Pending,
			UsageDropped: dataPlane.UsageReporting.Dropped,
		}
	}
	assessment := reliability.Evaluate(postgresOK, snapshot, dataPlaneErr)
	componentStatus := func(codes ...string) reliability.Status {
		status := reliability.StatusOperational
		for _, issue := range assessment.Issues {
			for _, code := range codes {
				if issue.Code == code && (issue.Severity == reliability.StatusOutage || status == reliability.StatusOperational) {
					status = issue.Severity
				}
			}
		}
		return status
	}
	payload := map[string]any{
		"status": assessment.Status,
		"components": []map[string]any{
			{"id": "gateway", "status": componentStatus("data_plane_unreachable", "upstream_capacity_empty", "upstream_capacity_low", "error_rate_high")},
			{"id": "billing", "status": componentStatus("postgres_unreachable")},
			{"id": "usage", "status": componentStatus("usage_reporting_unhealthy", "usage_reports_dropped", "usage_backlog_high")},
		},
		"checkedAt": time.Now().UTC().Format(time.RFC3339),
	}
	s.publicStatusCache = payload
	s.publicStatusUntil = time.Now().Add(15 * time.Second)
	writeJSON(w, http.StatusOK, payload)
}
