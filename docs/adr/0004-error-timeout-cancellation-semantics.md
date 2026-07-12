# ADR 0004: Expose typed errors and preflight-only context semantics

Date: 2026-07-13

Status: implemented; in-flight cancellation explicitly unsupported

## Context

Phase 4 must give callers stable error categories, measure what the CubeSQL SDK
timeout actually controls, and either prove concurrent cancellation safe or
state that it is unsupported. The public core already serializes native calls
per physical connection, and SDK 060600 ordinary response reads block inside C.

The Phase 3 empty-BLOB blocker also required one more boundary check. The
server stores `X''` as a BLOB with length zero, while the SDK cursor returns the
same NULL representation for that value and for SQL NULL.

## Decision

- Public SDK errors carry an `ErrorKind` and unwrap to stable sentinels for
  authentication, authorization, network, protocol, server SQL, and timeout
  failures. The copied numeric SDK/server code and message remain available.
- Connection-open failure now preserves `cubesql_errcode` before the native
  connection is disconnected. This exposes observed authentication code 7056
  instead of collapsing it to generic `-1`.
- Public `Context` methods check the context before each native call they make.
  A nil context is invalid. Existing non-context methods use a background
  context.
- Context expiration does not retroactively turn a completed write into an
  execution failure. If it occurs between a successful write and metric
  capture, the `Result` accessors report the context error.
- The configured SDK timeout is documented as a socket-connect/write setting,
  not an end-to-end operation deadline. SDK handshake reads use the fixed
  `CONNECT_TIMEOUT` value of five seconds. Ordinary query, statement, cursor,
  and acknowledgement reads pass `NO_TIMEOUT`.
- In-flight cancellation is unsupported. No public or private production
  wrapper for `cubesql_cancel` is retained, and Phase 5 must not map cancellation
  to `driver.ErrBadConn`.

## Evidence

A loopback peer accepted a connection but never sent a CubeSQL handshake.
`OpenContext` used a 100 ms context deadline and a one-second configured
timeout, yet returned SDK timeout code 810 after approximately five seconds.
This confirms that an in-flight context deadline cannot interrupt the fixed
handshake read.

A recursive query was also run with a one-second configured timeout. It did not
return before a 15-second external watchdog, matching the SDK source paths that
use `NO_TIMEOUT` for ordinary reads.

The concurrent cancellation experiment deliberately tore down client sockets
while long recursive queries were active. Client calls returned, but server
work continued and retained the dedicated Phase 2 sandbox database. Fifty
server-side connections remained in `CLOSE-WAIT`, and even the documented
`CLOSE CONNECTION` administrative command could not complete while that work
was running. The experimental cancellation wrapper and stress test were removed
from production code. This is failure evidence, not a sanitizer pass.

The native empty-BLOB regression queries both the value and independent server
predicates. For `X''`, predicates return `IS NULL=0`, `typeof=blob`, and
`length=0`; for SQL NULL they return `IS NULL=1`, `typeof=null`, and NULL length.
The cursor data field is NULL in both cases. The SDK parser only byte-swaps the
server-provided size array and treats wire size `-1` as NULL, so there is no safe
local parsing change that can reconstruct the lost distinction.

## Consequences

- Callers can branch on stable error categories with `errors.Is` and inspect
  exact copied SDK/server details with `errors.As`.
- Canceled contexts prevent side effects when cancellation is observed at a
  preflight boundary. They do not promise interruption once C has started.
- A hung ordinary server response can still block indefinitely. Applications
  needing a hard deadline must isolate and retire the physical connection at a
  higher level, while understanding that server work may continue.
- Phase 4 semantics are defined without overstating cancellation support.
- Phase 5 remains blocked by the empty-BLOB versus SQL NULL protocol result and
  cannot truthfully implement `database/sql` value semantics yet.
