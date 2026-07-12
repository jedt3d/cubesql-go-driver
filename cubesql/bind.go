package cubesql

import (
	"math"

	"github.com/jedt3d/cubesql-go-driver/internal/csdk"
)

// ZeroBlob requests a BLOB containing Size zero bytes. It is supported by
// prepared statements only; the pinned SDK one-shot bind path does not preserve
// the requested length.
type ZeroBlob struct {
	Size int
}

func setOneShot(bind *csdk.Bind, index int, value any) error {
	var err error
	switch value := value.(type) {
	case nil:
		err = bind.SetNull(index)
	case int64:
		err = bind.SetInt64(index, value)
	case float64:
		err = bind.SetDouble(index, value)
	case string:
		err = bind.SetText(index, value)
	case []byte:
		err = bind.SetBlob(index, value)
	case ZeroBlob:
		err = bind.SetZeroBlob(index, value.Size)
	default:
		return ErrInvalidArgument
	}
	return publicError(err)
}

func setPrepared(stmt *csdk.Stmt, index int, value any) error {
	var err error
	switch value := value.(type) {
	case nil:
		err = stmt.BindNull(index)
	case int64:
		err = stmt.BindInt64(index, value)
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return ErrInvalidArgument
		}
		err = stmt.BindDouble(index, value)
	case string:
		err = stmt.BindText(index, value)
	case []byte:
		if len(value) == 0 {
			err = stmt.BindZeroBlob(index, 0)
		} else {
			err = stmt.BindBlob(index, value)
		}
	case ZeroBlob:
		err = stmt.BindZeroBlob(index, value.Size)
	default:
		return ErrInvalidArgument
	}
	return publicError(err)
}
