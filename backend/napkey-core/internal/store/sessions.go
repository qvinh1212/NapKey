package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Session is a server-side login session.
type Session struct {
	TokenHash  []byte
	UserID     string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	UserAgent  string
	IP         string
}

// SessionUser is a session joined with its user, which is what every
// authenticated request needs. One query instead of two keeps the middleware to a
// single round trip.
type SessionUser struct {
	Session Session
	User    User
}

// CreateSession stores a session. The caller holds the cleartext token.
func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, userID string, expiresAt time.Time, userAgent, ip string) error {
	// A pathological User-Agent should not be stored in full.
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at, user_agent, ip)
		VALUES ($1, $2, $3, $4, $5)`,
		tokenHash, userID, expiresAt, userAgent, nullableIP(ip))
	if err != nil {
		return fmt.Errorf("store: creating session: %w", err)
	}
	return nil
}

// LookupSession resolves a session token hash to the session and its user.
//
// Expired rows are filtered in SQL rather than in Go: an expired session must not
// authenticate even if the cleanup job is behind.
func (s *Store) LookupSession(ctx context.Context, tokenHash []byte) (*SessionUser, error) {
	var out SessionUser
	err := s.db.QueryRowContext(ctx, `
		SELECT s.token_hash, s.user_id, s.created_at, s.expires_at, s.last_seen_at,
		       coalesce(s.user_agent, ''), coalesce(host(s.ip), ''),
		       u.id, u.email, u.password_hash, u.email_verified_at, u.status, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash).Scan(
		&out.Session.TokenHash, &out.Session.UserID, &out.Session.CreatedAt,
		&out.Session.ExpiresAt, &out.Session.LastSeenAt, &out.Session.UserAgent, &out.Session.IP,
		&out.User.ID, &out.User.Email, &out.User.PasswordHash, &out.User.EmailVerifiedAt,
		&out.User.Status, &out.User.CreatedAt, &out.User.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up session: %w", err)
	}
	return &out, nil
}

// TouchSession updates last_seen_at, throttled so a busy console does not write
// on every request.
func (s *Store) TouchSession(ctx context.Context, tokenHash []byte, minInterval time.Duration) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET last_seen_at = now()
		WHERE token_hash = $1 AND last_seen_at < now() - make_interval(secs => $2)`,
		tokenHash, minInterval.Seconds())
	if err != nil {
		return fmt.Errorf("store: touching session: %w", err)
	}
	return nil
}

// DeleteSession removes one session (logout).
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("store: deleting session: %w", err)
	}
	return nil
}

// PruneSessions deletes expired sessions.
func (s *Store) PruneSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
	if err != nil {
		return 0, fmt.Errorf("store: pruning sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CreateEmailToken stores a verification or reset token.
func (s *Store) CreateEmailToken(ctx context.Context, tokenHash []byte, userID, purpose string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO email_tokens (token_hash, user_id, purpose, expires_at)
		VALUES ($1, $2, $3, $4)`, tokenHash, userID, purpose, expiresAt)
	if err != nil {
		return fmt.Errorf("store: creating email token: %w", err)
	}
	return nil
}

// ConsumeEmailToken atomically marks a token used and returns its user id.
//
// The UPDATE ... WHERE consumed_at IS NULL RETURNING pattern is what makes the
// token single-use under concurrency: two simultaneous clicks on the same link
// mean one UPDATE matches and the other returns no rows. A SELECT-then-UPDATE
// would let both through.
func (s *Store) ConsumeEmailToken(ctx context.Context, tokenHash []byte, purpose string) (string, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `
		UPDATE email_tokens
		SET consumed_at = now()
		WHERE token_hash = $1
		  AND purpose = $2
		  AND consumed_at IS NULL
		  AND expires_at > now()
		RETURNING user_id`, tokenHash, purpose).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: consuming email token: %w", err)
	}
	return userID, nil
}

// VerifyEmailAndGrantTrial atomically consumes a verification token, verifies
// the account, and grants promotional wallet credit at most once per user and
// source fingerprint. A duplicate fingerprint still verifies the account but
// receives no promotional balance.
func (s *Store) VerifyEmailAndGrantTrial(ctx context.Context, tokenHash, ipHash []byte, amountMicros int64, expiresAt time.Time) (string, bool, error) {
	if amountMicros <= 0 || !expiresAt.After(time.Now()) {
		return "", false, errors.New("store: invalid trial grant")
	}
	var userID string
	granted := false
	err := s.withTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `
			UPDATE email_tokens SET consumed_at = now()
			WHERE token_hash = $1 AND purpose = 'verify_email'
			  AND consumed_at IS NULL AND expires_at > now()
			RETURNING user_id`, tokenHash).Scan(&userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE users SET email_verified_at = coalesce(email_verified_at, now()) WHERE id = $1`, userID); err != nil {
			return err
		}
		// A missing or malformed client address must never prevent email
		// verification. It only makes the account ineligible for automatic trial
		// credit, which is safer than assigning every unknown address one hash.
		if len(ipHash) == 0 {
			return nil
		}
		var grantID string
		err = tx.QueryRowContext(ctx, `
			INSERT INTO trial_grants(user_id, ip_hash, amount_micros, remaining_micros, expires_at)
			VALUES($1,$2,$3,$3,$4)
			ON CONFLICT DO NOTHING
			RETURNING id`, userID, ipHash, amountMicros, expiresAt).Scan(&grantID)
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
			UPDATE wallets
			SET balance_micros = balance_micros + $2,
			    promotional_micros = promotional_micros + $2,
			    promotional_expires_at = $3,
			    updated_at = now()
			WHERE user_id = $1
			RETURNING balance_micros, held_micros`, userID, amountMicros, expiresAt).Scan(&balance, &held)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ledger_entries(user_id,kind,amount_micros,balance_after_micros,held_after_micros,ref_type,ref_id,idempotency_key,metadata)
			VALUES($1,'trial',$2,$3,$4,'trial_grant',$5,$6,jsonb_build_object('expiresAt',$7::timestamptz))`,
			userID, amountMicros, balance, held, grantID, "trial:"+userID, expiresAt)
		if err == nil {
			granted = true
		}
		return err
	})
	if err != nil {
		return "", false, fmt.Errorf("store: verifying email and granting trial: %w", err)
	}
	return userID, granted, nil
}

// InvalidateEmailTokens consumes every outstanding token of a purpose for a user,
// so issuing a new link retires the previous one.
func (s *Store) InvalidateEmailTokens(ctx context.Context, userID, purpose string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE email_tokens SET consumed_at = now()
		WHERE user_id = $1 AND purpose = $2 AND consumed_at IS NULL`, userID, purpose)
	if err != nil {
		return fmt.Errorf("store: invalidating email tokens: %w", err)
	}
	return nil
}

// PruneEmailTokens deletes tokens that are expired or long consumed.
func (s *Store) PruneEmailTokens(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM email_tokens
		WHERE expires_at < now() - interval '7 days'
		   OR (consumed_at IS NOT NULL AND consumed_at < now() - interval '7 days')`)
	if err != nil {
		return 0, fmt.Errorf("store: pruning email tokens: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// AuditEntry is one row to append to audit_logs.
type AuditEntry struct {
	ActorType  string
	ActorID    string
	Action     string
	TargetType string
	TargetID   string
	Metadata   map[string]any
	IP         string
}

// WriteAudit appends an audit row.
//
// Audit failures are returned, not swallowed, but callers deliberately treat them
// as non-fatal for the request: losing the audit trail is bad, refusing a
// legitimate action because logging hiccuped is worse.
func (s *Store) WriteAudit(ctx context.Context, e AuditEntry) error {
	metadata := []byte("{}")
	if len(e.Metadata) > 0 {
		encoded, err := json.Marshal(e.Metadata)
		if err != nil {
			return fmt.Errorf("store: encoding audit metadata: %w", err)
		}
		metadata = encoded
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_logs (actor_type, actor_id, action, target_type, target_id, metadata, ip)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)`,
		e.ActorType, nullableString(e.ActorID), e.Action,
		nullableString(e.TargetType), nullableString(e.TargetID),
		string(metadata), nullableIP(e.IP))
	if err != nil {
		return fmt.Errorf("store: writing audit log: %w", err)
	}
	return nil
}

// AuditLogItem is a row read back from audit_logs.
type AuditLogItem struct {
	ID         int64
	ActorType  string
	ActorID    string
	Action     string
	TargetType string
	TargetID   string
	Metadata   map[string]any
	IP         string
	CreatedAt  time.Time
}

// ListAuditLogs returns recent audit rows, optionally filtered by actor.
func (s *Store) ListAuditLogs(ctx context.Context, actorID string, limit, offset int) ([]AuditLogItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	// One query with a NULL-tolerant filter rather than string-built SQL. The
	// empty actorID case passes NULL and the predicate short-circuits.
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_type, coalesce(actor_id, ''), action,
		       coalesce(target_type, ''), coalesce(target_id, ''),
		       metadata::text, coalesce(host(ip), ''), created_at
		FROM audit_logs
		WHERE ($1::text IS NULL OR actor_id = $1)
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, nullableString(actorID), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: listing audit logs: %w", err)
	}
	defer rows.Close()

	var out []AuditLogItem
	for rows.Next() {
		var it AuditLogItem
		var metadataJSON string
		if err := rows.Scan(&it.ID, &it.ActorType, &it.ActorID, &it.Action,
			&it.TargetType, &it.TargetID, &metadataJSON, &it.IP, &it.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scanning audit log: %w", err)
		}
		if metadataJSON != "" {
			_ = json.Unmarshal([]byte(metadataJSON), &it.Metadata)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating audit logs: %w", err)
	}
	return out, nil
}

// nullableString maps "" to SQL NULL so optional columns stay NULL instead of
// holding an empty string, which would break the IS NULL filters above.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
