package cubesql

import (
	"database/sql/driver"
	"io"
	"math"
	"reflect"
	"strconv"

	core "github.com/jedt3d/cubesql-go-driver/cubesql"
)

var (
	int64Type   = reflect.TypeOf(int64(0))
	float64Type = reflect.TypeOf(float64(0))
	stringType  = reflect.TypeOf("")
	bytesType   = reflect.TypeOf([]byte{})
	anyType     = reflect.TypeOf((*any)(nil)).Elem()
)

type rows struct {
	connection *conn
	native     *core.Rows
	columns    []string
	types      []core.ColumnType
}

func newRows(connection *conn, native *core.Rows) *rows {
	return &rows{
		connection: connection,
		native:     native,
		columns:    native.Columns(),
		types:      native.ColumnTypes(),
	}
}

func (result *rows) Columns() []string {
	return append([]string(nil), result.columns...)
}

func (result *rows) Close() error {
	if result == nil || result.native == nil {
		return nil
	}
	err := result.native.Close()
	result.native = nil
	result.connection.observe(err)
	return err
}

func (result *rows) Next(destination []driver.Value) error {
	if result == nil || result.native == nil {
		return io.EOF
	}
	if len(destination) != len(result.columns) {
		return core.ErrScan
	}
	if !result.native.Next() {
		err := result.native.Err()
		if err == nil {
			return io.EOF
		}
		result.connection.observe(err)
		return err
	}
	for index := range destination {
		value, err := result.native.Value(index + 1)
		if err != nil {
			result.connection.observe(err)
			return err
		}
		destination[index], err = driverValue(value)
		if err != nil {
			return err
		}
	}
	return nil
}

func driverValue(value core.Value) (driver.Value, error) {
	if value.Null {
		return nil, nil
	}
	switch value.Type {
	case core.TypeInteger, core.TypeBoolean:
		parsed, err := strconv.ParseInt(string(value.Raw), 10, 64)
		if err != nil {
			return nil, core.ErrScan
		}
		return parsed, nil
	case core.TypeFloat, core.TypeCurrency:
		parsed, err := strconv.ParseFloat(string(value.Raw), 64)
		if err != nil {
			return nil, core.ErrScan
		}
		return parsed, nil
	case core.TypeBlob:
		copied := make([]byte, len(value.Raw))
		copy(copied, value.Raw)
		return copied, nil
	default:
		return string(value.Raw), nil
	}
}

func (result *rows) ColumnTypeDatabaseTypeName(index int) string {
	if index < 0 || index >= len(result.types) {
		return ""
	}
	switch result.types[index] {
	case core.TypeInteger:
		return "INTEGER"
	case core.TypeFloat:
		return "FLOAT"
	case core.TypeText:
		return "TEXT"
	case core.TypeBlob:
		return "BLOB"
	case core.TypeBoolean:
		return "BOOLEAN"
	case core.TypeDate:
		return "DATE"
	case core.TypeTime:
		return "TIME"
	case core.TypeTimestamp:
		return "TIMESTAMP"
	case core.TypeCurrency:
		return "CURRENCY"
	default:
		return "NULL"
	}
}

func (result *rows) ColumnTypeScanType(index int) reflect.Type {
	if index < 0 || index >= len(result.types) {
		return anyType
	}
	switch result.types[index] {
	case core.TypeInteger, core.TypeBoolean:
		return int64Type
	case core.TypeFloat, core.TypeCurrency:
		return float64Type
	case core.TypeBlob:
		return bytesType
	case core.TypeText, core.TypeDate, core.TypeTime, core.TypeTimestamp:
		return stringType
	default:
		return anyType
	}
}

func (result *rows) ColumnTypeLength(index int) (int64, bool) {
	if index < 0 || index >= len(result.types) {
		return 0, false
	}
	if result.types[index] == core.TypeText || result.types[index] == core.TypeBlob {
		return math.MaxInt64, true
	}
	return 0, false
}

func (result *rows) ColumnTypeNullable(index int) (bool, bool) {
	return false, false
}
