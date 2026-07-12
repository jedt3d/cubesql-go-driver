# CubeSQL Go driver

Go module: `github.com/jedt3d/cubesql-go-driver`

Status: Phase 5 `database/sql` adapter implementation. The
`database/cubesql` package implements the planned standard-library driver
interfaces and passes persisted CRUD, transaction, pool-reuse, bad-connection,
normal, race, cgocheck2, and ASan integration coverage. The Gold Standard exit
gate remains blocked because SDK 060600 cursor results cannot distinguish an
empty BLOB from SQL NULL. The project must not yet be described as a fully
accepted driver.

The implementation wraps the official CubeSQL C SDK pinned in
`sources.lock.json`. Linux x86_64/glibc/cgo and Ubuntu 26.04 are the first
compatibility and reference targets.

## Current checks

```bash
go test ./internal/topology/vendored
./scripts/build_static_sdk.sh
go test -tags cubesql_static ./internal/topology/static
go test ./...
go vet ./...
./scripts/run_integration_tests.py --env-file ../.env.local --mode normal
./scripts/run_integration_tests.py --env-file ../.env.local --mode race
./scripts/run_integration_tests.py --env-file ../.env.local --mode cgocheck2
./scripts/run_integration_tests.py --env-file ../.env.local --mode asan
```

## database/sql

Import `github.com/jedt3d/cubesql-go-driver/database/cubesql` to register the
`cubesql` driver. A URL DSN has this form:

```text
cubesql://username:password@host:4430/database.db?timeout=12s&encryption=clear
```

`encryption` accepts `clear` or `aes256`. For applications that should not put
credentials in a DSN string, use `database/cubesql.OpenDB` with a `Config` and
the safe core `cubesql.Options` value.

One standard-library driver connection owns one C SDK connection. There is no
internal pool: `database/sql` owns pooling. Executions outside an explicit
`sql.Tx` are committed before returning. Session reset rolls back pending work
and restores the connector's database before a pooled connection is reused.

Both spikes compile the SDK with `CUBESQL_DISABLE_SSL_ENCRYPTION` and system
zlib. TLS remains an explicit later parity gate.

`internal/csdk` is deliberately private. It owns all native handles, retains
only C-allocated bind graphs, copies cursor and error bytes before returning to
Go, serializes calls per connection, and requires deterministic explicit
`Close` calls. Trace callbacks, TLS, and internal pooling are not implemented.

The public `cubesql` package exposes Go-owned `Options`, `Conn`, `Rows`, `Stmt`,
`Tx`, typed values, copied errors, and deterministic lifecycle guards. It
supports clear/AES connections, direct and prepared execution, query/scan,
explicit transactions, session database selection, affected rows, and last
inserted IDs. Ordinary SDK DML remains pending until the caller commits.

`OpenContext` and the `Context` methods check cancellation before every native
call they make. They cannot interrupt a native call after it begins. In
particular, the SDK uses the configured timeout for socket connect and write,
uses a fixed approximately five-second handshake read timeout, and performs
ordinary response reads without a client-side timeout. A context deadline that
expires during one of those reads therefore does not stop the call.

Concurrent `cubesql_cancel` testing closed the client socket but did not stop
server-side SQL work; the abandoned work retained the sandbox database. The
driver consequently exposes no in-flight cancellation API and makes no
cancellation-compliance claim. A canceled context is guaranteed to prevent a
call only when it is already canceled at a checked preflight boundary.

The same preflight-only context behavior applies through `database/sql`; the
adapter cannot meet an in-flight cancellation deadline once execution has
entered the C SDK. Empty BLOB writes are persisted correctly, but reads through
both the core and `database/sql` currently scan as SQL NULL. The reproducer and
upstream tracking are recorded in `reproducers/` and
<https://github.com/cubesql/sdk/issues/16>.

Empty BLOB writes use the prepared zeroblob command and are stored correctly:
Server 5.9.6 reports `IS NULL=0`, `typeof=blob`, and `length=0`. However, SDK
060600 returns both that value and a true SQL NULL as a NULL cursor field. The
public API therefore cannot yet preserve their distinction on reads. The SDK's
one-shot zeroblob bind also fails parity and remains rejected. These are
explicit compatibility boundaries, not hidden coercions.
