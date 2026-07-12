# CubeSQL Go driver

Go module: `github.com/jedt3d/cubesql-go-driver`

Status: Phase 4 public semantics implementation. The `cubesql` package now has
typed authentication, authorization, network, protocol, server, and timeout
errors plus context-aware preflight methods. The Phase 3 exit gate remains
blocked because SDK 060600 cursor results cannot distinguish an empty BLOB from
SQL NULL. This repository does not contain a `database/sql` adapter and must not
be described as a fully working Go driver yet.

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

Empty BLOB writes use the prepared zeroblob command and are stored correctly:
Server 5.9.6 reports `IS NULL=0`, `typeof=blob`, and `length=0`. However, SDK
060600 returns both that value and a true SQL NULL as a NULL cursor field. The
public API therefore cannot yet preserve their distinction on reads. The SDK's
one-shot zeroblob bind also fails parity and remains rejected. These are
explicit compatibility boundaries, not hidden coercions.
