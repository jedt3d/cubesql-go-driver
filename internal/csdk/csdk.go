// Package csdk owns the private cgo boundary to the CubeSQL C SDK.
//
// Native handles never leave this package. Every retained buffer is C-owned,
// and cursor/error bytes are copied before a method releases its connection
// lock or native cursor.
package csdk

/*
#cgo CFLAGS: -DCUBESQL_DISABLE_SSL_ENCRYPTION -I${SRCDIR} -I${SRCDIR}/../../third_party/cubesql-sdk -I${SRCDIR}/../../third_party/cubesql-sdk/crypt
#cgo LDFLAGS: -lz
#include "bridge.h"
*/
import "C"

import (
	"math"
	"sync"
	"unsafe"
)

type Encryption int

const (
	EncryptionClear  Encryption = 0
	EncryptionAES256 Encryption = 4
)

type Options struct {
	Host       string
	Port       int
	Username   string
	Password   string
	Timeout    int
	Encryption Encryption
}

// Conn serializes all calls into one native CubeSQL connection.
type Conn struct {
	mu       sync.Mutex
	ptr      *C.csqlgo_conn
	children int
}

type Rows struct {
	conn *Conn
	ptr  *C.csqlgo_cursor
}

type Stmt struct {
	conn *Conn
	ptr  *C.csqlgo_stmt
}

// Bind owns a complete C allocation graph suitable for cubesql_bind.
type Bind struct {
	mu  sync.Mutex
	ptr *C.csqlgo_bind
}

func Version() string {
	return C.GoString(C.csqlgo_version())
}

func Open(options Options) (*Conn, error) {
	if options.Host == "" {
		options.Host = "localhost"
	}
	if options.Port == 0 {
		options.Port = 4430
	}
	if options.Timeout == 0 {
		options.Timeout = 12
	}
	if options.Username == "" || options.Port < 0 || options.Timeout < 0 {
		return nil, ErrInvalidArgument
	}
	if options.Encryption != EncryptionClear && options.Encryption != EncryptionAES256 {
		return nil, ErrInvalidArgument
	}

	host := C.CString(options.Host)
	username := C.CString(options.Username)
	password := C.CString(options.Password)
	defer C.csqlgo_free(unsafe.Pointer(host))
	defer C.csqlgo_free(unsafe.Pointer(username))
	defer C.csqlgo_free(unsafe.Pointer(password))

	var ptr *C.csqlgo_conn
	var message *C.char
	result := C.csqlgo_conn_open(
		&ptr,
		host,
		C.int(options.Port),
		username,
		password,
		C.int(options.Timeout),
		C.int(options.Encryption),
		&message,
	)
	if message != nil {
		defer C.csqlgo_free(unsafe.Pointer(message))
	}
	if result != C.CSQLGO_OK {
		text := ""
		if message != nil {
			text = C.GoString(message)
		}
		return nil, &Error{Code: int(result), Message: text}
	}
	return &Conn{ptr: ptr}, nil
}

func (conn *Conn) Close() error {
	if conn == nil {
		return nil
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.ptr == nil {
		return nil
	}
	if conn.children != 0 {
		return ErrBusy
	}
	ptr := conn.ptr
	conn.ptr = nil
	C.csqlgo_conn_close(ptr, 1)
	return nil
}

func (conn *Conn) Ping() error {
	return conn.call(func(ptr *C.csqlgo_conn) C.int {
		return C.csqlgo_conn_ping(ptr)
	})
}

func (conn *Conn) Exec(sql string) error {
	if sql == "" {
		return ErrInvalidArgument
	}
	csql := C.CString(sql)
	defer C.csqlgo_free(unsafe.Pointer(csql))
	return conn.call(func(ptr *C.csqlgo_conn) C.int {
		return C.csqlgo_conn_execute(ptr, csql)
	})
}

func (conn *Conn) SetDatabase(name string) error {
	var cname *C.char
	if name != "" {
		cname = C.CString(name)
		defer C.csqlgo_free(unsafe.Pointer(cname))
	}
	return conn.call(func(ptr *C.csqlgo_conn) C.int {
		return C.csqlgo_conn_set_database(ptr, cname)
	})
}

func (conn *Conn) Begin() error {
	return conn.call(func(ptr *C.csqlgo_conn) C.int {
		return C.csqlgo_conn_begin(ptr)
	})
}

func (conn *Conn) Commit() error {
	return conn.call(func(ptr *C.csqlgo_conn) C.int {
		return C.csqlgo_conn_commit(ptr)
	})
}

func (conn *Conn) Rollback() error {
	return conn.call(func(ptr *C.csqlgo_conn) C.int {
		return C.csqlgo_conn_rollback(ptr)
	})
}

func (conn *Conn) Changes() (int64, error) {
	return conn.metric(func(ptr *C.csqlgo_conn) C.int64_t {
		return C.csqlgo_conn_changes(ptr)
	})
}

func (conn *Conn) AffectedRows() (int64, error) {
	return conn.metric(func(ptr *C.csqlgo_conn) C.int64_t {
		return C.csqlgo_conn_affected_rows(ptr)
	})
}

func (conn *Conn) LastInsertID() (int64, error) {
	return conn.metric(func(ptr *C.csqlgo_conn) C.int64_t {
		return C.csqlgo_conn_last_insert_id(ptr)
	})
}

func (conn *Conn) Query(sql string) (*Rows, error) {
	if sql == "" {
		return nil, ErrInvalidArgument
	}
	csql := C.CString(sql)
	defer C.csqlgo_free(unsafe.Pointer(csql))
	if conn == nil {
		return nil, ErrClosed
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.ptr == nil {
		return nil, ErrClosed
	}
	var cursor *C.csqlgo_cursor
	result := C.csqlgo_conn_query(conn.ptr, csql, &cursor)
	if result != C.CSQLGO_OK {
		return nil, conn.nativeErrorLocked(int(result))
	}
	conn.children++
	return &Rows{conn: conn, ptr: cursor}, nil
}

func NewBind(count int) (*Bind, error) {
	if count <= 0 {
		return nil, ErrInvalidArgument
	}
	ptr := C.csqlgo_bind_new(C.int(count))
	if ptr == nil {
		return nil, &Error{Code: int(C.CSQLGO_ERR_MEMORY)}
	}
	return &Bind{ptr: ptr}, nil
}

func (bind *Bind) Close() error {
	if bind == nil {
		return nil
	}
	bind.mu.Lock()
	defer bind.mu.Unlock()
	if bind.ptr == nil {
		return nil
	}
	ptr := bind.ptr
	bind.ptr = nil
	C.csqlgo_bind_close(ptr)
	return nil
}

func (bind *Bind) SetInt64(index int, value int64) error {
	return bind.set(func(ptr *C.csqlgo_bind) C.int {
		return C.csqlgo_bind_set_int64(ptr, C.int(index), C.int64_t(value))
	})
}

func (bind *Bind) SetDouble(index int, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ErrInvalidArgument
	}
	return bind.set(func(ptr *C.csqlgo_bind) C.int {
		return C.csqlgo_bind_set_double(ptr, C.int(index), C.double(value))
	})
}

func (bind *Bind) SetText(index int, value string) error {
	return bind.setBytes(index, []byte(value), func(ptr *C.csqlgo_bind, index C.int, data unsafe.Pointer, length C.int) C.int {
		return C.csqlgo_bind_set_text(ptr, index, data, length)
	})
}

func (bind *Bind) SetBlob(index int, value []byte) error {
	if index <= 0 {
		return ErrInvalidArgument
	}
	if len(value) == 0 {
		return bind.unsupported()
	}
	return bind.setBytes(index, value, func(ptr *C.csqlgo_bind, index C.int, data unsafe.Pointer, length C.int) C.int {
		return C.csqlgo_bind_set_blob(ptr, index, data, length)
	})
}

func (bind *Bind) SetNull(index int) error {
	return bind.set(func(ptr *C.csqlgo_bind) C.int {
		return C.csqlgo_bind_set_null(ptr, C.int(index))
	})
}

func (bind *Bind) SetZeroBlob(index, length int) error {
	if index <= 0 || length < 0 {
		return ErrInvalidArgument
	}
	return bind.unsupported()
}

func (conn *Conn) ExecBind(sql string, bind *Bind) error {
	if sql == "" || bind == nil {
		return ErrInvalidArgument
	}
	csql := C.CString(sql)
	defer C.csqlgo_free(unsafe.Pointer(csql))
	if conn == nil {
		return ErrClosed
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.ptr == nil {
		return ErrClosed
	}
	bind.mu.Lock()
	defer bind.mu.Unlock()
	if bind.ptr == nil {
		return ErrClosed
	}
	result := C.csqlgo_conn_execute_bind(conn.ptr, csql, bind.ptr)
	if result != C.CSQLGO_OK {
		return conn.nativeErrorLocked(int(result))
	}
	return nil
}

func (conn *Conn) Prepare(sql string) (*Stmt, error) {
	if sql == "" {
		return nil, ErrInvalidArgument
	}
	csql := C.CString(sql)
	defer C.csqlgo_free(unsafe.Pointer(csql))
	if conn == nil {
		return nil, ErrClosed
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.ptr == nil {
		return nil, ErrClosed
	}
	var ptr *C.csqlgo_stmt
	result := C.csqlgo_conn_prepare(conn.ptr, csql, &ptr)
	if result != C.CSQLGO_OK {
		return nil, conn.nativeErrorLocked(int(result))
	}
	conn.children++
	return &Stmt{conn: conn, ptr: ptr}, nil
}

func (rows *Rows) Close() error {
	if rows == nil || rows.conn == nil {
		return nil
	}
	rows.conn.mu.Lock()
	defer rows.conn.mu.Unlock()
	if rows.ptr == nil {
		return nil
	}
	ptr := rows.ptr
	rows.ptr = nil
	C.csqlgo_cursor_close(ptr)
	rows.conn.children--
	return nil
}

func (rows *Rows) NumRows() (int, error) {
	return rows.number(func(ptr *C.csqlgo_cursor) C.int {
		return C.csqlgo_cursor_num_rows(ptr)
	})
}

func (rows *Rows) NumColumns() (int, error) {
	return rows.number(func(ptr *C.csqlgo_cursor) C.int {
		return C.csqlgo_cursor_num_columns(ptr)
	})
}

func (rows *Rows) ColumnType(column int) (int, error) {
	if column <= 0 {
		return 0, ErrInvalidArgument
	}
	return rows.number(func(ptr *C.csqlgo_cursor) C.int {
		return C.csqlgo_cursor_column_type(ptr, C.int(column))
	})
}

func (rows *Rows) Seek(row int) error {
	if row <= 0 {
		return ErrInvalidArgument
	}
	if rows == nil || rows.conn == nil {
		return ErrClosed
	}
	rows.conn.mu.Lock()
	defer rows.conn.mu.Unlock()
	if rows.ptr == nil || rows.conn.ptr == nil {
		return ErrClosed
	}
	if C.csqlgo_cursor_seek(rows.ptr, C.int(row)) == 0 {
		return ErrInvalidArgument
	}
	return nil
}

func (rows *Rows) Field(row, column int) ([]byte, bool, error) {
	if row <= 0 || column <= 0 {
		return nil, false, ErrInvalidArgument
	}
	return rows.copyField(func(ptr *C.csqlgo_cursor, out **C.uchar, length *C.int) C.int {
		return C.csqlgo_cursor_copy_field(ptr, C.int(row), C.int(column), out, length)
	})
}

func (rows *Rows) ColumnName(column int) (string, error) {
	if column <= 0 {
		return "", ErrInvalidArgument
	}
	value, isNull, err := rows.copyField(func(ptr *C.csqlgo_cursor, out **C.uchar, length *C.int) C.int {
		return C.csqlgo_cursor_copy_column_name(ptr, C.int(column), out, length)
	})
	if err != nil {
		return "", err
	}
	if isNull {
		return "", ErrInvalidArgument
	}
	return string(value), nil
}

func (stmt *Stmt) Close() error {
	if stmt == nil || stmt.conn == nil {
		return nil
	}
	stmt.conn.mu.Lock()
	defer stmt.conn.mu.Unlock()
	if stmt.ptr == nil {
		return nil
	}
	ptr := stmt.ptr
	stmt.ptr = nil
	result := C.csqlgo_stmt_close(ptr)
	stmt.conn.children--
	if result != C.CSQLGO_OK {
		return stmt.conn.nativeErrorLocked(int(result))
	}
	return nil
}

func (stmt *Stmt) BindInt64(index int, value int64) error {
	return stmt.call(func(ptr *C.csqlgo_stmt) C.int {
		return C.csqlgo_stmt_bind_int64(ptr, C.int(index), C.int64_t(value))
	})
}

func (stmt *Stmt) BindDouble(index int, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ErrInvalidArgument
	}
	return stmt.call(func(ptr *C.csqlgo_stmt) C.int {
		return C.csqlgo_stmt_bind_double(ptr, C.int(index), C.double(value))
	})
}

func (stmt *Stmt) BindText(index int, value string) error {
	return stmt.bindBytes(index, []byte(value), func(ptr *C.csqlgo_stmt, index C.int, data unsafe.Pointer, length C.int) C.int {
		return C.csqlgo_stmt_bind_text(ptr, index, data, length)
	})
}

func (stmt *Stmt) BindBlob(index int, value []byte) error {
	if index <= 0 {
		return ErrInvalidArgument
	}
	if len(value) == 0 {
		return stmt.unsupported()
	}
	return stmt.bindBytes(index, value, func(ptr *C.csqlgo_stmt, index C.int, data unsafe.Pointer, length C.int) C.int {
		return C.csqlgo_stmt_bind_blob(ptr, index, data, length)
	})
}

func (stmt *Stmt) BindNull(index int) error {
	return stmt.call(func(ptr *C.csqlgo_stmt) C.int {
		return C.csqlgo_stmt_bind_null(ptr, C.int(index))
	})
}

func (stmt *Stmt) BindZeroBlob(index, length int) error {
	if index <= 0 || length < 0 {
		return ErrInvalidArgument
	}
	return stmt.call(func(ptr *C.csqlgo_stmt) C.int {
		return C.csqlgo_stmt_bind_zeroblob(ptr, C.int(index), C.int(length))
	})
}

func (stmt *Stmt) Exec() error {
	return stmt.call(func(ptr *C.csqlgo_stmt) C.int {
		return C.csqlgo_stmt_execute(ptr)
	})
}

func (stmt *Stmt) Query() (*Rows, error) {
	if stmt == nil || stmt.conn == nil {
		return nil, ErrClosed
	}
	stmt.conn.mu.Lock()
	defer stmt.conn.mu.Unlock()
	if stmt.ptr == nil || stmt.conn.ptr == nil {
		return nil, ErrClosed
	}
	var cursor *C.csqlgo_cursor
	result := C.csqlgo_stmt_query(stmt.ptr, &cursor)
	if result != C.CSQLGO_OK {
		return nil, stmt.conn.nativeErrorLocked(int(result))
	}
	stmt.conn.children++
	return &Rows{conn: stmt.conn, ptr: cursor}, nil
}

func (conn *Conn) call(call func(*C.csqlgo_conn) C.int) error {
	if conn == nil {
		return ErrClosed
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.ptr == nil {
		return ErrClosed
	}
	result := call(conn.ptr)
	if result != C.CSQLGO_OK {
		return conn.nativeErrorLocked(int(result))
	}
	return nil
}

func (conn *Conn) metric(metric func(*C.csqlgo_conn) C.int64_t) (int64, error) {
	if conn == nil {
		return 0, ErrClosed
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.ptr == nil {
		return 0, ErrClosed
	}
	value := metric(conn.ptr)
	if code := int(C.csqlgo_conn_error_code(conn.ptr)); code != C.CSQLGO_OK {
		return 0, conn.nativeErrorLocked(code)
	}
	return int64(value), nil
}

func (conn *Conn) nativeErrorLocked(fallback int) error {
	code := int(C.csqlgo_conn_error_code(conn.ptr))
	if code == 0 || code == int(C.CSQLGO_ERR_INVALID) {
		code = fallback
	}
	message := C.csqlgo_conn_error_message_copy(conn.ptr)
	if message == nil {
		return &Error{Code: code}
	}
	defer C.csqlgo_free(unsafe.Pointer(message))
	return &Error{Code: code, Message: C.GoString(message)}
}

func (bind *Bind) set(setter func(*C.csqlgo_bind) C.int) error {
	if bind == nil {
		return ErrClosed
	}
	bind.mu.Lock()
	defer bind.mu.Unlock()
	if bind.ptr == nil {
		return ErrClosed
	}
	result := setter(bind.ptr)
	if result == C.CSQLGO_ERR_INVALID {
		return ErrInvalidArgument
	}
	if result != C.CSQLGO_OK {
		return &Error{Code: int(result)}
	}
	return nil
}

func (bind *Bind) unsupported() error {
	if bind == nil {
		return ErrClosed
	}
	bind.mu.Lock()
	defer bind.mu.Unlock()
	if bind.ptr == nil {
		return ErrClosed
	}
	return ErrUnsupported
}

func (bind *Bind) setBytes(index int, value []byte, setter func(*C.csqlgo_bind, C.int, unsafe.Pointer, C.int) C.int) error {
	if index <= 0 {
		return ErrInvalidArgument
	}
	var data unsafe.Pointer
	if len(value) > 0 {
		data = C.CBytes(value)
		defer C.csqlgo_free(data)
	}
	return bind.set(func(ptr *C.csqlgo_bind) C.int {
		return setter(ptr, C.int(index), data, C.int(len(value)))
	})
}

func (rows *Rows) call(call func(*C.csqlgo_cursor) C.int) error {
	if rows == nil || rows.conn == nil {
		return ErrClosed
	}
	rows.conn.mu.Lock()
	defer rows.conn.mu.Unlock()
	if rows.ptr == nil || rows.conn.ptr == nil {
		return ErrClosed
	}
	result := call(rows.ptr)
	if result < 0 {
		return &Error{Code: int(result)}
	}
	return nil
}

func (rows *Rows) number(number func(*C.csqlgo_cursor) C.int) (int, error) {
	if rows == nil || rows.conn == nil {
		return 0, ErrClosed
	}
	rows.conn.mu.Lock()
	defer rows.conn.mu.Unlock()
	if rows.ptr == nil || rows.conn.ptr == nil {
		return 0, ErrClosed
	}
	value := number(rows.ptr)
	if value < 0 {
		return 0, &Error{Code: int(value)}
	}
	return int(value), nil
}

func (rows *Rows) copyField(copy func(*C.csqlgo_cursor, **C.uchar, *C.int) C.int) ([]byte, bool, error) {
	if rows == nil || rows.conn == nil {
		return nil, false, ErrClosed
	}
	rows.conn.mu.Lock()
	defer rows.conn.mu.Unlock()
	if rows.ptr == nil || rows.conn.ptr == nil {
		return nil, false, ErrClosed
	}
	var data *C.uchar
	var length C.int
	result := copy(rows.ptr, &data, &length)
	if result == C.CSQLGO_FIELD_NULL {
		return nil, true, nil
	}
	if result != C.CSQLGO_OK {
		return nil, false, &Error{Code: int(result)}
	}
	defer C.csqlgo_free(unsafe.Pointer(data))
	return C.GoBytes(unsafe.Pointer(data), length), false, nil
}

func (stmt *Stmt) call(call func(*C.csqlgo_stmt) C.int) error {
	if stmt == nil || stmt.conn == nil {
		return ErrClosed
	}
	stmt.conn.mu.Lock()
	defer stmt.conn.mu.Unlock()
	if stmt.ptr == nil || stmt.conn.ptr == nil {
		return ErrClosed
	}
	result := call(stmt.ptr)
	if result != C.CSQLGO_OK {
		return stmt.conn.nativeErrorLocked(int(result))
	}
	return nil
}

func (stmt *Stmt) unsupported() error {
	if stmt == nil || stmt.conn == nil {
		return ErrClosed
	}
	stmt.conn.mu.Lock()
	defer stmt.conn.mu.Unlock()
	if stmt.ptr == nil || stmt.conn.ptr == nil {
		return ErrClosed
	}
	return ErrUnsupported
}

func (stmt *Stmt) bindBytes(index int, value []byte, binder func(*C.csqlgo_stmt, C.int, unsafe.Pointer, C.int) C.int) error {
	if index <= 0 {
		return ErrInvalidArgument
	}
	var data unsafe.Pointer
	if len(value) > 0 {
		data = C.CBytes(value)
		defer C.csqlgo_free(data)
	}
	return stmt.call(func(ptr *C.csqlgo_stmt) C.int {
		return binder(ptr, C.int(index), data, C.int(len(value)))
	})
}
