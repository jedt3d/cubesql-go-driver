package cubesql

import (
	"context"
	"sync"

	"github.com/jedt3d/cubesql-go-driver/internal/csdk"
)

// Stmt owns one prepared statement on a Conn.
type Stmt struct {
	mu         sync.RWMutex
	conn       *Conn
	native     *csdk.Stmt
	queryText  string
	rows       int
	registered bool
}

func (stmt *Stmt) Close() error {
	if stmt == nil {
		return nil
	}
	stmt.mu.Lock()
	defer stmt.mu.Unlock()
	if stmt.native == nil {
		return nil
	}
	if stmt.rows != 0 {
		return ErrBusy
	}
	native := stmt.native
	stmt.native = nil
	err := publicError(native.Close())
	if stmt.registered {
		stmt.registered = false
		stmt.conn.releaseChild()
	}
	return err
}

func (stmt *Stmt) Bind(index int, value any) error {
	if index <= 0 {
		return ErrInvalidArgument
	}
	return stmt.call(func(native *csdk.Stmt) error {
		return setPrepared(native, index, value)
	})
}

func (stmt *Stmt) BindInt64(index int, value int64) error {
	return stmt.Bind(index, value)
}

func (stmt *Stmt) BindFloat64(index int, value float64) error {
	return stmt.Bind(index, value)
}

func (stmt *Stmt) BindText(index int, value string) error {
	return stmt.Bind(index, value)
}

func (stmt *Stmt) BindBlob(index int, value []byte) error {
	return stmt.Bind(index, value)
}

func (stmt *Stmt) BindNull(index int) error {
	return stmt.Bind(index, nil)
}

func (stmt *Stmt) BindZeroBlob(index, size int) error {
	return stmt.Bind(index, ZeroBlob{Size: size})
}

func (stmt *Stmt) Exec() (Result, error) {
	return stmt.ExecContext(context.Background())
}

func (stmt *Stmt) ExecContext(ctx context.Context) (Result, error) {
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	if stmt == nil {
		return Result{}, ErrClosed
	}
	stmt.mu.Lock()
	defer stmt.mu.Unlock()
	if stmt.native == nil || stmt.conn == nil {
		return Result{}, ErrClosed
	}
	if stmt.rows != 0 {
		return Result{}, ErrBusy
	}
	stmt.conn.mu.Lock()
	defer stmt.conn.mu.Unlock()
	if stmt.conn.native == nil {
		return Result{}, ErrClosed
	}
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	if err := stmt.native.Exec(); err != nil {
		return Result{}, publicError(err)
	}
	return resultForContext(ctx, stmt.conn.native), nil
}

func (stmt *Stmt) Query() (*Rows, error) {
	return stmt.QueryContext(context.Background())
}

func (stmt *Stmt) QueryContext(ctx context.Context) (*Rows, error) {
	return stmt.query(ctx, false)
}

func (stmt *Stmt) query(ctx context.Context, closeWithRows bool) (*Rows, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if stmt == nil {
		return nil, ErrClosed
	}
	stmt.mu.Lock()
	if stmt.native == nil {
		stmt.mu.Unlock()
		return nil, ErrClosed
	}
	if stmt.rows != 0 {
		stmt.mu.Unlock()
		return nil, ErrBusy
	}
	if err := contextError(ctx); err != nil {
		stmt.mu.Unlock()
		return nil, err
	}
	nativeRows, err := stmt.native.Query()
	if err != nil {
		stmt.mu.Unlock()
		return nil, publicError(err)
	}
	rows, rowsErr := newRowsContext(ctx, nativeRows)
	if rowsErr != nil {
		stmt.mu.Unlock()
		if closeWithRows {
			stmt.Close()
		}
		return nil, rowsErr
	}
	if err := stmt.conn.addChild(); err != nil {
		rows.Close()
		stmt.mu.Unlock()
		if closeWithRows {
			stmt.Close()
		}
		return nil, err
	}
	stmt.rows++
	rows.parentConn = stmt.conn
	rows.parentStmt = stmt
	rows.closeStmt = closeWithRows
	stmt.mu.Unlock()
	return rows, nil
}

// Reset replaces the native prepared statement with a freshly prepared copy of
// the same SQL, clearing all previous binds while preserving this Go handle.
func (stmt *Stmt) Reset() error {
	return stmt.ResetContext(context.Background())
}

func (stmt *Stmt) ResetContext(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if stmt == nil {
		return ErrClosed
	}
	stmt.mu.Lock()
	defer stmt.mu.Unlock()
	if stmt.native == nil || stmt.conn == nil {
		return ErrClosed
	}
	if stmt.rows != 0 {
		return ErrBusy
	}
	stmt.conn.mu.Lock()
	defer stmt.conn.mu.Unlock()
	if stmt.conn.native == nil {
		return ErrClosed
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	previous := stmt.native
	stmt.native = nil
	if err := previous.Close(); err != nil {
		if stmt.registered {
			stmt.registered = false
			stmt.conn.children--
		}
		return publicError(err)
	}
	if err := contextError(ctx); err != nil {
		if stmt.registered {
			stmt.registered = false
			stmt.conn.children--
		}
		return err
	}
	replacement, err := stmt.conn.native.Prepare(stmt.queryText)
	if err != nil {
		if stmt.registered {
			stmt.registered = false
			stmt.conn.children--
		}
		return publicError(err)
	}
	stmt.native = replacement
	return nil
}

func (stmt *Stmt) call(call func(*csdk.Stmt) error) error {
	if stmt == nil {
		return ErrClosed
	}
	stmt.mu.Lock()
	defer stmt.mu.Unlock()
	if stmt.native == nil {
		return ErrClosed
	}
	if stmt.rows != 0 {
		return ErrBusy
	}
	return publicError(call(stmt.native))
}

func (stmt *Stmt) releaseRow(closeStatement bool) error {
	stmt.mu.Lock()
	defer stmt.mu.Unlock()
	if stmt.rows > 0 {
		stmt.rows--
	}
	if !closeStatement || stmt.native == nil {
		return nil
	}
	if stmt.rows != 0 {
		return ErrBusy
	}
	native := stmt.native
	stmt.native = nil
	err := publicError(native.Close())
	if stmt.registered {
		stmt.registered = false
		stmt.conn.releaseChild()
	}
	return err
}
