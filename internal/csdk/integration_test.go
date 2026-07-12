//go:build integration

package csdk

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"sync"
	"testing"
)

const phase2Database = "go_cubesql_driver_phase2.db"

func integrationOptions(t *testing.T, encryption Encryption) Options {
	t.Helper()
	username := os.Getenv("CUBESQL_USERNAME")
	password, passwordSet := os.LookupEnv("CUBESQL_PASSWORD")
	if username == "" || !passwordSet {
		t.Skip("CubeSQL integration credentials are not configured")
	}
	port := 4430
	if value := os.Getenv("CUBESQL_PORT"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("invalid CUBESQL_PORT: %v", err)
		}
		port = parsed
	}
	return Options{
		Host:       valueOr(os.Getenv("CUBESQL_HOST"), "localhost"),
		Port:       port,
		Username:   username,
		Password:   password,
		Timeout:    12,
		Encryption: encryption,
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func openSandbox(t *testing.T) *Conn {
	t.Helper()
	options := integrationOptions(t, EncryptionClear)
	conn, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec(fmt.Sprintf("DROP DATABASE '%s' IF EXISTS;", phase2Database)); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := conn.Exec(fmt.Sprintf("CREATE DATABASE '%s' IF NOT EXISTS;", phase2Database)); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := conn.SetDatabase(phase2Database); err != nil {
		conn.Close()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := conn.SetDatabase(""); err != nil && !errors.Is(err, ErrClosed) {
			t.Errorf("unset database: %v", err)
		}
		if err := conn.Exec(fmt.Sprintf("DROP DATABASE '%s' IF EXISTS;", phase2Database)); err != nil && !errors.Is(err, ErrClosed) {
			t.Errorf("drop sandbox: %v", err)
		}
		if err := conn.Close(); err != nil {
			t.Errorf("close sandbox connection: %v", err)
		}

		verify, err := Open(options)
		if err != nil {
			t.Errorf("open cleanup verifier: %v", err)
			return
		}
		defer verify.Close()
		if err := verify.SetDatabase(phase2Database); err == nil {
			verify.SetDatabase("")
			t.Errorf("sandbox database still exists after cleanup")
		}
	})
	return conn
}

func TestIntegrationOwnershipAndCopies(t *testing.T) {
	conn := openSandbox(t)
	if err := conn.Ping(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("CREATE TABLE values_test (id INTEGER, score REAL, txt TEXT, data BLOB, nullable TEXT);"); err != nil {
		t.Fatal(err)
	}

	bind, err := NewBind(5)
	if err != nil {
		t.Fatal(err)
	}
	if err := bind.SetInt64(1, math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	if err := bind.SetDouble(2, 1.25); err != nil {
		t.Fatal(err)
	}
	if err := bind.SetText(3, "สวัสดี CubeSQL"); err != nil {
		t.Fatal(err)
	}
	wantBlob := []byte{0x00, 0x01, 0xff, 0x00}
	if err := bind.SetBlob(4, wantBlob); err != nil {
		t.Fatal(err)
	}
	if err := bind.SetNull(5); err != nil {
		t.Fatal(err)
	}
	if err := conn.ExecBind("INSERT INTO values_test VALUES (?1, ?2, ?3, ?4, ?5);", bind); err != nil {
		t.Fatal(err)
	}
	// Reuse the retained C bind graph after cubesql_bind has mutated the
	// temporary arrays supplied by the shim.
	wantReusedBlob := []byte{0x10, 0x00, 0x20}
	for _, operation := range []func() error{
		func() error { return bind.SetInt64(1, 77) },
		func() error { return bind.SetDouble(2, 7.7) },
		func() error { return bind.SetText(3, "reused blob") },
		func() error { return bind.SetBlob(4, wantReusedBlob) },
		func() error { return bind.SetNull(5) },
	} {
		if err := operation(); err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.ExecBind("INSERT INTO values_test VALUES (?1, ?2, ?3, ?4, ?5);", bind); err != nil {
		t.Fatal(err)
	}
	if err := bind.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bind.SetBlob(4, []byte{0x01}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetBlob after bind Close() = %v, want ErrClosed", err)
	}
	if err := conn.Exec("INSERT INTO values_test VALUES (-9223372036854775808, -2.5, 'empty blob', X'', NULL);"); err != nil {
		t.Fatal(err)
	}

	rows, err := conn.Query("SELECT id, score, txt, data, nullable FROM values_test WHERE id = 9223372036854775807;")
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Close() with active rows = %v, want ErrBusy", err)
	}
	if got, err := rows.NumRows(); err != nil || got != 1 {
		t.Fatalf("NumRows() = %d, %v", got, err)
	}
	if got, err := rows.NumColumns(); err != nil || got != 5 {
		t.Fatalf("NumColumns() = %d, %v", got, err)
	}
	if got, err := rows.ColumnName(3); err != nil || got != "txt" {
		t.Fatalf("ColumnName(3) = %q, %v", got, err)
	}
	if err := rows.Seek(1); err != nil {
		t.Fatal(err)
	}
	text, isNull, err := rows.Field(1, 3)
	if err != nil || isNull || string(text) != "สวัสดี CubeSQL" {
		t.Fatalf("text field = %q, null=%v, err=%v", text, isNull, err)
	}
	blob, isNull, err := rows.Field(1, 4)
	if err != nil || isNull || !bytes.Equal(blob, wantBlob) {
		t.Fatalf("blob field = %v, null=%v, err=%v", blob, isNull, err)
	}
	value, isNull, err := rows.Field(1, 5)
	if err != nil || !isNull || value != nil {
		t.Fatalf("NULL field = %v, null=%v, err=%v", value, isNull, err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	rows, err = conn.Query("SELECT data FROM values_test WHERE id = 77;")
	if err != nil {
		t.Fatal(err)
	}
	reusedBlob, isNull, err := rows.Field(1, 1)
	closeErr := rows.Close()
	if err != nil || isNull || !bytes.Equal(reusedBlob, wantReusedBlob) {
		t.Fatalf("reused blob field = %v, null=%v, err=%v", reusedBlob, isNull, err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("second rows Close(): %v", err)
	}
	if _, _, err := rows.Field(1, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("Field() after Close() = %v, want ErrClosed", err)
	}

	stmt, err := conn.Prepare("INSERT INTO values_test VALUES (?1, ?2, ?3, ?4, ?5);")
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Close() with active statement = %v, want ErrBusy", err)
	}
	if err := stmt.BindBlob(4, []byte{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("BindBlob(empty) = %v, want ErrUnsupported", err)
	}
	for _, operation := range []func() error{
		func() error { return stmt.BindInt64(1, -42) },
		func() error { return stmt.BindDouble(2, -4.5) },
		func() error { return stmt.BindText(3, "prepared blob") },
		func() error { return stmt.BindBlob(4, []byte{0x01}) },
		func() error { return stmt.BindNull(5) },
		stmt.Exec,
	} {
		if err := operation(); err != nil {
			t.Fatal(err)
		}
	}
	for _, operation := range []func() error{
		func() error { return stmt.BindInt64(1, -43) },
		func() error { return stmt.BindDouble(2, -4.6) },
		func() error { return stmt.BindText(3, "prepared zeroblob") },
		func() error { return stmt.BindZeroBlob(4, 32) },
		func() error { return stmt.BindNull(5) },
		stmt.Exec,
	} {
		if err := operation(); err != nil {
			t.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stmt.Close(); err != nil {
		t.Fatalf("second stmt Close(): %v", err)
	}
	if err := stmt.Exec(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Exec() after stmt Close() = %v, want ErrClosed", err)
	}

	rows, err = conn.Query("SELECT data, nullable FROM values_test WHERE id = -9223372036854775808;")
	if err != nil {
		t.Fatal(err)
	}
	empty, isNull, err := rows.Field(1, 1)
	closeErr = rows.Close()
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	// SDK 060600 reports SQL literal X'' as a NULL cursor field. A Phase 3
	// server-side predicate probe proves the stored value is actually a BLOB of
	// length zero, so this regression records a cursor-protocol ambiguity rather
	// than server-side NULL coercion.
	if err != nil || !isNull || empty != nil {
		t.Fatalf("X'' cursor field = %v, null=%v, err=%v; want SDK NULL report", empty, isNull, err)
	}

	rows, err = conn.Query("SELECT data FROM values_test WHERE id = -43;")
	if err != nil {
		t.Fatal(err)
	}
	zeroBlob, isNull, err := rows.Field(1, 1)
	closeErr = rows.Close()
	if err != nil || isNull || !bytes.Equal(zeroBlob, make([]byte, 32)) {
		t.Fatalf("prepared zeroblob field = %v, null=%v, err=%v", zeroBlob, isNull, err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}

	if _, err := conn.Changes(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.AffectedRows(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.LastInsertID(); err != nil {
		t.Fatal(err)
	}

	if err := conn.Exec("THIS IS NOT VALID SQL;"); err == nil {
		t.Fatal("invalid SQL unexpectedly succeeded")
	} else {
		var sdkError *Error
		if !errors.As(err, &sdkError) || sdkError.Message == "" {
			t.Fatalf("invalid SQL error = %#v, want copied SDK error", err)
		}
		copied := sdkError.Message
		if err := conn.Ping(); err != nil {
			t.Fatal(err)
		}
		if sdkError.Message != copied {
			t.Fatal("copied SDK error changed after subsequent native call")
		}
	}
}

func TestIntegrationTransactionsAndPreparedQuery(t *testing.T) {
	conn := openSandbox(t)
	if err := conn.Exec("CREATE TABLE tx_test (id INTEGER, value TEXT);"); err != nil {
		t.Fatal(err)
	}
	// CubeSQL DDL may leave the session in a transaction. Match the official
	// C integration baseline by restoring autocommit before an explicit Begin.
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("INSERT INTO tx_test VALUES (1, 'rolled back');"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("INSERT INTO tx_test VALUES (2, 'committed');"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}

	stmt, err := conn.Prepare("SELECT value FROM tx_test WHERE id = ?1;")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.BindInt64(1, 2); err != nil {
		t.Fatal(err)
	}
	rows, err := stmt.Query()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	value, isNull, err := rows.Field(1, 1)
	if err != nil || isNull || string(value) != "committed" {
		t.Fatalf("prepared result = %q, null=%v, err=%v", value, isNull, err)
	}
}

func TestIntegrationSerializedConcurrentPing(t *testing.T) {
	options := integrationOptions(t, EncryptionAES256)
	conn, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var wait sync.WaitGroup
	errorsSeen := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 10; iteration++ {
				if err := conn.Ping(); err != nil {
					errorsSeen <- err
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Error(err)
	}
}

func TestIntegrationRepeatedOpenClose(t *testing.T) {
	options := integrationOptions(t, EncryptionClear)
	for iteration := 0; iteration < 25; iteration++ {
		conn, err := Open(options)
		if err != nil {
			t.Fatalf("Open iteration %d: %v", iteration, err)
		}
		if err := conn.Ping(); err != nil {
			conn.Close()
			t.Fatalf("Ping iteration %d: %v", iteration, err)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("Close iteration %d: %v", iteration, err)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("second Close iteration %d: %v", iteration, err)
		}
	}
}
