package store

import (
	"context"
	"fmt"
	"time"
)

type BusinessSummary struct {
	NewUsers               int64
	VerifiedUsers          int64
	ActivatedUsers         int64
	NewPayingUsers         int64
	PayingCustomers        int64
	RepeatCustomers        int64
	PaidOrders             int64
	CashCollectedMicros   int64
	WalletLiabilityMicros int64
}

func (s *Store) GetBusinessSummary(ctx context.Context, since time.Time) (*BusinessSummary, error) {
	var out BusinessSummary
	err := s.db.QueryRowContext(ctx, `
		WITH business_accounts AS (
			SELECT count(*)::bigint AS new_users,
			       count(*) FILTER (WHERE email_verified_at IS NOT NULL)::bigint AS verified_users,
			       count(*) FILTER (WHERE EXISTS (
			           SELECT 1 FROM usage_records ur WHERE ur.user_id = users.id AND ur.created_at >= $1 AND ur.status = 'success'
			       ))::bigint AS activated_new_users,
			       count(*) FILTER (WHERE EXISTS (
			           SELECT 1 FROM topup_orders t WHERE t.user_id = users.id AND t.status = 'paid' AND t.paid_at >= $1
			       ))::bigint AS new_paying_users
			FROM users WHERE created_at >= $1
		), paid_by_customer AS (
			SELECT user_id, count(*)::bigint AS order_count,
			       coalesce(sum(received_amount_micros), 0)::bigint AS cash
			FROM topup_orders WHERE status = 'paid' AND paid_at >= $1 GROUP BY user_id
		), payments AS (
			SELECT count(*)::bigint AS paying_customers,
			       count(*) FILTER (WHERE order_count >= 2)::bigint AS repeat_customers,
			       coalesce(sum(order_count), 0)::bigint AS paid_orders,
			       coalesce(sum(cash), 0)::bigint AS cash_collected
			FROM paid_by_customer
		), liability AS (
			SELECT coalesce(sum(balance_micros), 0)::bigint AS wallet_liability FROM wallets
		)
		SELECT a.new_users, a.verified_users, a.activated_new_users, a.new_paying_users,
		       p.paying_customers, p.repeat_customers, p.paid_orders, p.cash_collected,
		       l.wallet_liability
		FROM business_accounts a CROSS JOIN payments p CROSS JOIN liability l`, since).Scan(
		&out.NewUsers, &out.VerifiedUsers, &out.ActivatedUsers, &out.NewPayingUsers,
		&out.PayingCustomers, &out.RepeatCustomers, &out.PaidOrders,
		&out.CashCollectedMicros, &out.WalletLiabilityMicros)
	if err != nil {
		return nil, fmt.Errorf("store: loading business summary: %w", err)
	}
	return &out, nil
}
