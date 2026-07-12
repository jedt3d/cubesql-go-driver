//go:build integration

package cubesql

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	core "github.com/jedt3d/cubesql-go-driver/cubesql"
)

const phase5Database = "go_cubesql_driver_phase5.db"

func integrationConfig(t *testing.T, encryption core.Encryption) Config {
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
	return Config{
		Options: core.Options{
			Host:       valueOr(os.Getenv("CUBESQL_HOST"), "localhost"),
			Port:       port,
			Username:   username,
			Password:   password,
			Timeout:    12 * time.Second,
			Encryption: encryption,
		},
		Database: phase5Database,
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func openAdmin(t *testing.T, config Config) *core.Conn {
	t.Helper()
	admin, err := core.Open(config.Options)
	if err != nil {
		t.Fatal(err)
	}
	return admin
}

func openSandbox(t *testing.T) (*sql.DB, Config) {
	t.Helper()
	config := integrationConfig(t, core.EncryptionClear)
	admin := openAdmin(t, config)
	if _, err := admin.Exec(fmt.Sprintf("DROP DATABASE '%s' IF EXISTS;", phase5Database)); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	if _, err := admin.Exec(fmt.Sprintf("CREATE DATABASE '%s' IF NOT EXISTS;", phase5Database)); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	if err := admin.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := OpenDB(config)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database/sql pool: %v", err)
		}
		cleanup := openAdmin(t, config)
		if _, err := cleanup.Exec(fmt.Sprintf("DROP DATABASE '%s' IF EXISTS;", phase5Database)); err != nil {
			t.Errorf("drop Phase 5 sandbox: %v", err)
		}
		if err := cleanup.Close(); err != nil {
			t.Errorf("close cleanup connection: %v", err)
		}

		verify := openAdmin(t, config)
		defer verify.Close()
		if err := verify.SetDatabase(phase5Database); err == nil {
			verify.SetDatabase("")
			t.Errorf("Phase 5 sandbox still exists after cleanup")
		}
	})
	return database, config
}

func openSecondPool(t *testing.T, config Config) *sql.DB {
	t.Helper()
	database, err := OpenDB(config)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(0)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close second pool: %v", err)
		}
	})
	return database
}

func scalarInt64(t *testing.T, database *sql.DB, query string, arguments ...any) int64 {
	t.Helper()
	var value int64
	if err := database.QueryRow(query, arguments...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestIntegrationDatabaseSQLFunctionalCoverage(t *testing.T) {
	database, config := openSandbox(t)
	ctx := context.Background()
	if err := database.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE values_test (
        id INTEGER PRIMARY KEY,
        whole INTEGER,
        score REAL,
        txt TEXT,
        payload BLOB,
        nullable TEXT
    );`); err != nil {
		t.Fatal(err)
	}

	second := openSecondPool(t, config)
	if got := scalarInt64(t, second, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='values_test';"); got != 1 {
		t.Fatalf("table visibility = %d, want 1", got)
	}

	direct, err := database.ExecContext(ctx,
		"INSERT INTO values_test VALUES (1, -9223372036854775808, 1.25, 'direct', X'0001FF', NULL);")
	if err != nil {
		t.Fatal(err)
	}
	if affected, err := direct.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("direct RowsAffected = %d, %v", affected, err)
	}
	if lastID, err := direct.LastInsertId(); err != nil || lastID != 1 {
		t.Fatalf("direct LastInsertId = %d, %v", lastID, err)
	}

	prepared, err := database.PrepareContext(ctx, "INSERT INTO values_test VALUES (?, ?, ?, ?, ?, ?);")
	if err != nil {
		t.Fatal(err)
	}
	preparedResult, err := prepared.ExecContext(ctx, int64(2), int64(math.MaxInt64), -2.5,
		"สวัสดี 'CubeSQL'", []byte{0x00, 0x02, 0xff, 0x00}, "present")
	if err != nil {
		prepared.Close()
		t.Fatal(err)
	}
	if affected, err := preparedResult.RowsAffected(); err != nil || affected != 1 {
		prepared.Close()
		t.Fatalf("prepared RowsAffected = %d, %v", affected, err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	lookup, err := database.PrepareContext(ctx, "SELECT txt, payload FROM values_test WHERE id=?;")
	if err != nil {
		t.Fatal(err)
	}
	var preparedText string
	var preparedBlob []byte
	if err := lookup.QueryRowContext(ctx, int64(2)).Scan(&preparedText, &preparedBlob); err != nil {
		lookup.Close()
		t.Fatal(err)
	}
	if err := lookup.Close(); err != nil {
		t.Fatal(err)
	}
	if preparedText != "สวัสดี 'CubeSQL'" || !bytes.Equal(preparedBlob, []byte{0x00, 0x02, 0xff, 0x00}) {
		t.Fatalf("prepared query mismatch")
	}

	if _, err := database.ExecContext(ctx, "INSERT INTO values_test VALUES (?, ?, ?, ?, ?, ?);",
		int64(3), int64(0), 0.0, "empty blob", []byte{}, nil); err != nil {
		t.Fatal(err)
	}
	var emptyIsNull int64
	var emptyType string
	var emptyLength int64
	if err := second.QueryRow("SELECT payload IS NULL, typeof(payload), length(payload) FROM values_test WHERE id=3;").
		Scan(&emptyIsNull, &emptyType, &emptyLength); err != nil {
		t.Fatal(err)
	}
	if emptyIsNull != 0 || emptyType != "blob" || emptyLength != 0 {
		t.Fatalf("empty BLOB predicates = %d, %q, %d", emptyIsNull, emptyType, emptyLength)
	}
	var ambiguous []byte
	if err := second.QueryRow("SELECT payload FROM values_test WHERE id=3;").Scan(&ambiguous); err != nil {
		t.Fatal(err)
	}
	if ambiguous != nil {
		t.Fatalf("known SDK limitation changed: empty BLOB read = %#v, want current nil result", ambiguous)
	}

	var whole int64
	var score float64
	var text string
	var payload []byte
	var nullable sql.NullString
	if err := second.QueryRow("SELECT whole, score, txt, payload, nullable FROM values_test WHERE id=2;").
		Scan(&whole, &score, &text, &payload, &nullable); err != nil {
		t.Fatal(err)
	}
	if whole != math.MaxInt64 || score != -2.5 || text != "สวัสดี 'CubeSQL'" ||
		!bytes.Equal(payload, []byte{0x00, 0x02, 0xff, 0x00}) || !nullable.Valid || nullable.String != "present" {
		t.Fatalf("prepared round trip mismatch")
	}

	rows, err := second.Query("SELECT whole, score, txt, payload FROM values_test WHERE id=2;")
	if err != nil {
		t.Fatal(err)
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		rows.Close()
		t.Fatal(err)
	}
	metadata := make([]string, len(columnTypes))
	for index := range columnTypes {
		metadata[index] = columnTypes[index].DatabaseTypeName()
	}
	if len(metadata) != 4 || metadata[0] != "INTEGER" ||
		metadata[1] != "TEXT" || metadata[2] != "TEXT" || metadata[3] != "BLOB" {
		rows.Close()
		t.Fatalf("column metadata = %v", metadata)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err = second.Query("SELECT id FROM values_test ORDER BY id;")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids) != "[1 2 3]" {
		t.Fatalf("multiple rows = %v", ids)
	}
	emptyRows, err := second.Query("SELECT id FROM values_test WHERE id=-1;")
	if err != nil {
		t.Fatal(err)
	}
	if emptyRows.Next() {
		emptyRows.Close()
		t.Fatal("empty query returned a row")
	}
	if err := emptyRows.Err(); err != nil {
		emptyRows.Close()
		t.Fatal(err)
	}
	if err := emptyRows.Close(); err != nil {
		t.Fatal(err)
	}

	update, err := database.Exec("UPDATE values_test SET txt='updated' WHERE id=2;")
	if err != nil {
		t.Fatal(err)
	}
	if affected, err := update.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("update RowsAffected = %d, %v", affected, err)
	}
	var updated string
	if err := second.QueryRow("SELECT txt FROM values_test WHERE id=2;").Scan(&updated); err != nil || updated != "updated" {
		t.Fatalf("updated value = %q, %v", updated, err)
	}

	deleted, err := database.Exec("DELETE FROM values_test WHERE id=1;")
	if err != nil {
		t.Fatal(err)
	}
	if affected, err := deleted.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("delete RowsAffected = %d, %v", affected, err)
	}
	if got := scalarInt64(t, second, "SELECT count(*) FROM values_test WHERE id=1;"); got != 0 {
		t.Fatalf("deleted row count = %d", got)
	}

	rolledBack, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rolledBack.ExecContext(ctx, "INSERT INTO values_test VALUES (?, 0, 0, 'rollback', NULL, NULL);", int64(4)); err != nil {
		rolledBack.Rollback()
		t.Fatal(err)
	}
	if err := rolledBack.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := scalarInt64(t, second, "SELECT count(*) FROM values_test WHERE id=4;"); got != 0 {
		t.Fatalf("rolled-back row count = %d", got)
	}

	committed, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := committed.ExecContext(ctx, "INSERT INTO values_test VALUES (?, 0, 0, 'commit', NULL, NULL);", int64(5)); err != nil {
		committed.Rollback()
		t.Fatal(err)
	}
	if err := committed.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := scalarInt64(t, second, "SELECT count(*) FROM values_test WHERE id=5;"); got != 1 {
		t.Fatalf("committed row count = %d", got)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := database.ExecContext(canceled,
		"INSERT INTO values_test VALUES (6, 0, 0, 'canceled', NULL, NULL);"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ExecContext = %v", err)
	}
	if got := scalarInt64(t, second, "SELECT count(*) FROM values_test WHERE id=6;"); got != 0 {
		t.Fatalf("canceled row count = %d", got)
	}

	pooled, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := pooled.Raw(func(raw any) error {
		physical := raw.(*conn)
		physical.mu.Lock()
		defer physical.mu.Unlock()
		_, err := physical.native.Exec("INSERT INTO values_test VALUES (7, 0, 0, 'pending', NULL, NULL);")
		return err
	}); err != nil {
		pooled.Close()
		t.Fatal(err)
	}
	if err := pooled.Close(); err != nil {
		t.Fatal(err)
	}
	if got := scalarInt64(t, database, "SELECT count(*) FROM values_test WHERE id=7;"); got != 0 {
		t.Fatalf("pool reset pending row count = %d", got)
	}
	pooled, err = database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pooled.ExecContext(ctx, "UNSET CURRENT DATABASE;"); err == nil {
		pooled.Close()
		t.Fatal("UNSET CURRENT DATABASE unexpectedly auto-committed without an error")
	}
	if err := pooled.Close(); err != nil {
		t.Fatal(err)
	}
	if got := scalarInt64(t, database, "SELECT count(*) FROM values_test;"); got != 3 {
		t.Fatalf("pool database reset row count = %d, want 3", got)
	}
	stats := database.Stats()
	if stats.OpenConnections != 1 || stats.Idle != 1 {
		t.Fatalf("pool stats after reuse = %+v", stats)
	}
}

func TestIntegrationDatabaseSQLBadConnectionRecovery(t *testing.T) {
	database, _ := openSandbox(t)
	if _, err := database.Exec("CREATE TABLE recovery_test (id INTEGER);"); err != nil {
		t.Fatal(err)
	}
	pooled, err := database.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = pooled.Raw(func(raw any) error {
		physical := raw.(*conn)
		physical.mu.Lock()
		physical.broken = true
		physical.mu.Unlock()
		return driver.ErrBadConn
	})
	if !errors.Is(err, driver.ErrBadConn) {
		pooled.Close()
		t.Fatalf("Raw bad connection = %v", err)
	}
	if err := pooled.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
		t.Fatal(err)
	}
	if got := scalarInt64(t, database, "SELECT count(*) FROM recovery_test;"); got != 0 {
		t.Fatalf("recovered query count = %d", got)
	}
}

func TestIntegrationDatabaseSQLConnectionModesAndRegistration(t *testing.T) {
	config := integrationConfig(t, core.EncryptionAES256)
	config.Database = ""
	aes, err := OpenDB(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := aes.Ping(); err != nil {
		aes.Close()
		t.Fatal(err)
	}
	if err := aes.Close(); err != nil {
		t.Fatal(err)
	}

	clear := integrationConfig(t, core.EncryptionClear)
	clear.Database = ""
	dsn := url.URL{
		Scheme: DriverName,
		User:   url.UserPassword(clear.Options.Username, clear.Options.Password),
		Host:   net.JoinHostPort(clear.Options.Host, strconv.Itoa(clear.Options.Port)),
	}
	query := dsn.Query()
	query.Set("timeout", clear.Options.Timeout.String())
	dsn.RawQuery = query.Encode()
	registered, err := sql.Open(DriverName, dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := registered.Ping(); err != nil {
		registered.Close()
		t.Fatal(err)
	}
	if err := registered.Close(); err != nil {
		t.Fatal(err)
	}

	bad := clear
	bad.Options.Password += "-invalid"
	badDatabase, err := OpenDB(bad)
	if err != nil {
		t.Fatal(err)
	}
	err = badDatabase.Ping()
	badDatabase.Close()
	if !errors.Is(err, core.ErrAuthentication) || errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("bad credentials error = %v", err)
	}
}
