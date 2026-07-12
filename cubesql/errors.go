// Package cubesql provides the safe public CubeSQL core API.
//
// Native handles and C SDK constants remain private in internal/csdk. All
// returned text and BLOB values are copied into Go-owned memory.
package cubesql

import (
	"errors"
	"fmt"

	"github.com/jedt3d/cubesql-go-driver/internal/csdk"
)

var (
	ErrClosed          = errors.New("cubesql: handle is closed")
	ErrBusy            = errors.New("cubesql: handle has active children or transaction")
	ErrInvalidArgument = errors.New("cubesql: invalid argument")
	ErrUnsupported     = errors.New("cubesql: operation is not safely supported")
	ErrScan            = errors.New("cubesql: cannot scan value into destination")
	ErrTxDone          = errors.New("cubesql: transaction has already completed")
)

// Error is a copied error reported by the CubeSQL SDK. Code is retained for
// diagnostics; callers should use errors.As rather than depend on SDK numbers.
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("cubesql: SDK error %d", e.Code)
	}
	return fmt.Sprintf("cubesql: SDK error %d: %s", e.Code, e.Message)
}

func publicError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, csdk.ErrClosed):
		return ErrClosed
	case errors.Is(err, csdk.ErrBusy):
		return ErrBusy
	case errors.Is(err, csdk.ErrInvalidArgument):
		return ErrInvalidArgument
	case errors.Is(err, csdk.ErrUnsupported):
		return ErrUnsupported
	}
	var native *csdk.Error
	if errors.As(err, &native) {
		return &Error{Code: native.Code, Message: native.Message}
	}
	return err
}
