package cubesql

import (
	"context"
	"sync"

	"github.com/jedt3d/cubesql-go-driver/internal/csdk"
)

// Conn owns one physical CubeSQL connection. Calls are serialized by the
// private native layer. Conn does not implement pooling.
type Conn struct {
	mu       sync.RWMutex
	native   *csdk.Conn
	txActive bool
	children int
}

func Open(options Options) (*Conn, error) {
	return OpenContext(context.Background(), options)
}

// OpenContext checks ctx before entering the blocking SDK connect call. SDK
// connect has its own timeout but cannot be interrupted by context cancellation.
func OpenContext(ctx context.Context, options Options) (*Conn, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	nativeOptions, err := options.native()
	if err != nil {
		return nil, err
	}
	native, err := csdk.Open(nativeOptions)
	if err != nil {
		return nil, publicError(err)
	}
	return &Conn{native: native}, nil
}

func (conn *Conn) Close() error {
	if conn == nil {
		return nil
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.native == nil {
		return nil
	}
	if conn.txActive || conn.children != 0 {
		return ErrBusy
	}
	if err := publicError(conn.native.Close()); err != nil {
		return err
	}
	conn.native = nil
	return nil
}

func (conn *Conn) Ping() error {
	return conn.PingContext(context.Background())
}

func (conn *Conn) PingContext(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	return conn.call(func(native *csdk.Conn) error {
		if err := contextError(ctx); err != nil {
			return err
		}
		return native.Ping()
	})
}

func (conn *Conn) SetDatabase(name string) error {
	return conn.SetDatabaseContext(context.Background(), name)
}

func (conn *Conn) SetDatabaseContext(ctx context.Context, name string) error {
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
	if conn.txActive || conn.children != 0 {
		return ErrBusy
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	return publicError(conn.native.SetDatabase(name))
}

func (conn *Conn) Exec(query string, args ...any) (Result, error) {
	return conn.ExecContext(context.Background(), query, args...)
}

func (conn *Conn) ExecContext(ctx context.Context, query string, args ...any) (Result, error) {
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	if query == "" {
		return Result{}, ErrInvalidArgument
	}
	if conn == nil {
		return Result{}, ErrClosed
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.native == nil {
		return Result{}, ErrClosed
	}
	if conn.children != 0 {
		return Result{}, ErrBusy
	}
	if len(args) == 0 {
		if err := contextError(ctx); err != nil {
			return Result{}, err
		}
		if err := conn.native.Exec(query); err != nil {
			return Result{}, publicError(err)
		}
		return resultForContext(ctx, conn.native), nil
	}
	if hasEmptyBlob(args) {
		if err := contextError(ctx); err != nil {
			return Result{}, err
		}
		stmt, err := conn.native.Prepare(query)
		if err != nil {
			return Result{}, publicError(err)
		}
		defer stmt.Close()
		for index, value := range args {
			if err := contextError(ctx); err != nil {
				return Result{}, err
			}
			if err := setPrepared(stmt, index+1, value); err != nil {
				return Result{}, err
			}
		}
		if err := contextError(ctx); err != nil {
			return Result{}, err
		}
		if err := stmt.Exec(); err != nil {
			return Result{}, publicError(err)
		}
		return resultForContext(ctx, conn.native), nil
	}
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	bind, err := csdk.NewBind(len(args))
	if err != nil {
		return Result{}, publicError(err)
	}
	defer bind.Close()
	for index, value := range args {
		if err := contextError(ctx); err != nil {
			return Result{}, err
		}
		if err := setOneShot(bind, index+1, value); err != nil {
			return Result{}, err
		}
	}
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	if err := conn.native.ExecBind(query, bind); err != nil {
		return Result{}, publicError(err)
	}
	return resultForContext(ctx, conn.native), nil
}

func (conn *Conn) Query(query string, args ...any) (*Rows, error) {
	return conn.QueryContext(context.Background(), query, args...)
}

func (conn *Conn) QueryContext(ctx context.Context, query string, args ...any) (*Rows, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if query == "" {
		return nil, ErrInvalidArgument
	}
	if len(args) == 0 {
		if conn == nil {
			return nil, ErrClosed
		}
		conn.mu.Lock()
		defer conn.mu.Unlock()
		if conn.native == nil {
			return nil, ErrClosed
		}
		if conn.children != 0 {
			return nil, ErrBusy
		}
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		native, err := conn.native.Query(query)
		if err != nil {
			return nil, publicError(err)
		}
		rows, err := newRowsContext(ctx, native)
		if err != nil {
			return nil, err
		}
		conn.children++
		rows.parentConn = conn
		return rows, nil
	}
	stmt, err := conn.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	for index, value := range args {
		if err := contextError(ctx); err != nil {
			stmt.Close()
			return nil, err
		}
		if err := stmt.Bind(index+1, value); err != nil {
			stmt.Close()
			return nil, err
		}
	}
	if err := contextError(ctx); err != nil {
		stmt.Close()
		return nil, err
	}
	rows, err := stmt.query(ctx, true)
	if err != nil {
		stmt.Close()
		return nil, err
	}
	return rows, nil
}

func (conn *Conn) Prepare(query string) (*Stmt, error) {
	return conn.PrepareContext(context.Background(), query)
}

func (conn *Conn) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if query == "" {
		return nil, ErrInvalidArgument
	}
	if conn == nil {
		return nil, ErrClosed
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.native == nil {
		return nil, ErrClosed
	}
	if conn.children != 0 {
		return nil, ErrBusy
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	native, err := conn.native.Prepare(query)
	if err != nil {
		return nil, publicError(err)
	}
	conn.children++
	return &Stmt{conn: conn, native: native, queryText: query, registered: true}, nil
}

func (conn *Conn) AffectedRows() (int64, error) {
	return conn.metric(func(native *csdk.Conn) (int64, error) { return native.AffectedRows() })
}

func (conn *Conn) LastInsertID() (int64, error) {
	return conn.metric(func(native *csdk.Conn) (int64, error) { return native.LastInsertID() })
}

func (conn *Conn) Changes() (int64, error) {
	return conn.metric(func(native *csdk.Conn) (int64, error) { return native.Changes() })
}

func (conn *Conn) call(call func(*csdk.Conn) error) error {
	if conn == nil {
		return ErrClosed
	}
	conn.mu.RLock()
	defer conn.mu.RUnlock()
	if conn.native == nil {
		return ErrClosed
	}
	return publicError(call(conn.native))
}

func (conn *Conn) metric(metric func(*csdk.Conn) (int64, error)) (int64, error) {
	if conn == nil {
		return 0, ErrClosed
	}
	conn.mu.RLock()
	defer conn.mu.RUnlock()
	if conn.native == nil {
		return 0, ErrClosed
	}
	if conn.children != 0 {
		return 0, ErrBusy
	}
	value, err := metric(conn.native)
	return value, publicError(err)
}

func resultFor(native *csdk.Conn) Result {
	return resultForContext(context.Background(), native)
}

func resultForContext(ctx context.Context, native *csdk.Conn) Result {
	if err := contextError(ctx); err != nil {
		return Result{rowsError: err, lastError: err}
	}
	rowsAffected, rowsError := native.AffectedRows()
	if err := contextError(ctx); err != nil {
		return Result{rowsAffected: rowsAffected, rowsError: publicError(rowsError), lastError: err}
	}
	lastInsertID, lastError := native.LastInsertID()
	return Result{
		rowsAffected: rowsAffected,
		rowsError:    publicError(rowsError),
		lastInsertID: lastInsertID,
		lastError:    publicError(lastError),
	}
}

func hasEmptyBlob(args []any) bool {
	for _, arg := range args {
		if value, ok := arg.([]byte); ok && len(value) == 0 {
			return true
		}
	}
	return false
}

func (conn *Conn) addChild() error {
	if conn == nil {
		return ErrClosed
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.native == nil {
		return ErrClosed
	}
	conn.children++
	return nil
}

func (conn *Conn) releaseChild() {
	if conn == nil {
		return
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.children > 0 {
		conn.children--
	}
}
