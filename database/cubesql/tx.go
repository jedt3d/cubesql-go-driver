package cubesql

import (
	"context"
	"errors"
	"sync"

	core "github.com/jedt3d/cubesql-go-driver/cubesql"
)

type transaction struct {
	mu         sync.Mutex
	connection *conn
	native     *core.Tx
	done       bool
}

func (transaction *transaction) Commit() error {
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.done || transaction.connection == nil || transaction.native == nil {
		return core.ErrTxDone
	}
	err := transaction.native.CommitContext(context.Background())
	if err != nil {
		rollbackError := transaction.native.RollbackContext(context.Background())
		transaction.connection.finishTransaction(transaction.native, true, rollbackError == nil)
		transaction.done = true
		return errors.Join(err, rollbackError)
	}
	transaction.connection.finishTransaction(transaction.native, false, true)
	transaction.done = true
	return nil
}

func (transaction *transaction) Rollback() error {
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.done || transaction.connection == nil || transaction.native == nil {
		return core.ErrTxDone
	}
	err := transaction.native.RollbackContext(context.Background())
	transaction.connection.finishTransaction(transaction.native, err != nil, err == nil)
	transaction.done = true
	return err
}

func (connection *conn) finishTransaction(native *core.Tx, broken, clear bool) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.activeTx == native && clear {
		connection.activeTx = nil
	}
	if broken {
		connection.broken = true
	}
}
