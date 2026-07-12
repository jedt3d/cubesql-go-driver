package cubesql

import (
	"context"
	"sync"
)

// Tx owns the explicit transaction state on one Conn.
type Tx struct {
	mu   sync.RWMutex
	conn *Conn
	done bool
}

func (conn *Conn) Begin() (*Tx, error) {
	return conn.BeginContext(context.Background())
}

func (conn *Conn) BeginContext(ctx context.Context) (*Tx, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, ErrClosed
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.native == nil {
		return nil, ErrClosed
	}
	if conn.txActive || conn.children != 0 {
		return nil, ErrBusy
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := conn.native.Begin(); err != nil {
		return nil, publicError(err)
	}
	conn.txActive = true
	return &Tx{conn: conn}, nil
}

// Commit commits implicit server transaction state when no Tx handle is
// active. Transactions created by Begin must be completed through their Tx.
func (conn *Conn) Commit() error {
	return conn.CommitContext(context.Background())
}

func (conn *Conn) CommitContext(ctx context.Context) error {
	return conn.finishImplicit(ctx, true)
}

// Rollback rolls back implicit server transaction state when no Tx handle is
// active. Transactions created by Begin must be completed through their Tx.
func (conn *Conn) Rollback() error {
	return conn.RollbackContext(context.Background())
}

func (conn *Conn) RollbackContext(ctx context.Context) error {
	return conn.finishImplicit(ctx, false)
}

func (conn *Conn) finishImplicit(ctx context.Context, commit bool) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if conn == nil {
		return ErrClosed
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.native == nil {
		return ErrClosed
	}
	if conn.txActive {
		return ErrBusy
	}
	if conn.children != 0 {
		return ErrBusy
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if commit {
		return publicError(conn.native.Commit())
	}
	return publicError(conn.native.Rollback())
}

func (tx *Tx) Exec(query string, args ...any) (Result, error) {
	return tx.ExecContext(context.Background(), query, args...)
}

func (tx *Tx) ExecContext(ctx context.Context, query string, args ...any) (Result, error) {
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	if tx == nil {
		return Result{}, ErrTxDone
	}
	tx.mu.RLock()
	defer tx.mu.RUnlock()
	if tx.done || tx.conn == nil {
		return Result{}, ErrTxDone
	}
	return tx.conn.ExecContext(ctx, query, args...)
}

func (tx *Tx) Query(query string, args ...any) (*Rows, error) {
	return tx.QueryContext(context.Background(), query, args...)
}

func (tx *Tx) QueryContext(ctx context.Context, query string, args ...any) (*Rows, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, ErrTxDone
	}
	tx.mu.RLock()
	defer tx.mu.RUnlock()
	if tx.done || tx.conn == nil {
		return nil, ErrTxDone
	}
	return tx.conn.QueryContext(ctx, query, args...)
}

func (tx *Tx) Prepare(query string) (*Stmt, error) {
	return tx.PrepareContext(context.Background(), query)
}

func (tx *Tx) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, ErrTxDone
	}
	tx.mu.RLock()
	defer tx.mu.RUnlock()
	if tx.done || tx.conn == nil {
		return nil, ErrTxDone
	}
	return tx.conn.PrepareContext(ctx, query)
}

func (tx *Tx) Commit() error {
	return tx.CommitContext(context.Background())
}

func (tx *Tx) CommitContext(ctx context.Context) error {
	return tx.finish(ctx, true)
}

func (tx *Tx) Rollback() error {
	return tx.RollbackContext(context.Background())
}

func (tx *Tx) RollbackContext(ctx context.Context) error {
	return tx.finish(ctx, false)
}

func (tx *Tx) finish(ctx context.Context, commit bool) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if tx == nil {
		return ErrTxDone
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.done || tx.conn == nil {
		return ErrTxDone
	}
	conn := tx.conn
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.native == nil {
		return ErrClosed
	}
	if !conn.txActive {
		return ErrTxDone
	}
	if conn.children != 0 {
		return ErrBusy
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	var err error
	if commit {
		err = conn.native.Commit()
	} else {
		err = conn.native.Rollback()
	}
	if err != nil {
		return publicError(err)
	}
	conn.txActive = false
	tx.done = true
	tx.conn = nil
	return nil
}
