package cubesql

import (
	"bytes"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/jedt3d/cubesql-go-driver/internal/csdk"
)

func TestVersion(t *testing.T) {
	if got, want := Version(), "060600"; got != want {
		t.Fatalf("Version() = %q, want %q", got, want)
	}
}

func TestOptionsValidation(t *testing.T) {
	valid, err := (Options{Username: "user", Timeout: time.Nanosecond}).native()
	if err != nil {
		t.Fatal(err)
	}
	if valid.Timeout != 1 {
		t.Fatalf("rounded Timeout = %d, want 1", valid.Timeout)
	}
	for _, options := range []Options{
		{Username: "user", Port: -1},
		{Username: "user", Port: 65536},
		{Username: "user", Timeout: -time.Second},
		{Username: "user", Encryption: 99},
	} {
		if _, err := options.native(); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("Options(%+v) error = %v, want ErrInvalidArgument", options, err)
		}
	}
}

func TestPublicErrorCopiesNativeError(t *testing.T) {
	native := &csdk.Error{Code: 7000, Message: "copied"}
	err := publicError(native)
	var public *Error
	if !errors.As(err, &public) {
		t.Fatalf("publicError(%v) = %T, want *Error", native, err)
	}
	if public.Code != native.Code || public.Message != native.Message {
		t.Fatalf("public error = %#v, want copied native fields", public)
	}
	native.Message = "changed"
	if public.Message != "copied" {
		t.Fatal("public error retained mutable native error state")
	}
}

func TestClosedLifecycleIsDeterministic(t *testing.T) {
	var nilConn *Conn
	if err := nilConn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := nilConn.Ping(); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil Ping() = %v, want ErrClosed", err)
	}
	conn := &Conn{}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("SELECT 1;"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed Exec() = %v, want ErrClosed", err)
	}
	if _, err := conn.Query("SELECT 1;"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed Query() = %v, want ErrClosed", err)
	}
	if _, err := conn.Prepare("SELECT 1;"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed Prepare() = %v, want ErrClosed", err)
	}

	stmt := &Stmt{}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stmt.BindNull(1); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed BindNull() = %v, want ErrClosed", err)
	}
	if _, err := stmt.Exec(); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed statement Exec() = %v, want ErrClosed", err)
	}

	rows := &Rows{}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("closed Rows.Next() = true")
	}
	if !errors.Is(rows.Err(), ErrClosed) {
		t.Fatalf("closed Rows.Err() = %v, want ErrClosed", rows.Err())
	}

	var nilTx *Tx
	if err := nilTx.Commit(); !errors.Is(err, ErrTxDone) {
		t.Fatalf("nil Commit() = %v, want ErrTxDone", err)
	}
}

func TestScanValueTypedAndNull(t *testing.T) {
	var integer int64
	if err := scanValue(Value{Type: TypeInteger, Raw: []byte("-9223372036854775808")}, &integer); err != nil {
		t.Fatal(err)
	}
	if integer != math.MinInt64 {
		t.Fatalf("integer = %d, want MinInt64", integer)
	}

	var floating float64
	if err := scanValue(Value{Type: TypeFloat, Raw: []byte("1.25")}, &floating); err != nil {
		t.Fatal(err)
	}
	if floating != 1.25 {
		t.Fatalf("floating = %v, want 1.25", floating)
	}

	original := []byte{0x00, 0xff, 0x00}
	var blob []byte
	if err := scanValue(Value{Type: TypeBlob, Raw: original}, &blob); err != nil {
		t.Fatal(err)
	}
	original[1] = 0
	if !bytes.Equal(blob, []byte{0x00, 0xff, 0x00}) {
		t.Fatalf("blob = %v, want independent copy", blob)
	}

	var nullable NullString
	if err := scanValue(Value{Type: TypeText, Null: true}, &nullable); err != nil {
		t.Fatal(err)
	}
	if nullable.Valid {
		t.Fatal("NULL string marked valid")
	}
	if err := scanValue(Value{Type: TypeText, Null: true}, new(string)); !errors.Is(err, ErrScan) {
		t.Fatalf("NULL into string = %v, want ErrScan", err)
	}
}

func TestValueInterfaceUsesColumnType(t *testing.T) {
	for _, test := range []struct {
		value Value
		want  any
	}{
		{Value{Type: TypeInteger, Raw: []byte("42")}, int64(42)},
		{Value{Type: TypeFloat, Raw: []byte("4.5")}, float64(4.5)},
		{Value{Type: TypeText, Raw: []byte("text")}, "text"},
	} {
		got, err := valueInterface(test.value)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("valueInterface(%#v) = %#v, want %#v", test.value, got, test.want)
		}
	}
}
