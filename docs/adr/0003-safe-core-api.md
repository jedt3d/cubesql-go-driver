# ADR 0003: Build the public core as a pure-Go ownership facade

Date: 2026-07-13

Status: implemented; Phase 3 exit gate blocked by SDK empty-BLOB reads

## Context

Phase 2 established a private, sanitizer-clean native boundary. Phase 3 needs a
public API that is useful without leaking C pointers, SDK storage, numeric C
constants, or native lifecycle details. CubeSQL connections and prepared
statements are stateful, DML remains pending until an explicit commit, and SDK
060600 supports only one current prepared VM per connection.

The public API also has to preserve the distinction between SQL NULL and BLOB
bytes. A server-side probe made that boundary more precise: Server 5.9.6 stores
both prepared zeroblob length zero and SQL literal `X''` as a real empty BLOB,
but SDK cursor 060600 reports the field as NULL.

## Decision

Add `cubesql` as a pure-Go facade over `internal/csdk`.

- `Options` uses semantic clear/AES encryption values and `time.Duration` for
  the connection timeout.
- `Conn`, `Rows`, `Stmt`, and `Tx` contain no public native pointer or C type.
- Every public handle has deterministic, idempotent close/completion state.
  Connections reject close, implicit commit, or implicit rollback while public
  children or an explicit transaction remain active.
- `Rows` copies names, metadata, text, and BLOB bytes. It supports iteration,
  seek, raw copied `Value`, and typed scans into int64, float64, string, bytes,
  nullable values, or `any`.
- Public binding accepts exactly `nil`, `int64`, `float64`, `string`, `[]byte`,
  and `ZeroBlob`. Unsupported Go types fail before entering C.
- Empty `[]byte` execution uses a temporary prepared VM and zeroblob length
  zero, avoiding the corrupt one-shot SDK bind path.
- `Stmt.Reset` closes the old VM before re-preparing the SQL because the SDK
  protocol has one current VM; preparing first and closing second closes the
  replacement on the server.
- `Result` captures affected rows and last inserted ID immediately after a
  successful execution. Metric failures are returned by the accessors so an
  already-applied statement is not misreported as an execution failure.
- Ordinary DDL/DML follows SDK session behavior and requires explicit
  `Conn.Commit` when persistence is intended. Explicit transactions use a
  single `Tx` owner.
- Context cancellation, callbacks, TLS, pooling, and `database/sql` remain out
  of this phase.

## Verification

The public package passes credential-free tests plus authenticated sandbox
integration in normal, race, `GOEXPERIMENT=cgocheck2`, and ASan modes. Executed
coverage includes clear/AES connections, bad credentials, create-table proof
from another connection, direct and prepared inserts, exact affected rows and
last ID, int64 min/max, float, UTF-8 and quotes, embedded-NUL and 256 KiB BLOBs,
NULL, empty and multiple-row results, update/delete persistence, statement
reset/reuse, active-child lifecycle guards, explicit commit/rollback visibility,
two physical connections, eight concurrent callers with execution/result
capture serialized as one compound operation, copied SDK errors, and independent
cleanup proof.

Empty-BLOB writes pass server-side predicates: `IS NULL=0`, `typeof=blob`, and
`length=0`. A true SQL NULL reports `IS NULL=1`, `typeof=null`, and NULL length.
Despite that server distinction, the SDK cursor reports both data fields as
NULL.

## Consequences

- The safe public surface and its ownership model are ready for review.
- Phase 3 cannot pass the core Gold Standard while an ordinary BLOB read cannot
  distinguish zero bytes from SQL NULL.
- Phase 4 must investigate an SDK fix, protocol-level correction, or an
  authoritative upstream compatibility decision before `database/sql` can
  preserve required value semantics.
- Passing smoke, CRUD, transaction, sanitizer, and cleanup tests does not allow
  the project to say the driver fully works.
