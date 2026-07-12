package cubesql

import (
	"context"
	"database/sql/driver"
	"sync"

	core "github.com/jedt3d/cubesql-go-driver/cubesql"
)

type stmt struct {
	mu         sync.Mutex
	connection *conn
	query      string
	closed     bool
}

func (statement *stmt) Close() error {
	if statement == nil {
		return nil
	}
	statement.mu.Lock()
	defer statement.mu.Unlock()
	statement.closed = true
	statement.connection = nil
	return nil
}

func (statement *stmt) NumInput() int { return -1 }

func (statement *stmt) Exec(arguments []driver.Value) (driver.Result, error) {
	return statement.ExecContext(context.Background(), positionalValues(arguments))
}

func (statement *stmt) ExecContext(ctx context.Context, arguments []driver.NamedValue) (driver.Result, error) {
	statement.mu.Lock()
	defer statement.mu.Unlock()
	if statement.closed || statement.connection == nil {
		return nil, core.ErrClosed
	}
	return statement.connection.ExecContext(ctx, statement.query, arguments)
}

func (statement *stmt) Query(arguments []driver.Value) (driver.Rows, error) {
	return statement.QueryContext(context.Background(), positionalValues(arguments))
}

func (statement *stmt) QueryContext(ctx context.Context, arguments []driver.NamedValue) (driver.Rows, error) {
	statement.mu.Lock()
	defer statement.mu.Unlock()
	if statement.closed || statement.connection == nil {
		return nil, core.ErrClosed
	}
	return statement.connection.QueryContext(ctx, statement.query, arguments)
}

func (statement *stmt) CheckNamedValue(value *driver.NamedValue) error {
	return checkNamedValue(value)
}

func positionalValues(values []driver.Value) []driver.NamedValue {
	arguments := make([]driver.NamedValue, len(values))
	for index, value := range values {
		arguments[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	return arguments
}
