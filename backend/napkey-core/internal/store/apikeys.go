package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Sync states for the push toward kiro-go.
const (
	// SyncPending means the key exists here but kiro-go has not confirmed it.
	SyncPending = "pending"
	// SyncSynced means kiro-go holds a matching key.
	SyncSynced = "synced"
	// SyncFailed means the last push failed; the reconciler retries with backoff.
	SyncFailed = "failed"
	// SyncDeletePending means the key must be removed from kiro-go. The row stays
	// here until that succeeds, otherwise a revoked key could keep working in the
	// data plane with nothing left to point at it.
	SyncDeletePending = "delete_pending"
)

// APIKey is a row of api_keys joined with its usage counters.
type APIKey struct {
	ID          string
	UserID      string
	Name        string
	KeyPrefix   string
	LastFour    string
	Enabled     bool
	RPMLimit    *int
	TPMLimit    *int
	TokenLimit  int64
	CreditLimit float64
	RevokedAt   *time.Time
	LastUsedAt  *time.Time
	CreatedAt   time.Time

	SyncState    string
	SyncError    string
	SyncedAt     *time.Time
	SyncAttempts int
	RemoteID     string

	TokensUsed    int64
	CreditsUsed   float64
	RequestsCount int64
}

// IsActive reports whether the key should be able to authenticate.
func (k *APIKey) IsActive() bool { return k.Enabled && k.RevokedAt == nil }

// IsTestMode reports whether this is a test-mode key.
func (k *APIKey) IsTestMode() bool { return k.KeyPrefix == "nk_test_" }

// CreateAPIKeyParams carries the inputs for CreateAPIKey.
type CreateAPIKeyParams struct {
	UserID      string
	Name        string
	KeyPrefix   string
	KeyHash     []byte
	LastFour    string
	TokenLimit  int64
	CreditLimit float64
	// MaxPerUser caps how many live keys a user may hold.
	MaxPerUser int
}

// CreateAPIKey inserts a key and its usage row.
//
// The count check and the insert share a transaction with a row lock on the user,
// so two concurrent creations cannot both see "9 keys" and both insert a tenth.
func (s *Store) CreateAPIKey(ctx context.Context, p CreateAPIKeyParams) (*APIKey, error) {
	var created *APIKey
	err := s.withTx(ctx, nil, func(tx *sql.Tx) error {
		// Locking the user row serializes key creation per user. Without it the
		// limit is advisory only.
		var status string
		err := tx.QueryRowContext(ctx,
			`SELECT status FROM users WHERE id = $1 FOR UPDATE`, p.UserID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("store: locking user for key creation: %w", err)
		}
		if status != "active" {
			return ErrUserSuspended
		}

		var count int
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM api_keys WHERE user_id = $1 AND revoked_at IS NULL`,
			p.UserID).Scan(&count); err != nil {
			return fmt.Errorf("store: counting keys: %w", err)
		}
		if p.MaxPerUser > 0 && count >= p.MaxPerUser {
			return ErrKeyLimit
		}

		var k APIKey
		err = tx.QueryRowContext(ctx, `
			INSERT INTO api_keys (user_id, name, key_prefix, key_hash, last_four, token_limit, credit_limit)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, user_id, name, key_prefix, last_four, enabled, rpm_limit, tpm_limit,
			          token_limit, credit_limit, revoked_at, last_used_at, created_at,
			          sync_state, coalesce(sync_error, ''), synced_at, sync_attempts, coalesce(remote_id, '')`,
			p.UserID, p.Name, p.KeyPrefix, p.KeyHash, p.LastFour, p.TokenLimit, p.CreditLimit,
		).Scan(&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.LastFour, &k.Enabled,
			&k.RPMLimit, &k.TPMLimit, &k.TokenLimit, &k.CreditLimit, &k.RevokedAt,
			&k.LastUsedAt, &k.CreatedAt, &k.SyncState, &k.SyncError, &k.SyncedAt,
			&k.SyncAttempts, &k.RemoteID)
		if err != nil {
			return fmt.Errorf("store: inserting api key: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO api_key_usage (api_key_id) VALUES ($1)`, k.ID); err != nil {
			return fmt.Errorf("store: inserting api key usage row: %w", err)
		}
		created = &k
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// apiKeySelect is the shared column list for reads.
const apiKeySelect = `
	SELECT k.id, k.user_id, k.name, k.key_prefix, k.last_four, k.enabled,
	       k.rpm_limit, k.tpm_limit, k.token_limit, k.credit_limit,
	       k.revoked_at, k.last_used_at, k.created_at,
	       k.sync_state, coalesce(k.sync_error, ''), k.synced_at, k.sync_attempts, coalesce(k.remote_id, ''),
	       coalesce(u.tokens_used, 0), coalesce(u.credits_used, 0), coalesce(u.requests_count, 0)
	FROM api_keys k
	LEFT JOIN api_key_usage u ON u.api_key_id = k.id`

func scanAPIKey(sc interface{ Scan(...any) error }) (*APIKey, error) {
	var k APIKey
	err := sc.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.LastFour, &k.Enabled,
		&k.RPMLimit, &k.TPMLimit, &k.TokenLimit, &k.CreditLimit,
		&k.RevokedAt, &k.LastUsedAt, &k.CreatedAt,
		&k.SyncState, &k.SyncError, &k.SyncedAt, &k.SyncAttempts, &k.RemoteID,
		&k.TokensUsed, &k.CreditsUsed, &k.RequestsCount)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// ListAPIKeys returns a user's keys, newest first. Revoked keys are included so
// the console can show history; the caller decides what to display.
func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, apiKeySelect+`
		WHERE k.user_id = $1
		ORDER BY k.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: listing api keys: %w", err)
	}
	defer rows.Close()

	var out []APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scanning api key: %w", err)
		}
		out = append(out, *k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating api keys: %w", err)
	}
	return out, nil
}

// GetAPIKey loads one key scoped to its owner.
//
// The user_id predicate is the authorization check, done in SQL. Loading by id
// and comparing ownership in Go is the shape that produces IDOR bugs when someone
// later forgets the comparison.
func (s *Store) GetAPIKey(ctx context.Context, userID, keyID string) (*APIKey, error) {
	k, err := scanAPIKey(s.db.QueryRowContext(ctx, apiKeySelect+`
		WHERE k.id = $1 AND k.user_id = $2`, keyID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: loading api key: %w", err)
	}
	return k, nil
}

// GetAPIKeyByID loads a key without an ownership filter, for admin use only.
func (s *Store) GetAPIKeyByID(ctx context.Context, keyID string) (*APIKey, error) {
	k, err := scanAPIKey(s.db.QueryRowContext(ctx, apiKeySelect+` WHERE k.id = $1`, keyID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: loading api key: %w", err)
	}
	return k, nil
}

// UpdateAPIKeyParams carries an edit. Nil fields are left unchanged.
type UpdateAPIKeyParams struct {
	Name        *string
	Enabled     *bool
	RPMLimit    *int
	TPMLimit    *int
	TokenLimit  *int64
	CreditLimit *float64
}

// UpdateAPIKey applies an edit and re-queues the key for a push to kiro-go.
//
// COALESCE keeps this to one statement with a fixed shape: no SQL is assembled
// from which fields happen to be set.
func (s *Store) UpdateAPIKey(ctx context.Context, userID, keyID string, p UpdateAPIKeyParams) (*APIKey, error) {
	var (
		name        any = nil
		enabled     any = nil
		tokenLimit  any = nil
		creditLimit any = nil
		rpmLimit    any = nil
		tpmLimit    any = nil
	)
	if p.Name != nil {
		name = *p.Name
	}
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	if p.RPMLimit != nil {
		rpmLimit = *p.RPMLimit
	}
	if p.TPMLimit != nil {
		tpmLimit = *p.TPMLimit
	}
	if p.TokenLimit != nil {
		tokenLimit = *p.TokenLimit
	}
	if p.CreditLimit != nil {
		creditLimit = *p.CreditLimit
	}

	// Any change to a field kiro-go mirrors resets sync bookkeeping, so the
	// reconciler pushes it promptly instead of waiting out an old backoff.
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_keys SET
			name         = coalesce($3::text, name),
			enabled      = coalesce($4::boolean, enabled),
			rpm_limit    = coalesce($5::integer, rpm_limit),
			tpm_limit    = coalesce($6::integer, tpm_limit),
			token_limit  = coalesce($7::bigint, token_limit),
			credit_limit = coalesce($8::double precision, credit_limit),
			sync_state   = CASE WHEN sync_state = 'delete_pending' THEN sync_state ELSE 'pending' END,
			sync_attempts = 0,
			next_sync_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		keyID, userID, name, enabled, rpmLimit, tpmLimit, tokenLimit, creditLimit)
	if err != nil {
		return nil, fmt.Errorf("store: updating api key: %w", err)
	}
	return s.GetAPIKey(ctx, userID, keyID)
}

// RevokeAPIKey marks a key revoked and queues its removal from kiro-go.
//
// The row is kept rather than deleted: usage records in Stage 3 reference it, and
// an audit trail that loses the subject of the action is not an audit trail.
func (s *Store) RevokeAPIKey(ctx context.Context, userID, keyID string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE api_keys
		SET revoked_at = now(),
		    enabled = false,
		    sync_state = 'delete_pending',
		    sync_attempts = 0,
		    next_sync_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, keyID, userID)
	if err != nil {
		return fmt.Errorf("store: revoking api key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: revoking api key: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClaimKeysForSync locks a batch of keys needing a push and returns them.
//
// FOR UPDATE SKIP LOCKED is what lets more than one reconciler run without two of
// them pushing the same key: a row already claimed is skipped rather than waited
// on.
func (s *Store) ClaimKeysForSync(ctx context.Context, limit int) ([]APIKey, error) {
	if limit <= 0 {
		limit = 20
	}
	var out []APIKey
	err := s.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT k.id, k.user_id, k.name, k.key_prefix, k.last_four, k.enabled,
			       k.rpm_limit, k.tpm_limit, k.token_limit, k.credit_limit,
			       k.revoked_at, k.last_used_at, k.created_at,
			       k.sync_state, coalesce(k.sync_error, ''), k.synced_at, k.sync_attempts, coalesce(k.remote_id, ''),
			       0::bigint, 0::double precision, 0::bigint
			FROM api_keys k
			WHERE k.sync_state IN ('pending', 'failed', 'delete_pending')
			  AND k.next_sync_at <= now()
			ORDER BY k.next_sync_at
			LIMIT $1
			FOR UPDATE OF k SKIP LOCKED`, limit)
		if err != nil {
			return fmt.Errorf("store: claiming keys for sync: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			k, err := scanAPIKey(rows)
			if err != nil {
				return fmt.Errorf("store: scanning claimed key: %w", err)
			}
			out = append(out, *k)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("store: iterating claimed keys: %w", err)
		}
		if len(out) == 0 {
			return nil
		}
		// Push next_sync_at out so a crash between claim and result does not spin
		// the reconciler on the same rows.
		ids := make([]string, len(out))
		for i, k := range out {
			ids[i] = k.ID
		}
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx,
				`UPDATE api_keys SET next_sync_at = now() + interval '2 minutes' WHERE id = $1`, id); err != nil {
				return fmt.Errorf("store: reserving key for sync: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MarkKeySynced records a successful push.
func (s *Store) MarkKeySynced(ctx context.Context, keyID, remoteID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_keys
		SET sync_state = 'synced', sync_error = NULL, synced_at = now(),
		    sync_attempts = 0, remote_id = $2,
		    next_sync_at = now() + interval '1 hour'
		WHERE id = $1`, keyID, nullableString(remoteID))
	if err != nil {
		return fmt.Errorf("store: marking key synced: %w", err)
	}
	return nil
}

// MarkKeySyncFailed records a failed push and schedules a retry.
//
// Backoff is exponential and capped. The attempt count is not a cutoff: a key
// that never syncs stays visible as failed forever, because silently giving up
// would leave a key the customer can see but cannot use.
func (s *Store) MarkKeySyncFailed(ctx context.Context, keyID, reason string, deletePending bool) error {
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	state := SyncFailed
	if deletePending {
		state = SyncDeletePending
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_keys
		SET sync_state = $3,
		    sync_error = $2,
		    sync_attempts = sync_attempts + 1,
		    next_sync_at = now() + make_interval(secs => least(power(2, least(sync_attempts + 1, 10)) * 5, 3600))
		WHERE id = $1`, keyID, reason, state)
	if err != nil {
		return fmt.Errorf("store: marking key sync failed: %w", err)
	}
	return nil
}

// DeleteSyncedKey removes a revoked key's row once kiro-go has dropped it.
//
// Only rows that are both revoked and delete_pending are eligible, so a live key
// can never be removed through this path.
func (s *Store) DeleteSyncedKey(ctx context.Context, keyID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_keys
		SET sync_state = 'synced', sync_error = NULL, synced_at = now(),
		    sync_attempts = 0, next_sync_at = now() + interval '100 years'
		WHERE id = $1 AND revoked_at IS NOT NULL AND sync_state = 'delete_pending'`, keyID)
	if err != nil {
		return fmt.Errorf("store: finalizing key deletion: %w", err)
	}
	return nil
}

// UsageSummary totals a user's consumption across keys.
type UsageSummary struct {
	TotalTokens   int64
	TotalCredits  float64
	TotalRequests int64
	ActiveKeys    int64
	// TotalCostMicros is the lifetime spend in micro-VND, summed from the integer
	// column. TotalCredits is the legacy float64 kept for kiro-go compatibility and
	// is not what the console bills against.
	TotalCostMicros int64
}

// GetUsageSummary aggregates the cached counters for a user.
//
// This reads api_key_usage, the derived cache, rather than the usage_records
// ledger: it is the console's landing-page number and one indexed row per key is
// far cheaper than aggregating the ledger. GetUserUsageTotals reads the ledger when
// an exact, range-bounded figure is needed, and FindCounterDrift proves the two
// agree.
func (s *Store) GetUsageSummary(ctx context.Context, userID string) (*UsageSummary, error) {
	var out UsageSummary
	err := s.db.QueryRowContext(ctx, `
		SELECT coalesce(sum(u.tokens_used), 0),
		       coalesce(sum(u.credits_used), 0),
		       coalesce(sum(u.requests_count), 0),
		       count(*) FILTER (WHERE k.revoked_at IS NULL AND k.enabled),
		       coalesce(sum(u.cost_micros), 0)
		FROM api_keys k
		LEFT JOIN api_key_usage u ON u.api_key_id = k.id
		WHERE k.user_id = $1`, userID).Scan(
		&out.TotalTokens, &out.TotalCredits, &out.TotalRequests, &out.ActiveKeys,
		&out.TotalCostMicros)
	if err != nil {
		return nil, fmt.Errorf("store: summarizing usage: %w", err)
	}
	return &out, nil
}

// CountPendingSyncs reports how many keys are waiting on kiro-go, for /health.
func (s *Store) CountPendingSyncs(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM api_keys
		WHERE sync_state IN ('pending', 'failed', 'delete_pending')`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: counting pending syncs: %w", err)
	}
	return n, nil
}

// DeleteAPIKeyRow hard-deletes a key row.
//
// This exists for exactly one case: a key whose creation push to kiro-go failed.
// Because napkey-core stores only the hash, there is no cleartext left to retry
// with, and the key was never shown to the user, so removing the row is the
// honest outcome. Every other removal path uses RevokeAPIKey, which preserves
// history.
func (s *Store) DeleteAPIKeyRow(ctx context.Context, keyID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM api_keys WHERE id = $1 AND sync_state <> 'synced'`, keyID)
	if err != nil {
		return fmt.Errorf("store: deleting unsynced api key: %w", err)
	}
	return nil
}

// MarkKeyUnusable revokes a key that never reached kiro-go.
//
// A key stuck in pending or failed with no remote id cannot be re-pushed, since
// the cleartext is gone. Leaving it visible and apparently valid would be a lie
// to the customer, so it is revoked with the reason recorded and the console tells
// them to create a new one.
func (s *Store) MarkKeyUnusable(ctx context.Context, keyID, reason string) error {
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_keys
		SET revoked_at = coalesce(revoked_at, now()),
		    enabled = false,
		    sync_state = 'failed',
		    sync_error = $2,
		    next_sync_at = now() + interval '100 years'
		WHERE id = $1 AND revoked_at IS NULL`, keyID, reason)
	if err != nil {
		return fmt.Errorf("store: marking key unusable: %w", err)
	}
	return nil
}

// ListSyncedRemoteIDs returns the kiro-go ids napkey-core believes exist, used to
// detect drift in the data plane.
func (s *Store) ListSyncedRemoteIDs(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT remote_id, id FROM api_keys
		WHERE remote_id IS NOT NULL AND remote_id <> '' AND revoked_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("store: listing remote ids: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var remoteID, localID string
		if err := rows.Scan(&remoteID, &localID); err != nil {
			return nil, fmt.Errorf("store: scanning remote id: %w", err)
		}
		out[remoteID] = localID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating remote ids: %w", err)
	}
	return out, nil
}

// StaleUnsyncedKey is a key whose creation never landed in the data plane.
type StaleUnsyncedKey struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	SyncError string
}

// ListStaleUnsyncedKeys finds keys that have been waiting on a create push longer
// than the grace period and have no remote id to retry against.
func (s *Store) ListStaleUnsyncedKeys(ctx context.Context, olderThan time.Duration, limit int) ([]StaleUnsyncedKey, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, created_at, coalesce(sync_error, '')
		FROM api_keys
		WHERE sync_state IN ('pending', 'failed')
		  AND (remote_id IS NULL OR remote_id = '')
		  AND revoked_at IS NULL
		  AND created_at < now() - make_interval(secs => $1)
		ORDER BY created_at
		LIMIT $2`, olderThan.Seconds(), limit)
	if err != nil {
		return nil, fmt.Errorf("store: listing stale unsynced keys: %w", err)
	}
	defer rows.Close()
	var out []StaleUnsyncedKey
	for rows.Next() {
		var k StaleUnsyncedKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.CreatedAt, &k.SyncError); err != nil {
			return nil, fmt.Errorf("store: scanning stale unsynced key: %w", err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating stale unsynced keys: %w", err)
	}
	return out, nil
}
