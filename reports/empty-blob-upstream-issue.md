# Empty-BLOB upstream compatibility issue

Recorded: 2026-07-13 (Asia/Bangkok)

Upstream issue: <https://github.com/cubesql/sdk/issues/16>

Status: open; authoritative SDK/server guidance pending

## Reproduction

- official SDK commit:
  `997c73702f8c1ac5e26972a469eeed19ae05618e`;
- SDK header version: `060600`;
- CubeSQL Server: `5.9.6`;
- SQLite engine: `3.46.1`;
- Ubuntu 26.04 LTS x86_64, glibc 2.43, GCC 15.2.0;
- original upstream integration test not executed;
- registration and license state untouched;
- credentials loaded from the ignored environment file and not logged.

The standalone C program inserts SQL literal `X''` and SQL NULL into its
dedicated sandbox. Independent SQL predicates report:

| Value | `IS NULL` | `typeof` | `length` |
|---|---:|---|---:|
| empty BLOB | 0 | `blob` | 0 |
| SQL NULL | 1 | `null` | NULL |

For both rows, `cubesql_cursor_field` returns a NULL pointer and length `-1`.
The defect is therefore reproduced at the exact upstream pin without the local
SDK ownership patch.

The program drops `go_cubesql_empty_blob_repro.db` and verifies that a new
authenticated connection cannot select it. Sanitized machine-readable evidence
is in `empty-blob-upstream-reproducer.json`.

## Acceptance effect

Phase 5 remains blocked. A `database/sql` driver cannot preserve both empty
`[]byte{}` and SQL NULL while the C SDK cursor exposes them identically. Resume
only after upstream supplies a compatible SDK/server correction or an
authoritative decision changes the acceptance requirement.
