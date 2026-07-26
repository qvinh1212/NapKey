package pgwire

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
)

// Begin implements driver.Conn.
func (c *conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx implements driver.ConnBeginTx.
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.bad {
		return nil, driver.ErrBadConn
	}
	if c.txStatus != 'I' {
		// Nested transactions would need savepoints, which database/sql does not
		// ask for. Getting here means a Tx was leaked.
		return nil, errors.New("pgwire: a transaction is already in progress on this connection")
	}

	stmt := "BEGIN"
	switch sql.IsolationLevel(opts.Isolation) {
	case sql.LevelDefault:
	case sql.LevelReadUncommitted:
		// Postgres has no dirty reads; read uncommitted behaves as read committed.
		stmt += " ISOLATION LEVEL READ UNCOMMITTED"
	case sql.LevelReadCommitted:
		stmt += " ISOLATION LEVEL READ COMMITTED"
	case sql.LevelRepeatableRead, sql.LevelSnapshot:
		stmt += " ISOLATION LEVEL REPEATABLE READ"
	case sql.LevelSerializable, sql.LevelLinearizable:
		stmt += " ISOLATION LEVEL SERIALIZABLE"
	default:
		return nil, fmt.Errorf("pgwire: unsupported isolation level %d", opts.Isolation)
	}
	if opts.ReadOnly {
		stmt += " READ ONLY"
	}

	if err := c.simpleExec(ctx, stmt); err != nil {
		return nil, err
	}
	return &tx{c: c}, nil
}

// tx is an explicit transaction.
type tx struct {
	c    *conn
	done bool
}

// Commit implements driver.Tx.
func (t *tx) Commit() error {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	if t.done {
		return errors.New("pgwire: transaction already finished")
	}
	t.done = true
	if t.c.closed || t.c.bad {
		return driver.ErrBadConn
	}
	// Committing a transaction the server has already aborted would silently
	// discard the work; Postgres turns COMMIT into ROLLBACK there. Report it.
	if t.c.txStatus == 'E' {
		if err := t.c.simpleExec(context.Background(), "ROLLBACK"); err != nil {
			return err
		}
		return errors.New("pgwire: transaction was aborted by an earlier error and has been rolled back")
	}
	return t.c.simpleExec(context.Background(), "COMMIT")
}

// Rollback implements driver.Tx.
func (t *tx) Rollback() error {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	if t.done {
		return errors.New("pgwire: transaction already finished")
	}
	t.done = true
	if t.c.closed || t.c.bad {
		return driver.ErrBadConn
	}
	return t.c.simpleExec(context.Background(), "ROLLBACK")
}

var _ driver.Tx = (*tx)(nil)
