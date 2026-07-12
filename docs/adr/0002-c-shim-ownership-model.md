# ADR 0002: Keep the CubeSQL native boundary private and C-owned

Date: 2026-07-12

Status: accepted for Phase 3 implementation

## Context

The CubeSQL C SDK uses opaque connection, cursor, and prepared-statement
handles. Its one-shot bind API mutates the caller's value, size, and type
arrays, including incrementing non-BLOB sizes to account for a terminal NUL.
Several SDK results, including cursor fields and error messages, point into
native storage whose lifetime ends or whose contents can change on a later SDK
call.

Passing retained Go pointer graphs to those APIs would violate cgo ownership
rules and make close ordering nondeterministic. The driver also needs one place
to serialize calls because the SDK connection is stateful and the public
`database/sql` layer must not mistake a native connection for a concurrent
request multiplexer.

## Decision

Use `internal/csdk` as a narrow, repository-owned C ABI over the pinned and
patched CubeSQL SDK.

- A Go `Conn` owns one C `csqlgo_conn`, serializes every SDK call, and refuses
  to close while tracked cursors or statements remain active.
- `Rows`, `Stmt`, and `Bind` each have one explicit, idempotent close state.
  Closed or nil handles fail in Go without crossing the C boundary.
- Retained one-shot bind values and all of their pointer arrays are allocated
  and freed by C. Before calling `cubesql_bind`, the shim copies the three
  arrays that the SDK mutates; the retained graph remains reusable and has one
  owner.
- Prepared-statement text and BLOB arguments cross C only for the duration of
  the synchronous bind call. The SDK does not retain them.
- Cursor fields, column names, connection failures, and later SDK error text are
  copied before their native storage can be advanced, freed, or reused.
- Trace callbacks and `cubesql_cancel` remain absent. Cancellation cannot be
  claimed until a separate concurrent ASan/race/stress design passes.
- No finalizer or `runtime.AddCleanup` is used. Explicit `Close` is
  authoritative.

## Compatibility findings

The executed Server 5.9.6 / SQLite 3.46.1 / SDK 060600 combination has two BLOB
boundaries that the private API exposes honestly:

- empty BLOB binds are rejected because VM binding produces SQL NULL and the
  one-shot API produces compressed bytes; SQL literal `X''` is also persisted
  by the server as NULL;
- one-shot `CUBESQL_BIND_ZEROBLOB` does not preserve the requested length and is
  rejected, while prepared-statement zeroblob is verified with a persisted
  32-byte value.

CubeSQL DDL may leave a session transaction open. Transaction integration tests
therefore follow the official C baseline and commit after DDL before testing an
explicit begin/rollback or begin/commit sequence.

## Verification

The credential-free lifecycle suite and the authenticated sandbox integration
suite pass in normal, Go race-detector, `GOEXPERIMENT=cgocheck2`, and ASan modes.
The integration suite verifies copied text/BLOB/NULL values, reusable C bind
graphs, prepared statements, prepared zeroblob, transaction rollback and
persisted commit, AES pings under concurrent Go callers, repeated open/close,
copied error stability, and cleanup from a separate connection.

## Consequences

- Public packages cannot expose C pointer types or depend on SDK storage.
- A single native connection is serialized; `database/sql` will provide pooling
  across multiple connections.
- Active-child close errors are deterministic and can be mapped deliberately by
  the public layer.
- Phase 2 ownership gates pass, but this is not public-driver or
  `database/sql` functional acceptance.
- TLS, context cancellation, callbacks, and empty-BLOB policy remain later
  compatibility gates.
