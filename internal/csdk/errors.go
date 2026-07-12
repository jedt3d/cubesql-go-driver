package csdk

import (
	"errors"
	"fmt"
)

var (
	ErrClosed          = errors.New("cubesql: native handle is closed")
	ErrBusy            = errors.New("cubesql: connection has active native children")
	ErrInvalidArgument = errors.New("cubesql: invalid argument")
	ErrUnsupported     = errors.New("cubesql: operation is not safely supported by this SDK/server combination")
)

// Error is a copied CubeSQL SDK error. It does not reference native memory.
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
