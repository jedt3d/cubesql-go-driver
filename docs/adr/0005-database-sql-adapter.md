# ADR 0005: Adapt the safe core to database/sql with explicit limitations

Date: 2026-07-13

Status: implemented; Gold Standard blocked by empty-BLOB reads

## Context

The safe core now has deterministic ownership, typed copied values, explicit
transactions, error categories, and preflight context checks. Phase 5 needs a
standard-library adapter without adding a second pool or weakening retry and
transaction guarantees.

SDK 060600 still returns the same NULL cursor representation for a real
zero-length BLOB and SQL NULL. The user directed Phase 5 to proceed while this
remains a documented compatibility limitation. In-flight C calls also remain
non-cancellable.

## Decision

- Register `database/cubesql` as driver name `cubesql` and implement
  `DriverContext`, `Connector`, connection, statement, transaction, rows,
  result, ping, validation, session reset, named-value, and metadata interfaces.
- Support URL DSNs and a preferred `Config`/`OpenDB` path that does not require
  credentials to be assembled into a string.
- Let `database/sql` own all pooling. One adapter connection owns one safe-core
  connection and therefore one SDK connection.
- Commit every successful execution outside an explicit `sql.Tx`. Inside a
  transaction, commit and rollback remain bound to that physical connection.
- Before pooled reuse, roll back pending implicit or abandoned work and restore
  the connector's configured database. A reset failure returns
  `driver.ErrBadConn` so the pool discards the connection.
- Return `driver.ErrBadConn` from safe pre-operation checks such as ping,
  prepare, reset, and validation when a reusable connection is known broken.
  Never return it after an execution may have applied a side effect.
- Validate a prepared query with the native VM and close it immediately. The
  adapter statement retains only the SQL text because SDK 060600 supports one
  current VM per physical connection; execution uses a temporary core bind.
- Accept positional nil, integers converted to int64, float64, string, bool
  converted to int64, BLOB bytes, and `ZeroBlob`. Reject named parameters and
  temporal values until representations are selected.
- Return copied driver values and runtime SDK column metadata. A non-NULL empty
  BLOB is converted to a non-nil empty byte slice if the SDK ever supplies it.

## Verification

Authenticated `database/sql` tests cover registered-driver and connector opens,
clear and AES256 pings, authentication errors, persisted create/direct and
prepared inserts, prepared queries, exact results, int64 boundaries, float,
UTF-8 and quotes, embedded-NUL BLOB, NULL, empty and multiple rows, column
metadata, update/delete, explicit rollback/commit visibility from a second
pool, canceled preflight execution, pending-transaction rollback on reuse,
database restoration on reuse, forced bad-connection replacement, and cleanup
from a separate authenticated connection.

Normal, race, `GOEXPERIMENT=cgocheck2`, and ASan integration modes pass across
the native, core, and adapter packages.

## Consequences

- Applications can use the standard `database/sql` pool and transaction API.
- Context deadlines remain preflight-only once execution enters C; this is not
  full `ExecerContext`/`QueryerContext` cancellation compliance.
- Empty BLOB writes pass server predicates, but reads scan as nil because the
  SDK cursor loses the distinction. Upstream issue #16 remains open.
- Phase 5 implementation is ready for review, but its Gold Standard exit gate
  is not passed and the project cannot yet claim full functional acceptance.
