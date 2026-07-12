package cubesql

import (
	"errors"
	"strconv"
	"sync"

	"github.com/jedt3d/cubesql-go-driver/internal/csdk"
)

type ColumnType uint8

const (
	TypeNone ColumnType = iota
	TypeInteger
	TypeFloat
	TypeText
	TypeBlob
	TypeBoolean
	TypeDate
	TypeTime
	TypeTimestamp
	TypeCurrency
)

type Value struct {
	Type ColumnType
	Raw  []byte
	Null bool
}

type NullInt64 struct {
	Int64 int64
	Valid bool
}

type NullFloat64 struct {
	Float64 float64
	Valid   bool
}

type NullString struct {
	String string
	Valid  bool
}

type NullBytes struct {
	Bytes []byte
	Valid bool
}

// Rows owns one native cursor. Next and Seek use one-based native row numbers;
// Scan always copies values into caller-owned Go storage.
type Rows struct {
	mu         sync.Mutex
	native     *csdk.Rows
	parentConn *Conn
	parentStmt *Stmt
	closeStmt  bool
	columns    []string
	types      []ColumnType
	total      int
	current    int
	err        error
}

func newRows(native *csdk.Rows) (*Rows, error) {
	total, err := native.NumRows()
	if err != nil {
		native.Close()
		return nil, publicError(err)
	}
	count, err := native.NumColumns()
	if err != nil {
		native.Close()
		return nil, publicError(err)
	}
	rows := &Rows{
		native:  native,
		columns: make([]string, count),
		types:   make([]ColumnType, count),
		total:   total,
	}
	for index := 1; index <= count; index++ {
		name, err := native.ColumnName(index)
		if err != nil {
			rows.Close()
			return nil, publicError(err)
		}
		columnType, err := native.ColumnType(index)
		if err != nil {
			rows.Close()
			return nil, publicError(err)
		}
		rows.columns[index-1] = name
		rows.types[index-1] = ColumnType(columnType)
	}
	return rows, nil
}

func (rows *Rows) Close() error {
	if rows == nil {
		return nil
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	if rows.native == nil {
		return nil
	}
	native := rows.native
	parentStmt := rows.parentStmt
	parentConn := rows.parentConn
	closeStmt := rows.closeStmt
	rows.native = nil
	rows.parentConn = nil
	rows.parentStmt = nil
	rows.closeStmt = false
	nativeError := publicError(native.Close())
	if parentConn != nil {
		parentConn.releaseChild()
	}
	var statementError error
	if parentStmt != nil {
		statementError = parentStmt.releaseRow(closeStmt)
	}
	return errors.Join(nativeError, statementError)
}

func (rows *Rows) Columns() []string {
	if rows == nil {
		return nil
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	return append([]string(nil), rows.columns...)
}

func (rows *Rows) ColumnTypes() []ColumnType {
	if rows == nil {
		return nil
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	return append([]ColumnType(nil), rows.types...)
}

func (rows *Rows) NumRows() int {
	if rows == nil {
		return 0
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	return rows.total
}

func (rows *Rows) Next() bool {
	if rows == nil {
		return false
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	if rows.native == nil {
		if rows.err == nil {
			rows.err = ErrClosed
		}
		return false
	}
	if rows.err != nil || rows.current >= rows.total {
		return false
	}
	row := rows.current + 1
	if err := rows.native.Seek(row); err != nil {
		rows.err = publicError(err)
		return false
	}
	rows.current = row
	return true
}

func (rows *Rows) Seek(row int) error {
	if row <= 0 {
		return ErrInvalidArgument
	}
	if rows == nil {
		return ErrClosed
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	if rows.native == nil {
		return ErrClosed
	}
	if err := rows.native.Seek(row); err != nil {
		return publicError(err)
	}
	rows.current = row
	rows.err = nil
	return nil
}

func (rows *Rows) Err() error {
	if rows == nil {
		return ErrClosed
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	return rows.err
}

func (rows *Rows) Value(column int) (Value, error) {
	if rows == nil {
		return Value{}, ErrClosed
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	return rows.valueLocked(column)
}

func (rows *Rows) Scan(destinations ...any) error {
	if rows == nil {
		return ErrClosed
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	if rows.native == nil {
		return ErrClosed
	}
	if rows.current <= 0 || rows.current > rows.total {
		return ErrScan
	}
	if len(destinations) != len(rows.columns) {
		return ErrScan
	}
	for index, destination := range destinations {
		value, err := rows.valueLocked(index + 1)
		if err != nil {
			return err
		}
		if err := scanValue(value, destination); err != nil {
			return err
		}
	}
	return nil
}

func (rows *Rows) valueLocked(column int) (Value, error) {
	if rows.native == nil {
		return Value{}, ErrClosed
	}
	if rows.current <= 0 || rows.current > rows.total || column <= 0 || column > len(rows.columns) {
		return Value{}, ErrInvalidArgument
	}
	raw, isNull, err := rows.native.Field(rows.current, column)
	if err != nil {
		return Value{}, publicError(err)
	}
	return Value{Type: rows.types[column-1], Raw: raw, Null: isNull}, nil
}

func scanValue(value Value, destination any) error {
	if destination == nil {
		return ErrScan
	}
	if value.Null {
		switch destination := destination.(type) {
		case *any:
			*destination = nil
		case *[]byte:
			*destination = nil
		case *NullInt64:
			*destination = NullInt64{}
		case *NullFloat64:
			*destination = NullFloat64{}
		case *NullString:
			*destination = NullString{}
		case *NullBytes:
			*destination = NullBytes{}
		case *Value:
			*destination = value
		default:
			return ErrScan
		}
		return nil
	}

	switch destination := destination.(type) {
	case *any:
		converted, err := valueInterface(value)
		if err != nil {
			return err
		}
		*destination = converted
	case *int64:
		parsed, err := strconv.ParseInt(string(value.Raw), 10, 64)
		if err != nil {
			return ErrScan
		}
		*destination = parsed
	case *float64:
		parsed, err := strconv.ParseFloat(string(value.Raw), 64)
		if err != nil {
			return ErrScan
		}
		*destination = parsed
	case *string:
		*destination = string(value.Raw)
	case *[]byte:
		*destination = append((*destination)[:0], value.Raw...)
	case *NullInt64:
		parsed, err := strconv.ParseInt(string(value.Raw), 10, 64)
		if err != nil {
			return ErrScan
		}
		*destination = NullInt64{Int64: parsed, Valid: true}
	case *NullFloat64:
		parsed, err := strconv.ParseFloat(string(value.Raw), 64)
		if err != nil {
			return ErrScan
		}
		*destination = NullFloat64{Float64: parsed, Valid: true}
	case *NullString:
		*destination = NullString{String: string(value.Raw), Valid: true}
	case *NullBytes:
		*destination = NullBytes{Bytes: append([]byte(nil), value.Raw...), Valid: true}
	case *Value:
		value.Raw = append([]byte(nil), value.Raw...)
		*destination = value
	default:
		return ErrScan
	}
	return nil
}

func valueInterface(value Value) (any, error) {
	switch value.Type {
	case TypeInteger, TypeBoolean:
		parsed, err := strconv.ParseInt(string(value.Raw), 10, 64)
		if err != nil {
			return nil, ErrScan
		}
		return parsed, nil
	case TypeFloat, TypeCurrency:
		parsed, err := strconv.ParseFloat(string(value.Raw), 64)
		if err != nil {
			return nil, ErrScan
		}
		return parsed, nil
	case TypeBlob:
		return append([]byte(nil), value.Raw...), nil
	default:
		return string(value.Raw), nil
	}
}
