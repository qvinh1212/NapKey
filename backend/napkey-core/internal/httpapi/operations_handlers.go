package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"napkey-core/internal/reliability"
	"napkey-core/internal/store"
)

func (s *Server) handleAdminOperationsSummary(w http.ResponseWriter, r *http.Request) {
	days := 30
	if value := strings.TrimSpace(r.URL.Query().Get("days")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1 && parsed <= 365 {
			days = parsed
		}
	}
	summary, err := s.store.GetOperationsSummary(r.Context(), time.Now().AddDate(0, 0, -days))
	if err != nil {
		writeStoreError(w, err, "loading operations summary")
		return
	}
	margin := summary.RevenueMicros - summary.UpstreamCostMicros
	dataPlane, dataPlaneErr := s.plane.OperationsStatus(r.Context())
	dataPlaneView := map[string]any{"healthy": false, "error": "data plane unavailable"}
	assessment := reliability.Evaluate(true, nil, dataPlaneErr)
	if dataPlaneErr == nil {
		snapshot := &reliability.DataPlaneSnapshot{
			Accounts: dataPlane.Accounts, Available: dataPlane.Available,
			RecentRequests: dataPlane.RecentRequests, RecentFailures: dataPlane.RecentFailures,
			UsageHealthy: dataPlane.UsageReporting.Healthy,
			UsagePending: dataPlane.UsageReporting.Pending,
			UsageDropped: dataPlane.UsageReporting.Dropped,
		}
		assessment = reliability.Evaluate(true, snapshot, nil)
		dataPlaneView = map[string]any{
			"healthy": assessment.Status == reliability.StatusOperational,
			"version": dataPlane.Version, "accounts": dataPlane.Accounts, "available": dataPlane.Available,
			"totalRequests": dataPlane.TotalRequests, "successRequests": dataPlane.SuccessRequests,
			"failedRequests": dataPlane.FailedRequests, "totalTokens": dataPlane.TotalTokens,
			"uptimeSeconds": dataPlane.Uptime, "usageReporting": dataPlane.UsageReporting,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"windowDays":           days,
		"revenue":              costView(summary.RevenueMicros),
		"upstreamCostEstimate": costView(summary.UpstreamCostMicros),
		"margin":               costView(margin),
		"wallets":              map[string]any{"driftCount": summary.WalletDriftCount, "absoluteDrift": costView(summary.WalletAbsoluteDrift)},
		"payments":             map[string]int64{"unmatched": summary.UnmatchedPayments, "rejected": summary.RejectedPayments, "stuck": summary.StuckPayments},
		"holds":                map[string]int64{"open": summary.OpenHolds, "expired": summary.ExpiredOpenHolds},
		"keySync":              map[string]int64{"pending": summary.PendingKeySync, "failed": summary.FailedKeySync},
		"openAlerts":           summary.OpenAlerts,
		"dataPlane":            dataPlaneView,
		"reliability":          assessment,
		"generatedAt":          time.Now().UTC(),
	})
}

func (s *Server) handleAdminBusinessSummary(w http.ResponseWriter, r *http.Request) {
	days := 30
	if value := strings.TrimSpace(r.URL.Query().Get("days")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1 && parsed <= 365 {
			days = parsed
		}
	}
	summary, err := s.store.GetBusinessSummary(r.Context(), time.Now().AddDate(0, 0, -days))
	if err != nil {
		writeStoreError(w, err, "loading business summary")
		return
	}
	averageOrderMicros := int64(0)
	if summary.PaidOrders > 0 {
		averageOrderMicros = summary.CashCollectedMicros / summary.PaidOrders
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"windowDays": days,
		"funnel": map[string]int64{
			"newUsers": summary.NewUsers, "verifiedUsers": summary.VerifiedUsers,
			"activatedUsers": summary.ActivatedUsers, "newPayingUsers": summary.NewPayingUsers,
		},
		"customers": map[string]int64{"paying": summary.PayingCustomers, "repeat": summary.RepeatCustomers},
		"payments": map[string]any{
			"paidOrders": summary.PaidOrders, "cashCollected": costView(summary.CashCollectedMicros),
			"averageOrder": costView(averageOrderMicros),
		},
		"walletLiability": costView(summary.WalletLiabilityMicros),
		"generatedAt": time.Now().UTC(),
	})
}

func (s *Server) handleAdminReconcileWallets(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	count, err := s.store.ReconcileWalletBalances(r.Context())
	if err != nil {
		writeStoreError(w, err, "reconciling wallet balances")
		return
	}
	_ = s.store.WriteAudit(r.Context(), store.AuditEntry{ActorType: "admin", ActorID: su.User.ID,
		Action: "admin.wallet.reconcile", TargetType: "wallets", Metadata: map[string]any{"driftCount": count}, IP: clientIP(r, s.trustProxy)})
	writeJSON(w, http.StatusOK, map[string]any{"driftCount": count})
}

func (s *Server) handleAdminOperationsAlerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.store.ListOpenOperationsAlerts(r.Context(), 50)
	if err != nil {
		writeStoreError(w, err, "loading operations alerts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": alerts})
}

type setAdminRolesRequest struct {
	Roles []string `json:"roles"`
}

func (s *Server) handleAdminGetRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.store.AdminRoles(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "loading admin roles")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

func (s *Server) handleAdminSetRoles(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	var req setAdminRolesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetAdminRoles(r.Context(), r.PathValue("id"), su.User.ID, req.Roles); err != nil {
		writeStoreError(w, err, "assigning admin roles")
		return
	}
	_ = s.store.WriteAudit(r.Context(), store.AuditEntry{ActorType: "admin", ActorID: su.User.ID,
		Action: "admin.roles.set", TargetType: "user", TargetID: r.PathValue("id"), Metadata: map[string]any{"roles": req.Roles}, IP: clientIP(r, s.trustProxy)})
	writeJSON(w, http.StatusOK, map[string]any{"roles": req.Roles})
}
