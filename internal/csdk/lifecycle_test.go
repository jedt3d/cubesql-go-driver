package csdk

import (
	"errors"
	"math"
	"testing"
)

func TestVersion(t *testing.T) {
	if got, want := Version(), "060600"; got != want {
		t.Fatalf("Version() = %q, want %q", got, want)
	}
}

func TestClosedConnectionIsDeterministic(t *testing.T) {
	var nilConn *Conn
	if err := nilConn.Close(); err != nil {
		t.Fatalf("nil Close() error = %v", err)
	}

	conn := &Conn{}
	if err := conn.Close(); err != nil {
		t.Fatalf("zero Close() error = %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := conn.Ping(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Ping() error = %v, want ErrClosed", err)
	}
	if err := conn.Exec("SELECT 1;"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Exec() error = %v, want ErrClosed", err)
	}
	if _, err := conn.Query("SELECT 1;"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Query() error = %v, want ErrClosed", err)
	}
	if _, err := conn.Prepare("SELECT 1;"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Prepare() error = %v, want ErrClosed", err)
	}
}

func TestCAllocatedBindLifecycle(t *testing.T) {
	bind, err := NewBind(5)
	if err != nil {
		t.Fatal(err)
	}
	setters := []func() error{
		func() error { return bind.SetInt64(1, math.MinInt64) },
		func() error { return bind.SetDouble(2, 1.25) },
		func() error { return bind.SetText(3, "hello") },
		func() error { return bind.SetBlob(4, []byte{0x00}) },
		func() error { return bind.SetNull(5) },
	}
	for index, set := range setters {
		if err := set(); err != nil {
			t.Fatalf("setter %d: %v", index+1, err)
		}
	}
	if err := bind.SetText(0, "bad"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("SetText(0) error = %v, want ErrInvalidArgument", err)
	}
	if err := bind.SetDouble(2, math.NaN()); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("SetDouble(NaN) error = %v, want ErrInvalidArgument", err)
	}
	if err := bind.SetBlob(4, []byte{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("SetBlob(empty) error = %v, want ErrUnsupported", err)
	}
	if err := bind.SetZeroBlob(4, 0); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("SetZeroBlob(empty) error = %v, want ErrUnsupported", err)
	}
	if err := bind.SetZeroBlob(4, 32); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("SetZeroBlob(32) error = %v, want ErrUnsupported", err)
	}
	if err := bind.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bind.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := bind.SetNull(1); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetNull after Close() error = %v, want ErrClosed", err)
	}
	if err := bind.SetBlob(1, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetBlob(empty) after Close() error = %v, want ErrClosed", err)
	}
}

func TestClosedChildHandlesAreDeterministic(t *testing.T) {
	conn := &Conn{}
	rows := &Rows{conn: conn}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := rows.NumRows(); !errors.Is(err, ErrClosed) {
		t.Fatalf("NumRows() error = %v, want ErrClosed", err)
	}

	stmt := &Stmt{conn: conn}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stmt.Exec(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Exec() error = %v, want ErrClosed", err)
	}
}

func TestOpenRejectsInvalidOptionsBeforeC(t *testing.T) {
	for _, options := range []Options{
		{},
		{Username: "user", Port: -1},
		{Username: "user", Timeout: -1},
		{Username: "user", Encryption: 99},
	} {
		if _, err := Open(options); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("Open(%+v) error = %v, want ErrInvalidArgument", options, err)
		}
	}
}
