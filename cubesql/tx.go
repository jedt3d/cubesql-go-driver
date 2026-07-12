package cubesql

import "sync"

// Tx owns the explicit transaction state on one Conn.
type Tx struct {
	mu   sync.RWMutex
	conn *Conn
	done bool
}

func (conn *Conn) Begin() (*Tx, error) {
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
	if err := conn.native.Begin(); err != nil {
		return nil, publicError(err)
	}
	conn.txActive = true
	return &Tx{conn: conn}, nil
}

// Commit commits implicit server transaction state when no Tx handle is
// active. Transactions created by Begin must be completed through their Tx.
func (conn *Conn) Commit() error {
	return conn.finishImplicit(true)
}

// Rollback rolls back implicit server transaction state when no Tx handle is
// active. Transactions created by Begin must be completed through their Tx.
func (conn *Conn) Rollback() error {
	return conn.finishImplicit(false)
}

func (conn *Conn) finishImplicit(commit bool) error {
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
	if commit {
		return publicError(conn.native.Commit())
	}
	return publicError(conn.native.Rollback())
}

func (tx *Tx) Exec(query string, args ...any) (Result, error) {
	if tx == nil {
		return Result{}, ErrTxDone
	}
	tx.mu.RLock()
	defer tx.mu.RUnlock()
	if tx.done || tx.conn == nil {
		return Result{}, ErrTxDone
	}
	return tx.conn.Exec(query, args...)
}

func (tx *Tx) Query(query string, args ...any) (*Rows, error) {
	if tx == nil {
		return nil, ErrTxDone
	}
	tx.mu.RLock()
	defer tx.mu.RUnlock()
	if tx.done || tx.conn == nil {
		return nil, ErrTxDone
	}
	return tx.conn.Query(query, args...)
}

func (tx *Tx) Prepare(query string) (*Stmt, error) {
	if tx == nil {
		return nil, ErrTxDone
	}
	tx.mu.RLock()
	defer tx.mu.RUnlock()
	if tx.done || tx.conn == nil {
		return nil, ErrTxDone
	}
	return tx.conn.Prepare(query)
}

func (tx *Tx) Commit() error {
	return tx.finish(true)
}

func (tx *Tx) Rollback() error {
	return tx.finish(false)
}

func (tx *Tx) finish(commit bool) error {
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
