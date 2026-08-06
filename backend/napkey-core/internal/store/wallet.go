package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"napkey-core/internal/pgwire"
	"napkey-core/internal/pricing"
)

const (
	MinTopupVND     int64 = 10_000
	TopupStepVND    int64 = 1_000
	MinTopupMicros  int64 = MinTopupVND * pricing.MicrosPerVND
	TopupStepMicros int64 = TopupStepVND * pricing.MicrosPerVND
)

const (
	FirstTopupBonusMinimumMicros int64 = 75_000 * pricing.MicrosPerVND
	FirstTopupBonusCapMicros     int64 = 75_000 * pricing.MicrosPerVND
)

func firstTopupBonusMicros(paidMicros, purchasedMicros int64) int64 {
	return 0
}

type promotionalGrant struct {
	kind      string
	remaining int64
	expiresAt time.Time
}

func consumePromotionalGrantsTx(ctx context.Context, tx *sql.Tx, userID string, amount int64, expiredOnly bool) error {
	if amount <= 0 {
		return refreshPromotionalExpiryTx(ctx, tx, userID)
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind,remaining_micros,expires_at FROM (SELECT 'trial' kind,remaining_micros,expires_at FROM trial_grants WHERE user_id=$1 UNION ALL SELECT 'first_topup_bonus',remaining_micros,expires_at FROM first_topup_bonus_grants WHERE user_id=$1) grants WHERE remaining_micros>0 AND (NOT $2 OR expires_at<=now()) ORDER BY expires_at`, userID, expiredOnly)
	if err != nil {
		return err
	}
	var grants []promotionalGrant
	for rows.Next() {
		var grant promotionalGrant
		if err := rows.Scan(&grant.kind, &grant.remaining, &grant.expiresAt); err != nil {
			rows.Close()
			return err
		}
		grants = append(grants, grant)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	left := amount
	for _, grant := range grants {
		if left == 0 {
			break
		}
		used := min(left, grant.remaining)
		if grant.kind == "trial" {
			_, err = tx.ExecContext(ctx, `UPDATE trial_grants SET remaining_micros=remaining_micros-$2 WHERE user_id=$1`, userID, used)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE first_topup_bonus_grants SET remaining_micros=remaining_micros-$2 WHERE user_id=$1`, userID, used)
		}
		if err != nil {
			return err
		}
		left -= used
	}
	if left != 0 {
		return fmt.Errorf("store: promotional grant balance drifted by %d micros", left)
	}
	return refreshPromotionalExpiryTx(ctx, tx, userID)
}

func refreshPromotionalExpiryTx(ctx context.Context, tx *sql.Tx, userID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE wallets SET promotional_expires_at=(SELECT min(expires_at) FROM (SELECT expires_at FROM trial_grants WHERE user_id=$1 AND remaining_micros>0 UNION ALL SELECT expires_at FROM first_topup_bonus_grants WHERE user_id=$1 AND remaining_micros>0) active) WHERE user_id=$1`, userID)
	return err
}

func grantFirstTopupBonusTx(ctx context.Context, tx *sql.Tx, userID, orderID string, paidMicros, purchasedMicros int64, paidAt time.Time) error {
	bonus := firstTopupBonusMicros(paidMicros, purchasedMicros)
	if bonus == 0 {
		return nil
	}
	var already bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM topup_orders WHERE user_id=$1 AND status='paid' AND received_amount_micros >= $3 AND id<>$2)`, userID, orderID, FirstTopupBonusMinimumMicros).Scan(&already); err != nil {
		return err
	}
	if already {
		return nil
	}
	expires := paidAt.Add(30 * 24 * time.Hour)
	var grantID string
	err := tx.QueryRowContext(ctx, `INSERT INTO first_topup_bonus_grants(user_id,topup_order_id,amount_micros,remaining_micros,expires_at) VALUES($1,$2,$3,$3,$4) ON CONFLICT DO NOTHING RETURNING id`, userID, orderID, bonus, expires).Scan(&grantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var newBalance, newHeld int64
	if err := tx.QueryRowContext(ctx, `UPDATE wallets SET balance_micros=balance_micros+$2,promotional_micros=promotional_micros+$2,updated_at=now() WHERE user_id=$1 RETURNING balance_micros,held_micros`, userID, bonus).Scan(&newBalance, &newHeld); err != nil {
		return err
	}
	if err := refreshPromotionalExpiryTx(ctx, tx, userID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ledger_entries(user_id,kind,amount_micros,balance_after_micros,held_after_micros,ref_type,ref_id,idempotency_key,metadata) VALUES($1,'promotion',$2,$3,$4,'first_topup_bonus',$5,'first-topup-bonus:'||$5,jsonb_build_object('expiresAt',$6))`, userID, bonus, newBalance, newHeld, grantID, expires)
	return err
}

const (
	TopupPending   = "pending"
	TopupPaid      = "paid"
	TopupUnderpaid = "underpaid"
)

type Wallet struct {
	UserID                                       string
	BalanceMicros, HeldMicros, PromotionalMicros int64
	PromotionalExpiresAt                         *time.Time
	Currency                                     string
	UpdatedAt                                    time.Time
}
type WalletHold struct {
	ID, RequestID string
	AmountMicros  int64
	ExpiresAt     time.Time
}
type TopupOrder struct {
	ID, UserID, MemoCode, BankAccountNumber, Status      string
	Provider, ProviderPaymentLinkID, CheckoutURL, QRCode string
	ProviderOrderCode                                    int64
	ExpectedAmountMicros, ReceivedAmountMicros           int64
	RetailVNDPerCredit                                   int64
	ExpiresAt                                            time.Time
	PaidAt                                               *time.Time
	CreatedAt                                            time.Time
}

func ValidateTopupAmount(amount int64) error {
	if amount < MinTopupMicros {
		return errors.New("store: top-up must be at least 10,000 VND")
	}
	if amount%TopupStepMicros != 0 {
		return errors.New("store: top-up must use 1,000 VND increments")
	}
	return nil
}

func topupStatus(expected, received int64) string {
	if received < expected {
		return TopupUnderpaid
	}
	return TopupPaid
}

func walletCreditMicros(amountMicros, orderVNDPerCredit int64) (int64, error) {
	if amountMicros < 0 || orderVNDPerCredit <= 0 {
		return 0, errors.New("store: invalid top-up credit rate")
	}
	whole := amountMicros / orderVNDPerCredit
	remainder := amountMicros % orderVNDPerCredit
	if whole > math.MaxInt64/pricing.RetailVNDPerCredit || remainder > math.MaxInt64/pricing.RetailVNDPerCredit {
		return 0, errors.New("store: top-up credit value overflows")
	}
	base := whole * pricing.RetailVNDPerCredit
	fraction := remainder * pricing.RetailVNDPerCredit / orderVNDPerCredit
	if base > math.MaxInt64-fraction {
		return 0, errors.New("store: top-up credit value overflows")
	}
	return base + fraction, nil
}

func (s *Store) GetWallet(ctx context.Context, userID string) (*Wallet, error) {
	var w Wallet
	err := s.withTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO wallets(user_id) VALUES($1) ON CONFLICT DO NOTHING`, userID); err != nil {
			return err
		}
		if err := expirePromotionalWalletTx(ctx, tx, userID); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT user_id, balance_micros, held_micros, promotional_micros, promotional_expires_at, currency, updated_at FROM wallets WHERE user_id=$1`, userID).Scan(&w.UserID, &w.BalanceMicros, &w.HeldMicros, &w.PromotionalMicros, &w.PromotionalExpiresAt, &w.Currency, &w.UpdatedAt)
	})
	if err != nil {
		return nil, fmt.Errorf("store: loading wallet: %w", err)
	}
	return &w, nil
}

// expirePromotionalWalletTx removes expired promotional funds that are not
// protecting an open hold. A held portion remains until that request settles or
// releases, at which point this helper runs again.
func expirePromotionalWalletTx(ctx context.Context, tx *sql.Tx, userID string) error {
	var expired, balance, held int64
	err := tx.QueryRowContext(ctx, `
		WITH current AS (
			SELECT user_id, promotional_micros,
			       least(promotional_micros, greatest(balance_micros-held_micros, 0), coalesce((
			           SELECT sum(remaining_micros) FROM (
			               SELECT remaining_micros FROM trial_grants WHERE user_id=$1 AND remaining_micros>0 AND expires_at<=now()
			               UNION ALL
			               SELECT remaining_micros FROM first_topup_bonus_grants WHERE user_id=$1 AND remaining_micros>0 AND expires_at<=now()
			           ) expired_grants
			       ),0)) AS removable
			FROM wallets
			WHERE user_id=$1 AND promotional_micros>0 AND promotional_expires_at<=now()
			FOR UPDATE
		), updated AS (
			UPDATE wallets w
			SET balance_micros=w.balance_micros-c.removable,
			    promotional_micros=w.promotional_micros-c.removable,
			    promotional_expires_at=CASE WHEN w.promotional_micros-c.removable=0 THEN NULL ELSE w.promotional_expires_at END,
			    updated_at=now()
			FROM current c WHERE w.user_id=c.user_id
			RETURNING c.removable, w.balance_micros, w.held_micros
		)
		SELECT removable,balance_micros,held_micros FROM updated`, userID).Scan(&expired, &balance, &held)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if expired == 0 {
		return nil
	}
	if err = consumePromotionalGrantsTx(ctx, tx, userID, expired, true); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ledger_entries(user_id,kind,amount_micros,balance_after_micros,held_after_micros,ref_type,ref_id,idempotency_key,metadata)
		VALUES($1,'adjustment',$2,$3,$4,'promotional_expiry',$1::text,'promotional-expiry:'||gen_random_uuid()::text,jsonb_build_object('reason','promotional_credit_expired'))`,
		userID, -expired, balance, held)
	return err
}

// ExpirePromotionalCredits sweeps dormant wallets so expired trial liability is
// removed even when the owner never opens the console or sends another request.
func (s *Store) ExpirePromotionalCredits(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM wallets WHERE promotional_micros>0 AND promotional_expires_at<=now() ORDER BY promotional_expires_at LIMIT $1`, limit)
	if err != nil {
		return 0, fmt.Errorf("store: listing expired promotional wallets: %w", err)
	}
	var userIDs []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: scanning expired promotional wallet: %w", err)
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("store: closing expired promotional wallets: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: iterating expired promotional wallets: %w", err)
	}
	expired := 0
	for _, userID := range userIDs {
		removed := false
		err := s.withTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *sql.Tx) error {
			var before, after int64
			if err := tx.QueryRowContext(ctx, `SELECT promotional_micros FROM wallets WHERE user_id=$1`, userID).Scan(&before); err != nil {
				return err
			}
			if err := expirePromotionalWalletTx(ctx, tx, userID); err != nil {
				return err
			}
			if err := tx.QueryRowContext(ctx, `SELECT promotional_micros FROM wallets WHERE user_id=$1`, userID).Scan(&after); err != nil {
				return err
			}
			removed = after < before
			return nil
		})
		if err != nil {
			return expired, fmt.Errorf("store: expiring promotional wallet: %w", err)
		}
		if removed {
			expired++
		}
	}
	return expired, nil
}

func (s *Store) ReserveWallet(ctx context.Context, userID, keyID, requestID string, amountMicros int64) (*WalletHold, error) {
	if amountMicros <= 0 {
		return nil, errors.New("store: hold amount must be positive")
	}
	var hold WalletHold
	err := s.withTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT id,request_id,amount_micros,expires_at FROM wallet_holds WHERE request_id=$1`, requestID).Scan(&hold.ID, &hold.RequestID, &hold.AmountMicros, &hold.ExpiresAt)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO wallets(user_id) VALUES($1) ON CONFLICT DO NOTHING`, userID); err != nil {
			return err
		}
		if err := expirePromotionalWalletTx(ctx, tx, userID); err != nil {
			return err
		}
		var balance, held int64
		err = tx.QueryRowContext(ctx, `UPDATE wallets SET held_micros=held_micros+$2,updated_at=now() WHERE user_id=$1 AND balance_micros-held_micros >= $2 RETURNING balance_micros,held_micros`, userID, amountMicros).Scan(&balance, &held)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInsufficientFunds
		}
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO wallet_holds(user_id,api_key_id,request_id,amount_micros,expires_at) VALUES($1,$2,$3,$4,now()+interval '15 minutes') ON CONFLICT(request_id) DO UPDATE SET request_id=EXCLUDED.request_id RETURNING id,request_id,amount_micros,expires_at`, userID, keyID, requestID, amountMicros).Scan(&hold.ID, &hold.RequestID, &hold.AmountMicros, &hold.ExpiresAt)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ledger_entries(user_id,kind,amount_micros,balance_after_micros,held_after_micros,ref_type,ref_id,idempotency_key) VALUES($1,'hold',$2,$3,$4,'request',$5,$6) ON CONFLICT(idempotency_key) DO NOTHING`, userID, -amountMicros, balance, held, requestID, "hold:"+requestID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &hold, nil
}

func (s *Store) ReserveWalletForKey(ctx context.Context, keyID, requestID string, amountMicros int64) (*WalletHold, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM api_keys WHERE id=$1 AND revoked_at IS NULL AND enabled=true`, keyID).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: resolving wallet owner: %w", err)
	}
	return s.ReserveWallet(ctx, userID, keyID, requestID, amountMicros)
}

func (s *Store) ReleaseWallet(ctx context.Context, requestID string) error {
	return s.withTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *sql.Tx) error {
		var holdID, userID, status string
		var amount int64
		err := tx.QueryRowContext(ctx, `SELECT id,user_id,amount_micros,status FROM wallet_holds WHERE request_id=$1 FOR UPDATE`, requestID).Scan(&holdID, &userID, &amount, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != "open" {
			return nil
		}
		var balance, held int64
		err = tx.QueryRowContext(ctx, `UPDATE wallets SET held_micros=held_micros-$2,updated_at=now() WHERE user_id=$1 AND held_micros >= $2 RETURNING balance_micros,held_micros`, userID, amount).Scan(&balance, &held)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE wallet_holds SET status='released',settled_at=now() WHERE id=$1`, holdID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ledger_entries(user_id,kind,amount_micros,balance_after_micros,held_after_micros,ref_type,ref_id,idempotency_key) VALUES($1,'hold_release',$2,$3,$4,'request',$5,$6) ON CONFLICT DO NOTHING`, userID, amount, balance, held, requestID, "release:"+requestID); err != nil {
			return err
		}
		return expirePromotionalWalletTx(ctx, tx, userID)
	})
}

func (s *Store) SettleWallet(ctx context.Context, requestID string, actualMicros int64) error {
	if actualMicros < 0 {
		return errors.New("store: settlement cannot be negative")
	}
	return s.withTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *sql.Tx) error {
		return settleWalletTx(ctx, tx, requestID, actualMicros, false)
	})
}

func settleWalletTx(ctx context.Context, tx *sql.Tx, requestID string, actualMicros int64, allowMissing bool) error {
	var holdID, userID string
	var reserved int64
	err := tx.QueryRowContext(ctx, `SELECT id,user_id,amount_micros FROM wallet_holds WHERE request_id=$1 AND status='open' FOR UPDATE`, requestID).Scan(&holdID, &userID, &reserved)
	if errors.Is(err, sql.ErrNoRows) {
		if allowMissing {
			return nil
		}
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var balance, held, promotionalBefore int64
	if err = tx.QueryRowContext(ctx, `SELECT promotional_micros FROM wallets WHERE user_id=$1 FOR UPDATE`, userID).Scan(&promotionalBefore); err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `UPDATE wallets SET balance_micros=balance_micros-$2,held_micros=held_micros-$3,promotional_micros=promotional_micros-least(promotional_micros,$2),promotional_expires_at=CASE WHEN promotional_micros-least(promotional_micros,$2)=0 THEN NULL ELSE promotional_expires_at END,updated_at=now() WHERE user_id=$1 AND balance_micros >= $2 AND held_micros >= $3 RETURNING balance_micros,held_micros`, userID, actualMicros, reserved).Scan(&balance, &held)
	if err != nil {
		return err
	}
	if err = consumePromotionalGrantsTx(ctx, tx, userID, min(promotionalBefore, actualMicros), false); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ledger_entries(user_id,kind,amount_micros,balance_after_micros,held_after_micros,ref_type,ref_id,idempotency_key) VALUES($1,'usage',$2,$3,$4,'request',$5,$6)`, userID, -actualMicros, balance, held, requestID, "settle:"+requestID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE wallet_holds SET status='settled',settled_at=now() WHERE id=$1`, holdID)
	return err
}

func (s *Store) ReleaseExpiredHolds(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	released := 0
	for released < limit {
		err := s.withTx(ctx, nil, func(tx *sql.Tx) error {
			var id, userID, requestID string
			var amount int64
			err := tx.QueryRowContext(ctx, `SELECT id,user_id,request_id,amount_micros FROM wallet_holds WHERE status='open' AND expires_at<now() ORDER BY expires_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &userID, &requestID, &amount)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			var balance, held int64
			err = tx.QueryRowContext(ctx, `UPDATE wallets SET held_micros=held_micros-$2,updated_at=now() WHERE user_id=$1 RETURNING balance_micros,held_micros`, userID, amount).Scan(&balance, &held)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE wallet_holds SET status='expired',settled_at=now() WHERE id=$1`, id); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO ledger_entries(user_id,kind,amount_micros,balance_after_micros,held_after_micros,ref_type,ref_id,idempotency_key) VALUES($1,'hold_release',$2,$3,$4,'request',$5,$6) ON CONFLICT DO NOTHING`, userID, amount, balance, held, requestID, "expire:"+requestID); err != nil {
				return err
			}
			return expirePromotionalWalletTx(ctx, tx, userID)
		})
		if errors.Is(err, ErrNotFound) {
			break
		}
		if err != nil {
			return released, err
		}
		released++
	}
	return released, nil
}

func (s *Store) CreateTopupOrder(ctx context.Context, userID string, amount int64) (*TopupOrder, error) {
	if err := ValidateTopupAmount(amount); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		memo, err := newMemoCode()
		if err != nil {
			return nil, err
		}
		orderCode, err := newProviderOrderCode()
		if err != nil {
			return nil, err
		}
		var o TopupOrder
		err = s.db.QueryRowContext(ctx, `INSERT INTO topup_orders(user_id,memo_code,expected_amount_micros,provider,provider_order_code,bank_account_number,expires_at) VALUES($1,$2,$3,'payos',$4,NULL,now()+interval '15 minutes') RETURNING id,user_id,memo_code,provider,provider_order_code,coalesce(bank_account_number,''),status,expected_amount_micros,received_amount_micros,retail_vnd_per_credit,expires_at,paid_at,created_at`, userID, memo, amount, orderCode).Scan(&o.ID, &o.UserID, &o.MemoCode, &o.Provider, &o.ProviderOrderCode, &o.BankAccountNumber, &o.Status, &o.ExpectedAmountMicros, &o.ReceivedAmountMicros, &o.RetailVNDPerCredit, &o.ExpiresAt, &o.PaidAt, &o.CreatedAt)
		if err == nil {
			return &o, nil
		}
		if !pgwire.IsUniqueViolation(err) {
			return nil, fmt.Errorf("store: creating top-up order: %w", err)
		}
	}
	return nil, errors.New("store: could not allocate a unique transfer memo")
}

func (s *Store) AttachPayOSCheckout(ctx context.Context, userID, orderID, paymentLinkID, checkoutURL, qrCode string) (*TopupOrder, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE topup_orders SET provider_payment_link_id=$3,checkout_url=$4,qr_code=$5 WHERE id=$1 AND user_id=$2 AND provider='payos' AND status='pending'`, orderID, userID, paymentLinkID, checkoutURL, qrCode)
	if err != nil {
		return nil, fmt.Errorf("store: attaching PayOS checkout: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return nil, fmt.Errorf("store: checking attached PayOS checkout: %w", err)
	} else if rows != 1 {
		return nil, ErrNotFound
	}
	return s.GetTopupOrder(ctx, userID, orderID)
}

func (s *Store) CancelTopupOrder(ctx context.Context, userID, orderID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE topup_orders SET status='cancelled' WHERE id=$1 AND user_id=$2 AND status='pending'`, orderID, userID)
	if err != nil {
		return fmt.Errorf("store: cancelling top-up order: %w", err)
	}
	return nil
}

func (s *Store) GetTopupOrder(ctx context.Context, userID, id string) (*TopupOrder, error) {
	if _, err := s.db.ExecContext(ctx, `UPDATE topup_orders SET status='expired' WHERE id=$1 AND user_id=$2 AND status='pending' AND expires_at<now()`, id, userID); err != nil {
		return nil, fmt.Errorf("store: expiring top-up order: %w", err)
	}
	var o TopupOrder
	err := s.db.QueryRowContext(ctx, `SELECT id,user_id,memo_code,provider,coalesce(provider_order_code,0),coalesce(provider_payment_link_id,''),coalesce(checkout_url,''),coalesce(qr_code,''),coalesce(bank_account_number,''),status,expected_amount_micros,received_amount_micros,retail_vnd_per_credit,expires_at,paid_at,created_at FROM topup_orders WHERE id=$1 AND user_id=$2`, id, userID).Scan(&o.ID, &o.UserID, &o.MemoCode, &o.Provider, &o.ProviderOrderCode, &o.ProviderPaymentLinkID, &o.CheckoutURL, &o.QRCode, &o.BankAccountNumber, &o.Status, &o.ExpectedAmountMicros, &o.ReceivedAmountMicros, &o.RetailVNDPerCredit, &o.ExpiresAt, &o.PaidAt, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: loading top-up order: %w", err)
	}
	return &o, nil
}

func (s *Store) ListTopupOrders(ctx context.Context, userID string, limit int) ([]TopupOrder, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE topup_orders SET status='expired' WHERE user_id=$1 AND status='pending' AND expires_at<now()`, userID); err != nil {
		return nil, fmt.Errorf("store: expiring top-up orders: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,memo_code,provider,coalesce(provider_order_code,0),coalesce(provider_payment_link_id,''),coalesce(checkout_url,''),coalesce(qr_code,''),coalesce(bank_account_number,''),status,expected_amount_micros,received_amount_micros,retail_vnd_per_credit,expires_at,paid_at,created_at FROM topup_orders WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: listing top-up orders: %w", err)
	}
	defer rows.Close()
	orders := make([]TopupOrder, 0)
	for rows.Next() {
		var o TopupOrder
		if err := rows.Scan(&o.ID, &o.UserID, &o.MemoCode, &o.Provider, &o.ProviderOrderCode, &o.ProviderPaymentLinkID, &o.CheckoutURL, &o.QRCode, &o.BankAccountNumber, &o.Status, &o.ExpectedAmountMicros, &o.ReceivedAmountMicros, &o.RetailVNDPerCredit, &o.ExpiresAt, &o.PaidAt, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scanning top-up order: %w", err)
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: listing top-up orders: %w", err)
	}
	return orders, nil
}

func newProviderOrderCode() (int64, error) {
	const min int64 = 100_000_000_000
	n, err := rand.Int(rand.Reader, big.NewInt(900_000_000_000))
	if err != nil {
		return 0, err
	}
	return min + n.Int64(), nil
}

type PayOSPaymentInput struct {
	ProviderTxID            string
	OrderCode, AmountMicros int64
	Payload                 json.RawMessage
}

func (s *Store) CreditPayOSPayment(ctx context.Context, in PayOSPaymentInput) (bool, error) {
	if in.ProviderTxID == "" || in.OrderCode <= 0 || in.AmountMicros <= 0 {
		return false, errors.New("store: invalid PayOS payment")
	}
	duplicate := false
	err := s.withTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *sql.Tx) error {
		var eventID int64
		err := tx.QueryRowContext(ctx, `INSERT INTO payment_events(provider,provider_tx_id,signature_verified,payload,status) VALUES('payos',$1,true,$2,'processing') ON CONFLICT(provider,provider_tx_id) DO NOTHING RETURNING id`, in.ProviderTxID, []byte(in.Payload)).Scan(&eventID)
		if errors.Is(err, sql.ErrNoRows) {
			duplicate = true
			return nil
		}
		if err != nil {
			return err
		}
		var order TopupOrder
		err = tx.QueryRowContext(ctx, `SELECT id,user_id,status,expected_amount_micros,retail_vnd_per_credit FROM topup_orders WHERE provider='payos' AND provider_order_code=$1 FOR UPDATE`, in.OrderCode).Scan(&order.ID, &order.UserID, &order.Status, &order.ExpectedAmountMicros, &order.RetailVNDPerCredit)
		if errors.Is(err, sql.ErrNoRows) {
			_, _ = tx.ExecContext(ctx, `UPDATE payment_events SET status='unmatched',error_message='PayOS order was not found',processed_at=now() WHERE id=$1`, eventID)
			return nil
		}
		if err != nil {
			return err
		}
		if order.Status == TopupPaid {
			duplicate = true
			_, _ = tx.ExecContext(ctx, `UPDATE payment_events SET status='duplicate',matched_order_id=$2,error_message='additional PayOS transaction received for an already paid order; review for refund',processed_at=now() WHERE id=$1`, eventID, order.ID)
			return nil
		}
		if order.Status != TopupPending {
			_, _ = tx.ExecContext(ctx, `UPDATE payment_events SET status='rejected',matched_order_id=$2,error_message='top-up order is not pending',processed_at=now() WHERE id=$1`, eventID, order.ID)
			return nil
		}
		if in.AmountMicros != order.ExpectedAmountMicros {
			_, _ = tx.ExecContext(ctx, `UPDATE payment_events SET status='rejected',matched_order_id=$2,error_message='amount does not match the PayOS order',processed_at=now() WHERE id=$1`, eventID, order.ID)
			return nil
		}
		creditedMicros, err := walletCreditMicros(in.AmountMicros, order.RetailVNDPerCredit)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO wallets(user_id) VALUES($1) ON CONFLICT DO NOTHING`, order.UserID); err != nil {
			return err
		}
		var balance, held int64
		err = tx.QueryRowContext(ctx, `UPDATE wallets SET balance_micros=balance_micros+$2,updated_at=now() WHERE user_id=$1 RETURNING balance_micros,held_micros`, order.UserID, creditedMicros).Scan(&balance, &held)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ledger_entries(user_id,kind,amount_micros,balance_after_micros,held_after_micros,ref_type,ref_id,idempotency_key) VALUES($1,'topup',$2,$3,$4,'payment_event',$5,$6)`, order.UserID, creditedMicros, balance, held, in.ProviderTxID, "payos:"+in.ProviderTxID)
		if err != nil {
			return err
		}
		paidAt := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `UPDATE topup_orders SET received_amount_micros=$2,status='paid',paid_at=$3 WHERE id=$1`, order.ID, in.AmountMicros, paidAt)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE payment_events SET status='credited',matched_order_id=$2,processed_at=now() WHERE id=$1`, eventID, order.ID)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("store: crediting PayOS payment: %w", err)
	}
	return duplicate, nil
}

func newMemoCode() (string, error) {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	out := make([]byte, 8)
	copy(out, "NK")
	for i := 2; i < len(out); i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}

type PaymentEventInput struct {
	ProviderTxID, BankReference string
	SignatureVerified           bool
	Payload                     json.RawMessage
	Status, ErrorMessage        string
}

type PaymentEvent struct {
	ID           int64
	ProviderTxID string
	Payload      json.RawMessage
}

func (s *Store) InsertPaymentEvent(ctx context.Context, in PaymentEventInput) (int64, bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO payment_events(provider,provider_tx_id,bank_reference,signature_verified,payload,status,error_message) VALUES('casso',$1,$2,$3,$4,$5,$6) ON CONFLICT(provider,provider_tx_id) DO NOTHING RETURNING id`, in.ProviderTxID, in.BankReference, in.SignatureVerified, []byte(in.Payload), in.Status, in.ErrorMessage).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, true, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("store: inserting payment event: %w", err)
	}
	return id, false, nil
}

func (s *Store) ClaimPaymentEvent(ctx context.Context) (*PaymentEvent, error) {
	var event PaymentEvent
	err := s.withTx(ctx, nil, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT id,provider_tx_id,payload FROM payment_events WHERE status='received' OR (status='processing' AND processing_started_at < now()-interval '5 minutes') ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&event.ID, &event.ProviderTxID, &event.Payload)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE payment_events SET status='processing',processing_started_at=now(),error_message=NULL WHERE id=$1`, event.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (s *Store) RejectPaymentEvent(ctx context.Context, eventID int64, status, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE payment_events SET status=$2,error_message=$3,processed_at=now() WHERE id=$1 AND status='processing'`, eventID, status, message)
	if err != nil {
		return fmt.Errorf("store: rejecting payment event: %w", err)
	}
	return nil
}

func (s *Store) CountStaleUnmatchedPayments(ctx context.Context, olderThan time.Duration) (int, error) {
	if olderThan <= 0 {
		olderThan = 30 * time.Minute
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*)::integer FROM payment_events WHERE status='unmatched' AND received_at < now()-($1 * interval '1 second')`, int64(olderThan/time.Second)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: counting unmatched payments: %w", err)
	}
	return count, nil
}

// CreditPaymentEvent atomically credits the real received amount and closes the event.
func (s *Store) CreditPaymentEvent(ctx context.Context, eventID int64, providerTxID, memo string, amountMicros int64) error {
	if amountMicros <= 0 {
		return errors.New("store: incoming payment amount must be positive")
	}
	return s.withTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *sql.Tx) error {
		var eventStatus string
		err := tx.QueryRowContext(ctx, `SELECT status FROM payment_events WHERE id=$1 FOR UPDATE`, eventID).Scan(&eventStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if eventStatus == "credited" {
			return nil
		}
		if eventStatus != "processing" {
			return fmt.Errorf("store: payment event is %s, not processing", eventStatus)
		}
		var order TopupOrder
		err = tx.QueryRowContext(ctx, `SELECT id,user_id,memo_code,expected_amount_micros,received_amount_micros,retail_vnd_per_credit FROM topup_orders WHERE memo_code=$1 FOR UPDATE`, memo).Scan(&order.ID, &order.UserID, &order.MemoCode, &order.ExpectedAmountMicros, &order.ReceivedAmountMicros, &order.RetailVNDPerCredit)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		creditedMicros, err := walletCreditMicros(amountMicros, order.RetailVNDPerCredit)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO wallets(user_id) VALUES($1) ON CONFLICT DO NOTHING`, order.UserID); err != nil {
			return err
		}
		var balance, held int64
		err = tx.QueryRowContext(ctx, `UPDATE wallets SET balance_micros=balance_micros+$2,updated_at=now() WHERE user_id=$1 RETURNING balance_micros,held_micros`, order.UserID, creditedMicros).Scan(&balance, &held)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ledger_entries(user_id,kind,amount_micros,balance_after_micros,held_after_micros,ref_type,ref_id,idempotency_key) VALUES($1,'topup',$2,$3,$4,'payment_event',$5,$6)`, order.UserID, creditedMicros, balance, held, providerTxID, "casso:"+providerTxID)
		if err != nil {
			return err
		}
		received := order.ReceivedAmountMicros + amountMicros
		status := topupStatus(order.ExpectedAmountMicros, received)
		paidAt := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `UPDATE topup_orders SET received_amount_micros=$2,status=$3,paid_at=CASE WHEN $3='paid' THEN $4 ELSE paid_at END WHERE id=$1`, order.ID, received, status, paidAt)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE payment_events SET status='credited',matched_order_id=$2,processed_at=now() WHERE id=$1 AND status='processing'`, eventID, order.ID)
		return err
	})
}
