package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"napkey-core/internal/pgwire"
)

// FindOrCreateOAuthUser resolves a stable provider subject, linking an existing
// verified email account or creating a new verified user when needed.
func (s *Store) FindOrCreateOAuthUser(ctx context.Context, provider, subject, email, passwordHash string) (*User, bool, error) {
	var out User
	created := false
	err := s.withTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *sql.Tx) error {
		scan := func(query string, args ...any) error {
			return tx.QueryRowContext(ctx, query, args...).Scan(
				&out.ID, &out.Email, &out.PasswordHash, &out.EmailVerifiedAt,
				&out.Status, &out.CreatedAt, &out.UpdatedAt)
		}

		err := scan(`
			SELECT u.id,u.email,u.password_hash,u.email_verified_at,u.status,u.created_at,u.updated_at
			FROM oauth_identities oi JOIN users u ON u.id=oi.user_id
			WHERE oi.provider=$1 AND oi.provider_subject=$2`, provider, subject)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		err = scan(`
			SELECT id,email,password_hash,email_verified_at,status,created_at,updated_at
			FROM users WHERE email=$1 FOR UPDATE`, NormalizeEmail(email))
		if errors.Is(err, sql.ErrNoRows) {
			err = scan(`
				INSERT INTO users(email,password_hash,email_verified_at)
				VALUES($1,$2,now())
				RETURNING id,email,password_hash,email_verified_at,status,created_at,updated_at`,
				NormalizeEmail(email), passwordHash)
			created = err == nil
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx,
			`UPDATE users SET email_verified_at=coalesce(email_verified_at,now()) WHERE id=$1`, out.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO oauth_identities(user_id,provider,provider_subject)
			VALUES($1,$2,$3)`, out.ID, provider, subject)
		if pgwire.IsUniqueViolation(err) {
			return ErrOAuthConflict
		}
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("store: resolving oauth user: %w", err)
	}
	return &out, created, nil
}

// GrantTrialForUser grants the same one-time promotional balance used by email
// verification. A duplicate user or IP fingerprint is a successful no-op.
func (s *Store) GrantTrialForUser(ctx context.Context, userID string, ipHash []byte, amountMicros int64, expiresAt time.Time) (bool, error) {
	if userID == "" || len(ipHash) == 0 || amountMicros <= 0 || !expiresAt.After(time.Now()) {
		return false, nil
	}
	granted := false
	err := s.withTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *sql.Tx) error {
		var grantID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO trial_grants(user_id,ip_hash,amount_micros,remaining_micros,expires_at)
			VALUES($1,$2,$3,$3,$4)
			ON CONFLICT DO NOTHING RETURNING id`, userID, ipHash, amountMicros, expiresAt).Scan(&grantID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO wallets(user_id) VALUES($1) ON CONFLICT DO NOTHING`, userID); err != nil {
			return err
		}
		var balance, held int64
		err = tx.QueryRowContext(ctx, `
			UPDATE wallets SET balance_micros=balance_micros+$2,
			promotional_micros=promotional_micros+$2,promotional_expires_at=least(coalesce(promotional_expires_at,$3),$3),updated_at=now()
			WHERE user_id=$1 RETURNING balance_micros,held_micros`, userID, amountMicros, expiresAt).Scan(&balance, &held)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ledger_entries(user_id,kind,amount_micros,balance_after_micros,held_after_micros,ref_type,ref_id,idempotency_key,metadata)
			VALUES($1,'trial',$2,$3,$4,'trial_grant',$5,$6,jsonb_build_object('expiresAt',$7::timestamptz))`,
			userID, amountMicros, balance, held, grantID, "trial:"+userID, expiresAt)
		granted = err == nil
		return err
	})
	if err != nil {
		return false, fmt.Errorf("store: granting oauth trial: %w", err)
	}
	return granted, nil
}
