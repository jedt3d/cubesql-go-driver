// Package cubesql provides the safe public CubeSQL core API.
//
// Native handles and C SDK constants remain private in internal/csdk. All
// returned text and BLOB values are copied into Go-owned memory. Context-aware
// methods check cancellation before native calls but cannot interrupt a native
// call already in progress.
package cubesql

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jedt3d/cubesql-go-driver/internal/csdk"
)

var (
	ErrClosed          = errors.New("cubesql: handle is closed")
	ErrBusy            = errors.New("cubesql: handle has active children or transaction")
	ErrInvalidArgument = errors.New("cubesql: invalid argument")
	ErrUnsupported     = errors.New("cubesql: operation is not safely supported")
	ErrScan            = errors.New("cubesql: cannot scan value into destination")
	ErrTxDone          = errors.New("cubesql: transaction has already completed")
	ErrAuthentication  = errors.New("cubesql: authentication failed")
	ErrAuthorization   = errors.New("cubesql: authorization failed")
	ErrNetwork         = errors.New("cubesql: network failure")
	ErrProtocol        = errors.New("cubesql: protocol failure")
	ErrServer          = errors.New("cubesql: server SQL failure")
	ErrTimeout         = errors.New("cubesql: timeout")
)

type ErrorKind uint8

const (
	ErrorUnknown ErrorKind = iota
	ErrorAuthentication
	ErrorAuthorization
	ErrorNetwork
	ErrorProtocol
	ErrorServer
	ErrorTimeout
)

func (kind ErrorKind) String() string {
	switch kind {
	case ErrorAuthentication:
		return "authentication"
	case ErrorAuthorization:
		return "authorization"
	case ErrorNetwork:
		return "network"
	case ErrorProtocol:
		return "protocol"
	case ErrorServer:
		return "server"
	case ErrorTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// Error is a copied error reported by the CubeSQL SDK. Code is retained for
// diagnostics; callers should use errors.As rather than depend on SDK numbers.
type Error struct {
	Code    int
	Message string
	Kind    ErrorKind
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("cubesql: SDK error %d", e.Code)
	}
	return fmt.Sprintf("cubesql: SDK error %d: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	switch e.Kind {
	case ErrorAuthentication:
		return ErrAuthentication
	case ErrorAuthorization:
		return ErrAuthorization
	case ErrorNetwork:
		return ErrNetwork
	case ErrorProtocol:
		return ErrProtocol
	case ErrorServer:
		return ErrServer
	case ErrorTimeout:
		return ErrTimeout
	default:
		return nil
	}
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
		return &Error{Code: native.Code, Message: native.Message, Kind: classifyError(native.Code, native.Message)}
	}
	return err
}

func classifyError(code int, message string) ErrorKind {
	lower := strings.ToLower(message)
	if code == 7056 || strings.Contains(lower, "authentication failed") ||
		strings.Contains(lower, "invalid password") || strings.Contains(lower, "invalid username") {
		return ErrorAuthentication
	}
	if code == 810 || strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") {
		return ErrorTimeout
	}
	if code == 800 || code == 802 || code == 805 || code == 820 || code == 830 || code == 888 ||
		code == -6 || code == -7 || code == -8 || strings.Contains(lower, "csql_socket") ||
		strings.Contains(lower, "sock_read") || strings.Contains(lower, "sock_write") ||
		strings.Contains(lower, "getaddrinfo") || strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection refused") || strings.Contains(lower, "broken pipe") {
		return ErrorNetwork
	}
	if (code >= 835 && code <= 870) || code == -4 || code == -5 ||
		strings.Contains(lower, "protocol") || strings.Contains(lower, "signature header") {
		return ErrorProtocol
	}
	if code > 0 {
		if strings.Contains(lower, "not authorized") || strings.Contains(lower, "permission") ||
			strings.Contains(lower, "privilege") || strings.Contains(lower, "access denied") {
			return ErrorAuthorization
		}
		return ErrorServer
	}
	return ErrorUnknown
}
