// Package store is the Postgres persistence layer.
//
// Every query is written against database/sql with bound parameters. No SQL is
// ever assembled from user input; the only dynamic fragments are fixed sort
// keywords chosen from a closed allowlist.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"napkey-core/internal/logger"
	_ "napkey-core/internal/pgwire"
)

// Common domain errors. Handlers map these to HTTP status codes, which keeps
// SQLSTATE handling from leaking into the transport layer.
var (
	ErrNotFound      = errors.New("not found")
	ErrEmailTaken    = errors.New("email is already registered")
	ErrKeyLimit      = errors.New("api key limit reached")
	ErrUserSuspended = errors.New("user is suspended")
	ErrOAuthConflict = errors.New("oauth identity is linked to another account")
	ErrInsufficientFunds = errors.New("insufficient wallet balance")
)

// Store wraps the connection pool.
type Store struct {
	db *sql.DB
}

// Open connects, verifies the connection, and configures the pool.
func Open(ctx context.Context, dsn string, maxOpen, maxIdle int) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: opening database: %w", err)
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	// Postgres backs each connection with a process, so recycling bounds the
	// damage from a slow leak and lets a rolling database restart drain cleanly.
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: connecting to Postgres: %w", err)
	}
	return &Store{db: db}, nil
}

// DB exposes the pool for the migration runner.
func (s *Store) DB() *sql.DB { return s.db }

// Close releases the pool.
func (s *Store) Close() error { return s.db.Close() }

// withTx runs fn inside a transaction, rolling back on error or panic.
//
// The rollback in the deferred function is what makes a panic mid-transaction
// safe: without it the connection would return to the pool with an open
// transaction holding locks.
func (s *Store) withTx(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("store: beginning transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				logger.Warnf("transaction rollback failed: %v", rbErr)
			}
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: committing transaction: %w", err)
	}
	committed = true
	return nil
}
