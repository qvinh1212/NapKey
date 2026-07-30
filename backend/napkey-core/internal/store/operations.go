package store

import (
	"context"
	"fmt"
	"time"
)

type OperationsSummary struct {
	RevenueMicros       int64
	UpstreamCostMicros  int64
	WalletDriftCount    int64
	WalletAbsoluteDrift int64
	UnmatchedPayments   int64
	RejectedPayments    int64
	StuckPayments       int64
	OpenHolds           int64
	ExpiredOpenHolds    int64
	PendingKeySync      int64
	FailedKeySync       int64
	OpenAlerts          int64
}

type OperationsAlert struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Severity  string         `json:"severity"`
	Title     string         `json:"title"`
	Metadata  map[string]any `json:"metadata"`
	OpenedAt  time.Time      `json:"openedAt"`
}

func (s *Store) ListOpenOperationsAlerts(ctx context.Context, limit int) ([]OperationsAlert, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, alert_type, severity, title, metadata, opened_at
		FROM operations_alerts WHERE status = 'open'
		ORDER BY CASE severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END, opened_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: listing operations alerts: %w", err)
	}
	defer rows.Close()
	alerts := make([]OperationsAlert, 0)
	for rows.Next() {
		var alert OperationsAlert
		if err := rows.Scan(&alert.ID, &alert.Type, &alert.Severity, &alert.Title, &alert.Metadata, &alert.OpenedAt); err != nil {
			return nil, fmt.Errorf("store: scanning operations alert: %w", err)
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}

func (s *Store) RefreshOperationsAlerts(ctx context.Context) error {
	type check struct {
		fingerprint string
		alertType   string
		severity    string
		title       string
		query       string
	}
	checks := []check{
		{"payment-unmatched", "payment_unmatched", "warning", "Casso payments remain unmatched", `SELECT count(*) FROM payment_events WHERE status = 'unmatched' AND received_at < now() - interval '30 minutes'`},
		{"payment-duplicate", "payment_duplicate", "warning", "Additional payment transactions require review", `SELECT count(*) FROM payment_events WHERE status = 'duplicate' AND received_at > now() - interval '24 hours'`},
		{"payment-stuck", "payment_stuck", "critical", "Payment events are stuck processing", `SELECT count(*) FROM payment_events WHERE status = 'processing' AND received_at < now() - interval '10 minutes'`},
		{"key-sync-failed", "key_sync_failed", "warning", "API keys failed to synchronize", `SELECT count(*) FROM api_keys WHERE sync_state = 'failed' AND revoked_at IS NULL`},
		{"holds-expired-open", "wallet_hold_expired", "critical", "Wallet holds are open past expiry", `SELECT count(*) FROM wallet_holds WHERE status = 'open' AND expires_at < now()`},
	}
	for _, item := range checks {
		var count int64
		if err := s.db.QueryRowContext(ctx, item.query).Scan(&count); err != nil {
			return fmt.Errorf("store: checking alert %s: %w", item.fingerprint, err)
		}
		if count > 0 {
			_, err := s.db.ExecContext(ctx, `INSERT INTO operations_alerts (alert_type, severity, fingerprint, title, metadata)
				VALUES ($1, $2, $3, $4, jsonb_build_object('count', $5))
				ON CONFLICT (fingerprint) WHERE status = 'open' DO UPDATE SET metadata = excluded.metadata`,
				item.alertType, item.severity, item.fingerprint, item.title, count)
			if err != nil {
				return fmt.Errorf("store: opening alert %s: %w", item.fingerprint, err)
			}
		} else if _, err := s.db.ExecContext(ctx, `UPDATE operations_alerts SET status = 'resolved', resolved_at = now()
			WHERE fingerprint = $1 AND status = 'open'`, item.fingerprint); err != nil {
			return fmt.Errorf("store: resolving alert %s: %w", item.fingerprint, err)
		}
	}
	return nil
}

func (s *Store) GetOperationsSummary(ctx context.Context, since time.Time) (*OperationsSummary, error) {
	var out OperationsSummary
	err := s.db.QueryRowContext(ctx, `
		WITH revenue AS (
			SELECT coalesce(sum(cost_micros), 0)::bigint AS value,
			       coalesce(sum(upstream_cost_micros), 0)::bigint AS upstream_value
			FROM usage_records WHERE created_at >= $1 AND status = 'success'
		), wallet_expected AS (
			SELECT w.user_id, w.balance_micros,
			       coalesce(sum(CASE WHEN l.kind IN ('topup','trial','promotion','usage','refund','adjustment') THEN l.amount_micros ELSE 0 END), 0)::bigint expected
			FROM wallets w LEFT JOIN ledger_entries l ON l.user_id = w.user_id GROUP BY w.user_id, w.balance_micros
		), wallet_drift AS (
			SELECT count(*) FILTER (WHERE balance_micros <> expected)::bigint drift_count,
			       coalesce(sum(abs(balance_micros - expected)), 0)::bigint absolute_drift FROM wallet_expected
		), payments AS (
			SELECT count(*) FILTER (WHERE status = 'unmatched' AND received_at < now() - interval '30 minutes')::bigint unmatched,
			       count(*) FILTER (WHERE status = 'rejected' AND received_at >= $1)::bigint rejected,
			       count(*) FILTER (WHERE status = 'processing' AND received_at < now() - interval '10 minutes')::bigint stuck
			FROM payment_events
		), holds AS (
			SELECT count(*) FILTER (WHERE status = 'open')::bigint open_count,
			       count(*) FILTER (WHERE status = 'open' AND expires_at < now())::bigint expired_count FROM wallet_holds
		), sync AS (
			SELECT count(*) FILTER (WHERE sync_state = 'pending')::bigint pending,
			       count(*) FILTER (WHERE sync_state = 'failed')::bigint failed FROM api_keys WHERE revoked_at IS NULL
		), alerts AS (SELECT count(*)::bigint open_count FROM operations_alerts WHERE status = 'open')
		SELECT r.value, r.upstream_value, wd.drift_count, wd.absolute_drift,
		       p.unmatched, p.rejected, p.stuck, h.open_count, h.expired_count,
		       s.pending, s.failed, a.open_count
		FROM revenue r CROSS JOIN wallet_drift wd CROSS JOIN payments p CROSS JOIN holds h CROSS JOIN sync s CROSS JOIN alerts a`, since).Scan(
		&out.RevenueMicros, &out.UpstreamCostMicros, &out.WalletDriftCount, &out.WalletAbsoluteDrift,
		&out.UnmatchedPayments, &out.RejectedPayments, &out.StuckPayments, &out.OpenHolds,
		&out.ExpiredOpenHolds, &out.PendingKeySync, &out.FailedKeySync, &out.OpenAlerts)
	if err != nil {
		return nil, fmt.Errorf("store: loading operations summary: %w", err)
	}
	return &out, nil
}

// ReconcileWalletBalances records drift as an incident. It never changes money.
func (s *Store) ReconcileWalletBalances(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `
		WITH expected AS (
			SELECT w.user_id, w.balance_micros,
			       coalesce(sum(CASE WHEN l.kind IN ('topup','trial','promotion','usage','refund','adjustment') THEN l.amount_micros ELSE 0 END), 0)::bigint expected
			FROM wallets w LEFT JOIN ledger_entries l ON l.user_id = w.user_id GROUP BY w.user_id, w.balance_micros
		), drift AS (SELECT count(*)::bigint count FROM expected WHERE balance_micros <> expected)
		SELECT count FROM drift`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: reconciling wallets: %w", err)
	}
	if count > 0 {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO operations_alerts (alert_type, severity, fingerprint, title, metadata)
			VALUES ('wallet_drift', 'critical', 'wallet-drift', 'Wallet balances disagree with ledger', jsonb_build_object('walletCount', $1))
			ON CONFLICT (fingerprint) WHERE status = 'open'
			DO UPDATE SET metadata = excluded.metadata`, count)
	} else {
		_, err = s.db.ExecContext(ctx, `UPDATE operations_alerts SET status = 'resolved', resolved_at = now()
			WHERE fingerprint = 'wallet-drift' AND status = 'open'`)
	}
	if err != nil {
		return count, fmt.Errorf("store: updating wallet reconciliation alert: %w", err)
	}
	return count, nil
}
