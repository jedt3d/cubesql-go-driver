//go:build integration

package cubesql

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

const phase3Database = "go_cubesql_driver_phase3.db"

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
		Timeout:    12 * time.Second,
		Encryption: encryption,
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func openSandbox(t *testing.T) (*Conn, Options) {
	t.Helper()
	options := integrationOptions(t, EncryptionClear)
	conn, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(fmt.Sprintf("DROP DATABASE '%s' IF EXISTS;", phase3Database)); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if _, err := conn.Exec(fmt.Sprintf("CREATE DATABASE '%s' IF NOT EXISTS;", phase3Database)); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := conn.SetDatabase(phase3Database); err != nil {
		conn.Close()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := conn.SetDatabase(""); err != nil && !errors.Is(err, ErrClosed) {
			t.Errorf("unset database: %v", err)
		}
		if _, err := conn.Exec(fmt.Sprintf("DROP DATABASE '%s' IF EXISTS;", phase3Database)); err != nil && !errors.Is(err, ErrClosed) {
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
		if err := verify.SetDatabase(phase3Database); err == nil {
			verify.SetDatabase("")
			t.Errorf("sandbox database still exists after cleanup")
		}
	})
	return conn, options
}

func openSelected(t *testing.T, options Options) *Conn {
	t.Helper()
	conn, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetDatabase(phase3Database); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	return conn
}

func scalarInt64(t *testing.T, conn *Conn, query string, args ...any) int64 {
	t.Helper()
	rows, err := conn.Query(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("scalar query returned no row: %v", rows.Err())
	}
	var value int64
	if err := rows.Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestIntegrationCoreFunctionalCoverage(t *testing.T) {
	conn, options := openSandbox(t)
	if err := conn.Ping(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`CREATE TABLE core_values (
        id INTEGER PRIMARY KEY,
        score REAL NOT NULL,
        txt TEXT NOT NULL,
        data BLOB,
        nullable TEXT
    );`); err != nil {
		t.Fatal(err)
	}
	// CubeSQL DDL can leave the session in a transaction.
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}

	verify := openSelected(t, options)
	if got := scalarInt64(t, verify, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='core_values';"); got != 1 {
		verify.Close()
		t.Fatalf("table count = %d, want 1", got)
	}
	if err := verify.Close(); err != nil {
		t.Fatal(err)
	}

	wantBlob := []byte{0x00, 0x01, 0xff, 0x00}
	result, err := conn.Exec(
		"INSERT INTO core_values VALUES (?1, ?2, ?3, ?4, ?5);",
		int64(math.MinInt64), float64(-2.5), "สวัสดี 'CubeSQL'", wantBlob, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("direct RowsAffected() = %d, %v; want 1", affected, err)
	}
	if lastID, err := result.LastInsertID(); err != nil || lastID != math.MinInt64 {
		t.Fatalf("direct LastInsertID() = %d, %v; want MinInt64", lastID, err)
	}

	rows, err := conn.Query("SELECT id, score, txt, data, nullable FROM core_values WHERE id = ?1;", int64(math.MinInt64))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rows.Columns(), []string{"id", "score", "txt", "data", "nullable"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("Columns() = %v, want %v", got, want)
	}
	if rows.NumRows() != 1 || !rows.Next() {
		t.Fatalf("direct query rows = %d, next=%v, err=%v", rows.NumRows(), rows.Next(), rows.Err())
	}
	if got := rows.ColumnTypes(); len(got) != 5 || got[0] != TypeInteger || got[3] != TypeBlob {
		rows.Close()
		t.Fatalf("ColumnTypes() = %v", got)
	}
	var (
		id       int64
		score    float64
		text     string
		blob     []byte
		nullable NullString
	)
	if err := rows.Scan(&id, &score, &text, &blob, &nullable); err != nil {
		t.Fatal(err)
	}
	if id != math.MinInt64 || score != -2.5 || text != "สวัสดี 'CubeSQL'" || !bytes.Equal(blob, wantBlob) || nullable.Valid {
		t.Fatalf("direct row = %d, %v, %q, %v, %#v", id, score, text, blob, nullable)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	stmt, err := conn.Prepare("INSERT INTO core_values VALUES (?1, ?2, ?3, ?4, ?5);")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if _, err := conn.Prepare("SELECT 1;"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Prepare() = %v, want ErrBusy", err)
	}
	if _, err := conn.Exec("SELECT 1;"); !errors.Is(err, ErrBusy) {
		t.Fatalf("connection Exec with active statement = %v, want ErrBusy", err)
	}
	if err := stmt.BindInt64(0, 1); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("BindInt64(0) = %v, want ErrInvalidArgument", err)
	}
	for index, value := range []any{int64(math.MaxInt64), float64(1.25), "prepared", []byte{0x10}, nil} {
		if err := stmt.Bind(index+1, value); err != nil {
			t.Fatal(err)
		}
	}
	preparedResult, err := stmt.Exec()
	if err != nil {
		t.Fatal(err)
	}
	if affected, err := preparedResult.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("prepared RowsAffected() = %d, %v; want 1", affected, err)
	}
	if err := stmt.Reset(); err != nil {
		t.Fatal(err)
	}
	for index, value := range []any{int64(3), float64(3.5), "reused", ZeroBlob{Size: 32}, "present"} {
		if err := stmt.Bind(index+1, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := stmt.Exec(); err != nil {
		t.Fatal(err)
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := stmt.Exec(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Exec after statement Close() = %v, want ErrClosed", err)
	}

	rows, err = conn.Query("SELECT data FROM core_values WHERE id = 3;")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var zeroBlob []byte
	if err := rows.Scan(&zeroBlob); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(zeroBlob, make([]byte, 32)) {
		t.Fatalf("prepared zeroblob length = %d, want 32", len(zeroBlob))
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	emptyResult, err := conn.Exec("INSERT INTO core_values VALUES (?1, 0, 'empty', ?2, NULL);", int64(4), []byte{})
	if err != nil {
		t.Fatal(err)
	}
	if affected, err := emptyResult.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("empty BLOB RowsAffected() = %d, %v; want 1", affected, err)
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err = conn.Query("SELECT data, data IS NULL, typeof(data), coalesce(length(data), -1) FROM core_values WHERE id=4;")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var (
		boundData   NullBytes
		boundIsNull int64
		boundType   string
		boundLength int64
	)
	if err := rows.Scan(&boundData, &boundIsNull, &boundType, &boundLength); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if boundData.Valid || boundIsNull != 0 || boundType != "blob" || boundLength != 0 {
		t.Fatalf("bound empty BLOB: data=%#v is_null=%d type=%s length=%d", boundData, boundIsNull, boundType, boundLength)
	}
	if _, err := conn.Exec("INSERT INTO core_values VALUES (6, 0, 'literal empty', X'', NULL);"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err = conn.Query("SELECT data, data IS NULL, typeof(data), coalesce(length(data), -1) FROM core_values WHERE id=6;")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var (
		literalData   NullBytes
		literalIsNull int64
		literalType   string
		literalLength int64
	)
	if err := rows.Scan(&literalData, &literalIsNull, &literalType, &literalLength); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if literalData.Valid || literalIsNull != 0 || literalType != "blob" || literalLength != 0 {
		t.Fatalf("literal empty BLOB: data=%#v is_null=%d type=%s length=%d", literalData, literalIsNull, literalType, literalLength)
	}
	if _, err := conn.Exec("INSERT INTO core_values VALUES (7, 0, 'true null', NULL, NULL);"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err = conn.Query("SELECT data, data IS NULL, typeof(data), coalesce(length(data), -1) FROM core_values WHERE id=7;")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var (
		nullData   NullBytes
		nullIsNull int64
		nullType   string
		nullLength int64
	)
	if err := rows.Scan(&nullData, &nullIsNull, &nullType, &nullLength); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if nullData.Valid || nullIsNull != 1 || nullType != "null" || nullLength != -1 {
		t.Fatalf("SQL NULL BLOB: data=%#v is_null=%d type=%s length=%d", nullData, nullIsNull, nullType, nullLength)
	}
	if _, err := conn.Exec("DELETE FROM core_values WHERE id IN (4, 6, 7);"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}

	largeBlob := make([]byte, 256*1024)
	for index := range largeBlob {
		largeBlob[index] = byte(index)
	}
	if _, err := conn.Exec("INSERT INTO core_values VALUES (?1, 5, 'large', ?2, NULL);", int64(5), largeBlob); err != nil {
		t.Fatal(err)
	}
	rows, err = conn.Query("SELECT data FROM core_values WHERE id = 5;")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var largeCopy []byte
	if err := rows.Scan(&largeCopy); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(largeCopy, largeBlob) {
		t.Fatal("large BLOB copy mismatch")
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}
	verify = openSelected(t, options)
	if got := scalarInt64(t, verify, "SELECT count(*) FROM core_values WHERE id IN (?1, ?2, ?3);", int64(math.MinInt64), int64(math.MaxInt64), int64(3)); got != 3 {
		verify.Close()
		t.Fatalf("persisted insert count = %d, want 3", got)
	}
	verify.Close()

	updateResult, err := conn.Exec("UPDATE core_values SET txt='updated' WHERE id=?1;", int64(3))
	if err != nil {
		t.Fatal(err)
	}
	if affected, err := updateResult.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("update RowsAffected() = %d, %v; want 1", affected, err)
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}
	verify = openSelected(t, options)
	if got := scalarInt64(t, verify, "SELECT count(*) FROM core_values WHERE id=3 AND txt='updated';"); got != 1 {
		verify.Close()
		t.Fatalf("persisted update count = %d, want 1", got)
	}
	verify.Close()

	deleteResult, err := conn.Exec("DELETE FROM core_values WHERE id=?1;", int64(5))
	if err != nil {
		t.Fatal(err)
	}
	if affected, err := deleteResult.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("delete RowsAffected() = %d, %v; want 1", affected, err)
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}
	verify = openSelected(t, options)
	if got := scalarInt64(t, verify, "SELECT count(*) FROM core_values WHERE id=5;"); got != 0 {
		verify.Close()
		t.Fatalf("persisted delete count = %d, want 0", got)
	}
	verify.Close()

	rows, err = conn.Query("SELECT id FROM core_values WHERE id < 0 ORDER BY id;")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err = conn.Query("SELECT id FROM core_values WHERE id = 999999;")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() || rows.Err() != nil || rows.NumRows() != 0 {
		t.Fatalf("empty result: next=%v err=%v rows=%d", rows.Next(), rows.Err(), rows.NumRows())
	}
	rows.Close()

	rows, err = conn.Query("SELECT id FROM core_values ORDER BY id;")
	if err != nil {
		t.Fatal(err)
	}
	rowCount := 0
	for rows.Next() {
		var value int64
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		rowCount++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if rowCount != 3 {
		t.Fatalf("multiple-row count = %d, want 3", rowCount)
	}
	if err := conn.Close(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Close with active rows = %v, want ErrBusy", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	queryStmt, err := conn.Prepare("SELECT txt FROM core_values WHERE id=?1;")
	if err != nil {
		t.Fatal(err)
	}
	if err := queryStmt.BindInt64(1, 3); err != nil {
		t.Fatal(err)
	}
	queryRows, err := queryStmt.Query()
	if err != nil {
		t.Fatal(err)
	}
	if err := queryStmt.Close(); !errors.Is(err, ErrBusy) {
		t.Fatalf("statement Close with active rows = %v, want ErrBusy", err)
	}
	if err := queryStmt.Reset(); !errors.Is(err, ErrBusy) {
		t.Fatalf("statement Reset with active rows = %v, want ErrBusy", err)
	}
	if err := conn.Commit(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Commit with active statement rows = %v, want ErrBusy", err)
	}
	if err := queryRows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := queryStmt.Close(); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	concurrentErrors := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func(id int64) {
			defer wait.Done()
			result, err := conn.Exec("INSERT INTO core_values VALUES (?1, 0, 'concurrent', X'01', NULL);", id)
			if err != nil {
				concurrentErrors <- err
				return
			}
			if affected, err := result.RowsAffected(); err != nil || affected != 1 {
				concurrentErrors <- fmt.Errorf("concurrent RowsAffected() = %d, %v", affected, err)
			}
		}(int64(100 + worker))
	}
	wait.Wait()
	close(concurrentErrors)
	for err := range concurrentErrors {
		t.Error(err)
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}
	verify = openSelected(t, options)
	if got := scalarInt64(t, verify, "SELECT count(*) FROM core_values WHERE id BETWEEN 100 AND 107;"); got != 8 {
		verify.Close()
		t.Fatalf("concurrent insert count = %d, want 8", got)
	}
	verify.Close()
	if _, err := conn.Exec("DELETE FROM core_values WHERE id BETWEEN 100 AND 107;"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.Exec("THIS IS NOT VALID SQL;"); err == nil {
		t.Fatal("invalid SQL unexpectedly succeeded")
	} else {
		var sdkError *Error
		if !errors.As(err, &sdkError) || sdkError.Message == "" {
			t.Fatalf("invalid SQL error = %#v, want copied *Error", err)
		}
		if sdkError.Kind != ErrorServer || !errors.Is(err, ErrServer) {
			t.Fatalf("invalid SQL kind = %v, want ErrorServer", sdkError.Kind)
		}
		copied := sdkError.Message
		if err := conn.Ping(); err != nil {
			t.Fatal(err)
		}
		if sdkError.Message != copied {
			t.Fatal("public SDK error changed after later call")
		}
	}

	if _, err := conn.Exec("CREATE TABLE tx_values (id INTEGER PRIMARY KEY, value TEXT);"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Close during transaction = %v, want ErrBusy", err)
	}
	if _, err := tx.Exec("INSERT INTO tx_values VALUES (1, 'rollback');"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); !errors.Is(err, ErrTxDone) {
		t.Fatalf("second Rollback() = %v, want ErrTxDone", err)
	}
	verify = openSelected(t, options)
	if got := scalarInt64(t, verify, "SELECT count(*) FROM tx_values WHERE id=1;"); got != 0 {
		verify.Close()
		t.Fatalf("rolled-back row count = %d, want 0", got)
	}
	verify.Close()

	tx, err = conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO tx_values VALUES (2, 'commit');"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	verify = openSelected(t, options)
	if got := scalarInt64(t, verify, "SELECT count(*) FROM tx_values WHERE id=2 AND value='commit';"); got != 1 {
		verify.Close()
		t.Fatalf("committed row count = %d, want 1", got)
	}
	verify.Close()
}

func TestIntegrationCoreConnectionModesAndLifecycle(t *testing.T) {
	options := integrationOptions(t, EncryptionAES256)
	conn, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Ping(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Ping after Close() = %v, want ErrClosed", err)
	}

	bad := options
	bad.Password += "-invalid"
	if _, err := Open(bad); err == nil {
		t.Fatal("bad credentials unexpectedly connected")
	} else {
		var sdkError *Error
		if !errors.As(err, &sdkError) {
			t.Fatalf("bad credentials error = %T, want *Error", err)
		}
		if sdkError.Code != 7056 || sdkError.Kind != ErrorAuthentication || !errors.Is(err, ErrAuthentication) {
			t.Fatalf("bad credentials classification = code:%d kind:%v", sdkError.Code, sdkError.Kind)
		}
	}
}

func TestIntegrationContextPreflightHasNoSideEffect(t *testing.T) {
	conn, _ := openSandbox(t)
	if _, err := conn.Exec("CREATE TABLE context_values (id INTEGER PRIMARY KEY);"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Commit(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := conn.ExecContext(ctx, "INSERT INTO context_values VALUES (1);"); !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecContext(canceled) = %v, want context.Canceled", err)
	}
	if got := scalarInt64(t, conn, "SELECT count(*) FROM context_values;"); got != 0 {
		t.Fatalf("canceled execution row count = %d, want 0", got)
	}
}

func TestIntegrationNetworkAndHandshakeTimeoutClassification(t *testing.T) {
	if _, err := Open(Options{
		Host:     "does-not-exist.invalid",
		Port:     4430,
		Username: "invalid",
		Password: "invalid",
		Timeout:  time.Second,
	}); err == nil {
		t.Fatal("invalid host unexpectedly connected")
	} else {
		var sdkError *Error
		if !errors.As(err, &sdkError) || sdkError.Code != 802 || sdkError.Kind != ErrorNetwork || !errors.Is(err, ErrNetwork) {
			t.Fatalf("invalid-host classification = %#v", err)
		}
	}

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, openErr := OpenContext(ctx, Options{
		Host:     "127.0.0.1",
		Port:     listener.Addr().(*net.TCPAddr).Port,
		Username: "timeout",
		Password: "timeout",
		Timeout:  time.Second,
	})
	elapsed := time.Since(started)
	select {
	case conn := <-accepted:
		conn.Close()
	default:
	}
	var sdkError *Error
	if !errors.As(openErr, &sdkError) || sdkError.Code != 810 || sdkError.Kind != ErrorTimeout || !errors.Is(openErr, ErrTimeout) {
		t.Fatalf("handshake-timeout classification = %#v", openErr)
	}
	if elapsed < 4*time.Second || elapsed > 8*time.Second {
		t.Fatalf("handshake timeout elapsed = %v, want fixed SDK window near 5s", elapsed)
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("context state = %v, want DeadlineExceeded", ctx.Err())
	}
}
