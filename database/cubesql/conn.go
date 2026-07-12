package cubesql

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"

	core "github.com/jedt3d/cubesql-go-driver/cubesql"
)

var (
	ErrReadOnlyTransaction  = errors.New("cubesql database/sql: read-only transactions are unsupported")
	ErrTransactionIsolation = errors.New("cubesql database/sql: non-default isolation is unsupported")
	ErrNamedParameters      = errors.New("cubesql database/sql: named parameters are unsupported")
)

type conn struct {
	mu       sync.Mutex
	native   *core.Conn
	database string
	activeTx *core.Tx
	closed   bool
	broken   bool
}

func (connection *conn) Prepare(query string) (driver.Stmt, error) {
	return connection.PrepareContext(context.Background(), query)
}

func (connection *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if err := connection.usableLocked(); err != nil {
		return nil, err
	}
	prepared, err := connection.native.PrepareContext(ctx, query)
	if err != nil {
		connection.observeLocked(err)
		return nil, connection.safeRetryErrorLocked(err)
	}
	if err := prepared.Close(); err != nil {
		connection.observeLocked(err)
		return nil, connection.safeRetryErrorLocked(err)
	}
	return &stmt{connection: connection, query: query}, nil
}

func (connection *conn) Close() error {
	if connection == nil {
		return nil
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.closed {
		return nil
	}
	var rollbackError error
	if connection.activeTx != nil {
		rollbackError = connection.activeTx.RollbackContext(context.Background())
		connection.activeTx = nil
	}
	closeError := connection.native.Close()
	if closeError == nil {
		connection.native = nil
		connection.closed = true
	}
	return errors.Join(rollbackError, closeError)
}

func (connection *conn) Begin() (driver.Tx, error) {
	return connection.BeginTx(context.Background(), driver.TxOptions{})
}

func (connection *conn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	if options.ReadOnly {
		return nil, ErrReadOnlyTransaction
	}
	if options.Isolation != driver.IsolationLevel(0) {
		return nil, ErrTransactionIsolation
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if err := connection.usableLocked(); err != nil {
		return nil, err
	}
	if connection.activeTx != nil {
		return nil, core.ErrBusy
	}
	native, err := connection.native.BeginContext(ctx)
	if err != nil {
		connection.observeLocked(err)
		return nil, err
	}
	connection.activeTx = native
	return &transaction{connection: connection, native: native}, nil
}

func (connection *conn) Ping(ctx context.Context) error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if err := connection.usableLocked(); err != nil {
		return err
	}
	err := connection.native.PingContext(ctx)
	connection.observeLocked(err)
	return connection.safeRetryErrorLocked(err)
}

func (connection *conn) IsValid() bool {
	if connection == nil {
		return false
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return !connection.closed && !connection.broken && connection.native != nil
}

func (connection *conn) ResetSession(ctx context.Context) error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.closed || connection.native == nil || connection.broken {
		return driver.ErrBadConn
	}
	if connection.activeTx != nil {
		if err := connection.activeTx.RollbackContext(ctx); err != nil {
			connection.broken = true
			return driver.ErrBadConn
		}
		connection.activeTx = nil
	}
	if err := connection.native.RollbackContext(ctx); err != nil && !isNoDatabaseSelected(err) {
		connection.broken = true
		return driver.ErrBadConn
	}
	if err := connection.native.SetDatabaseContext(ctx, connection.database); err != nil {
		connection.broken = true
		return driver.ErrBadConn
	}
	return nil
}

func isNoDatabaseSelected(err error) bool {
	var sdkError *core.Error
	return errors.As(err, &sdkError) && sdkError.Code == 7030
}

func (connection *conn) ExecContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	values, err := namedValues(arguments)
	if err != nil {
		return nil, err
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if err := connection.usableLocked(); err != nil {
		return nil, err
	}
	result, err := connection.native.ExecContext(ctx, query, values...)
	if err != nil {
		connection.observeLocked(err)
		return nil, err
	}
	if connection.activeTx == nil {
		if err := connection.native.CommitContext(context.Background()); err != nil {
			connection.observeLocked(err)
			return nil, err
		}
	}
	return execResult{native: result}, nil
}

func (connection *conn) QueryContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	values, err := namedValues(arguments)
	if err != nil {
		return nil, err
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if err := connection.usableLocked(); err != nil {
		return nil, err
	}
	native, err := connection.native.QueryContext(ctx, query, values...)
	if err != nil {
		connection.observeLocked(err)
		return nil, err
	}
	return newRows(connection, native), nil
}

func (connection *conn) CheckNamedValue(value *driver.NamedValue) error {
	return checkNamedValue(value)
}

func (connection *conn) usableLocked() error {
	if connection == nil || connection.closed || connection.native == nil {
		return driver.ErrBadConn
	}
	if connection.broken {
		return driver.ErrBadConn
	}
	return nil
}

func (connection *conn) observe(err error) {
	if connection == nil || err == nil {
		return
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	connection.observeLocked(err)
}

func (connection *conn) observeLocked(err error) {
	if errors.Is(err, core.ErrClosed) || errors.Is(err, core.ErrNetwork) ||
		errors.Is(err, core.ErrProtocol) || errors.Is(err, core.ErrTimeout) {
		connection.broken = true
	}
}

func (connection *conn) safeRetryErrorLocked(err error) error {
	if err == nil {
		return nil
	}
	if connection.broken {
		return driver.ErrBadConn
	}
	return err
}

func checkNamedValue(value *driver.NamedValue) error {
	if value == nil {
		return core.ErrInvalidArgument
	}
	if value.Name != "" {
		return ErrNamedParameters
	}
	if _, ok := value.Value.(core.ZeroBlob); ok {
		return nil
	}
	converted, err := driver.DefaultParameterConverter.ConvertValue(value.Value)
	if err != nil {
		return err
	}
	switch converted := converted.(type) {
	case nil, int64, float64, string, []byte:
		value.Value = converted
		return nil
	case bool:
		if converted {
			value.Value = int64(1)
		} else {
			value.Value = int64(0)
		}
		return nil
	default:
		return fmt.Errorf("cubesql database/sql: unsupported parameter type %T", converted)
	}
}

func namedValues(arguments []driver.NamedValue) ([]any, error) {
	values := make([]any, len(arguments))
	for index := range arguments {
		argument := arguments[index]
		if err := checkNamedValue(&argument); err != nil {
			return nil, err
		}
		values[index] = argument.Value
	}
	return values, nil
}
