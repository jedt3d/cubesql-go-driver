package cubesql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	core "github.com/jedt3d/cubesql-go-driver/cubesql"
)

func TestParseDSN(t *testing.T) {
	config, err := ParseDSN("cubesql://user:p%40ss@127.0.0.1:4431/example.db?timeout=1500ms&encryption=aes256")
	if err != nil {
		t.Fatal(err)
	}
	if config.Options.Host != "127.0.0.1" || config.Options.Port != 4431 ||
		config.Options.Username != "user" || config.Options.Password != "p@ss" ||
		config.Options.Timeout != 1500*time.Millisecond || config.Options.Encryption != core.EncryptionAES256 ||
		config.Database != "example.db" {
		t.Fatalf("ParseDSN() = %#v", config)
	}
	if !slices.Contains(sql.Drivers(), DriverName) {
		t.Fatalf("registered drivers = %v, want %q", sql.Drivers(), DriverName)
	}
}

func TestParseDSNRejectsInvalidWithoutEchoingDSN(t *testing.T) {
	for _, dsn := range []string{
		"postgres://user:secret@localhost/db",
		"cubesql://user@localhost/db",
		"cubesql://:secret@localhost/db",
		"cubesql://user:secret@localhost:bad/db",
		"cubesql://user:secret@localhost/a/b",
		"cubesql://user:secret@localhost/db?unknown=1",
		"cubesql://user:secret@localhost/db?timeout=bad",
		"cubesql://user:secret@localhost/db?encryption=tls",
	} {
		_, err := ParseDSN(dsn)
		if !errors.Is(err, ErrInvalidDSN) {
			t.Fatalf("ParseDSN(%q) = %v, want ErrInvalidDSN", dsn, err)
		}
		if err != nil && containsAny(err.Error(), "secret", dsn) {
			t.Fatalf("ParseDSN error leaks DSN: %v", err)
		}
	}
}

func TestNamedValueConversion(t *testing.T) {
	for _, test := range []struct {
		input any
		want  any
	}{
		{int(7), int64(7)},
		{true, int64(1)},
		{false, int64(0)},
		{[]byte{}, []byte{}},
		{core.ZeroBlob{Size: 4}, core.ZeroBlob{Size: 4}},
	} {
		value := driver.NamedValue{Ordinal: 1, Value: test.input}
		if err := checkNamedValue(&value); err != nil {
			t.Fatalf("checkNamedValue(%T) = %v", test.input, err)
		}
		if !reflect.DeepEqual(value.Value, test.want) {
			t.Fatalf("checkNamedValue(%T) = %#v, want %#v", test.input, value.Value, test.want)
		}
	}

	named := driver.NamedValue{Name: "named", Value: int64(1)}
	if err := checkNamedValue(&named); !errors.Is(err, ErrNamedParameters) {
		t.Fatalf("named parameter error = %v", err)
	}
	unsupported := driver.NamedValue{Value: time.Now()}
	if err := checkNamedValue(&unsupported); err == nil {
		t.Fatal("time.Time unexpectedly accepted")
	}
}

func TestDriverValuePreservesNonNullEmptyBlob(t *testing.T) {
	value, err := driverValue(core.Value{Type: core.TypeBlob, Raw: nil, Null: false})
	if err != nil {
		t.Fatal(err)
	}
	blob, ok := value.([]byte)
	if !ok || blob == nil || len(blob) != 0 {
		t.Fatalf("driverValue(empty BLOB) = %#v, want non-nil empty []byte", value)
	}
	if value, err := driverValue(core.Value{Type: core.TypeBlob, Null: true}); err != nil || value != nil {
		t.Fatalf("driverValue(NULL) = %#v, %v", value, err)
	}
}

func TestCanceledConnectAndClosedConnection(t *testing.T) {
	connector, err := NewConnector(Config{Options: core.Options{Username: "user"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := connector.Connect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Connect(canceled) = %v", err)
	}
	closed := &conn{closed: true}
	if err := closed.Ping(context.Background()); !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("Ping(closed) = %v", err)
	}
	if closed.IsValid() {
		t.Fatal("closed connection is valid")
	}
}

func TestTransactionOptionsFailBeforeNativeCall(t *testing.T) {
	connection := &conn{}
	if _, err := connection.BeginTx(context.Background(), driver.TxOptions{ReadOnly: true}); !errors.Is(err, ErrReadOnlyTransaction) {
		t.Fatalf("BeginTx(read-only) = %v", err)
	}
	if _, err := connection.BeginTx(context.Background(), driver.TxOptions{Isolation: driver.IsolationLevel(1)}); !errors.Is(err, ErrTransactionIsolation) {
		t.Fatalf("BeginTx(isolation) = %v", err)
	}
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if value != "" && strings.Contains(text, value) {
			return true
		}
	}
	return false
}
