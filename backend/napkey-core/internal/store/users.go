package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"napkey-core/internal/pgwire"
)

// User is a row of the users table.
type User struct {
	ID              string
	Email           string
	PasswordHash    string
	EmailVerifiedAt *time.Time
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// IsVerified reports whether the email has been confirmed.
func (u *User) IsVerified() bool { return u.EmailVerifiedAt != nil }

// IsActive reports whether the account may be used.
func (u *User) IsActive() bool { return u.Status == "active" }

// NormalizeEmail lowercases and trims an address.
//
// The database column is citext so comparison is already case-insensitive;
// normalizing on the way in keeps what is stored predictable and what is echoed
// back in emails consistent.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// CreateUser inserts a user. Returns ErrEmailTaken when the address exists.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (*User, error) {
	email = NormalizeEmail(email)
	var u User
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ($1, $2)
		RETURNING id, email, password_hash, email_verified_at, status, created_at, updated_at`,
		email, passwordHash,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.EmailVerifiedAt, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		// Relying on the unique index rather than a prior SELECT is what closes
		// the race between two simultaneous registrations of the same address.
		if pgwire.IsUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("store: creating user: %w", err)
	}
	return &u, nil
}

// GetUserByEmail looks a user up by address.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return s.scanUser(ctx, `
		SELECT id, email, password_hash, email_verified_at, status, created_at, updated_at
		FROM users WHERE email = $1`, NormalizeEmail(email))
}

// GetUserByID looks a user up by id.
func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	return s.scanUser(ctx, `
		SELECT id, email, password_hash, email_verified_at, status, created_at, updated_at
		FROM users WHERE id = $1`, id)
}

func (s *Store) scanUser(ctx context.Context, query string, args ...any) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.EmailVerifiedAt, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: loading user: %w", err)
	}
	return &u, nil
}

// MarkEmailVerified sets email_verified_at if it is not already set.
func (s *Store) MarkEmailVerified(ctx context.Context, userID string) error {
	// The WHERE clause makes this idempotent: clicking a verification link twice
	// must not move the timestamp.
	_, err := s.db.ExecContext(ctx, `
		UPDATE users SET email_verified_at = now()
		WHERE id = $1 AND email_verified_at IS NULL`, userID)
	if err != nil {
		return fmt.Errorf("store: marking email verified: %w", err)
	}
	return nil
}

// UpdatePasswordHash replaces the stored hash and invalidates every session.
//
// Revoking sessions is the point of a password change: if the old password
// leaked, whoever used it keeps access until their session is gone.
func (s *Store) UpdatePasswordHash(ctx context.Context, userID, hash string, keepSessionHash []byte) error {
	return s.withTx(ctx, nil, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash)
		if err != nil {
			return fmt.Errorf("store: updating password: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: updating password: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		if keepSessionHash == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
		} else {
			// Keep the session that performed the change so the user is not logged
			// out of the tab they are looking at.
			_, err = tx.ExecContext(ctx,
				`DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2`, userID, keepSessionHash)
		}
		if err != nil {
			return fmt.Errorf("store: revoking sessions after password change: %w", err)
		}
		return nil
	})
}

// SetUserStatus activates or suspends an account. Suspending drops sessions and
// disables the user's keys, because leaving either alive would let a suspended
// account keep spending.
func (s *Store) SetUserStatus(ctx context.Context, userID, status string) error {
	if status != "active" && status != "suspended" {
		return fmt.Errorf("store: invalid user status %q", status)
	}
	return s.withTx(ctx, nil, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE users SET status = $2 WHERE id = $1`, userID, status)
		if err != nil {
			return fmt.Errorf("store: setting user status: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: setting user status: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		if status == "suspended" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
				return fmt.Errorf("store: revoking sessions on suspend: %w", err)
			}
			// Queue the keys for a push to kiro-go so the data plane stops
			// honoring them too.
			if _, err := tx.ExecContext(ctx, `
				UPDATE api_keys
				SET enabled = false, sync_state = 'pending', next_sync_at = now(), sync_attempts = 0
				WHERE user_id = $1 AND revoked_at IS NULL AND enabled = true`, userID); err != nil {
				return fmt.Errorf("store: disabling keys on suspend: %w", err)
			}
		}
		return nil
	})
}

// CountUsers returns the total number of users, for the admin overview.
func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: counting users: %w", err)
	}
	return n, nil
}

// UserListItem is a row of the admin user list.
type UserListItem struct {
	User
	KeyCount int64
}

// ListUsers returns a page of users, newest first.
func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]UserListItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.email, u.password_hash, u.email_verified_at, u.status, u.created_at, u.updated_at,
		       (SELECT count(*) FROM api_keys k WHERE k.user_id = u.id AND k.revoked_at IS NULL)
		FROM users u
		ORDER BY u.created_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: listing users: %w", err)
	}
	defer rows.Close()

	var out []UserListItem
	for rows.Next() {
		var it UserListItem
		if err := rows.Scan(&it.ID, &it.Email, &it.PasswordHash, &it.EmailVerifiedAt,
			&it.Status, &it.CreatedAt, &it.UpdatedAt, &it.KeyCount); err != nil {
			return nil, fmt.Errorf("store: scanning user list: %w", err)
		}
		// The hash has no business leaving this layer for a list view.
		it.PasswordHash = ""
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating user list: %w", err)
	}
	return out, nil
}

// RecordAuthAttempt logs an attempt for rate limiting.
func (s *Store) RecordAuthAttempt(ctx context.Context, scope, identifier string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_attempts (scope, identifier) VALUES ($1, $2)`, scope, identifier)
	if err != nil {
		return fmt.Errorf("store: recording auth attempt: %w", err)
	}
	return nil
}

// CountAuthAttempts counts attempts for an identifier inside a window.
func (s *Store) CountAuthAttempts(ctx context.Context, scope, identifier string, window time.Duration) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM auth_attempts
		WHERE scope = $1 AND identifier = $2 AND created_at > now() - make_interval(secs => $3)`,
		scope, identifier, window.Seconds()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: counting auth attempts: %w", err)
	}
	return n, nil
}

// ClearAuthAttempts drops the attempt history after a success, so a user who
// mistyped a few times then got in is not still throttled.
func (s *Store) ClearAuthAttempts(ctx context.Context, scope, identifier string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_attempts WHERE scope = $1 AND identifier = $2`, scope, identifier)
	if err != nil {
		return fmt.Errorf("store: clearing auth attempts: %w", err)
	}
	return nil
}

// PruneAuthAttempts deletes rows older than the retention window.
func (s *Store) PruneAuthAttempts(ctx context.Context, olderThan time.Duration) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_attempts WHERE created_at < now() - make_interval(secs => $1)`,
		olderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("store: pruning auth attempts: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// nullableIP renders a client IP for an inet column, returning nil for an
// unparseable value so a malformed X-Forwarded-For cannot fail the insert.
func nullableIP(ip string) any {
	if ip == "" {
		return nil
	}
	if parsed := net.ParseIP(ip); parsed != nil {
		return parsed.String()
	}
	return nil
}
