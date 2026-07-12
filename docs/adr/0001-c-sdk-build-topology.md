# ADR 0001: Compile vendored CubeSQL SDK source through private cgo packages

Date: 2026-07-12

Status: accepted for Phase 2 implementation

## Context

The driver needs a deterministic Linux x86_64/glibc build that does not depend
on a globally installed CubeSQL client library or files inside CubeSQL Server or
Admin installations. The authoritative source is
`cubesql/sdk@997c73702f8c1ac5e26972a469eeed19ae05618e`, header version `060600`.

The executed Phase 0 baseline also established that this exact SDK needs the
reviewed one-line receive-buffer ownership patch recorded under
`third_party/cubesql-sdk/patches/`. The unpatched failure remains part of the
acceptance evidence; the patch is not an implicit source upgrade.

## Options evaluated

### Vendored source compiled by cgo

The spike compiles the SDK and AES sources from repository-owned paths. C flags
and system zlib linkage are package-local. A normal `go test ./...` needs no
separate client-library build or global library search path.

Observed results:

- normal test: pass;
- race build/test: pass;
- `GOEXPERIMENT=cgocheck2`: pass;
- Go `-asan`: pass;
- `go vet`: pass.

### Prebuilt static archive

The spike compiles the same patched source with `-fPIC`, creates
`build/static/libcubesqlclient.a`, and links it through a `${SRCDIR}`-relative
cgo flag. Two clean normal builds produced the same archive SHA-256:
`ed937a0e3bb9a88d9a4571d0762260591ae8e8ccc8ff25d3002143390b4570e7`.
The ASan-instrumented archive also passed `go test -asan`.

This path is reproducible but requires an explicit build step and a tagged Go
package before tests can link. It is useful for later distribution packaging,
not as the default developer topology.

## Decision

Use vendored patched C source compiled through a private cgo boundary as the
default topology. Retain the static-archive builder as an optional packaging and
diagnostic path.

Phase 2 will move from the version-only spike to `internal/csdk` with a narrow C
shim. Public packages must never expose C pointers. Retained bind graphs will be
C-owned, cursor strings/BLOBs will be copied into Go memory, and every native
handle will have one explicit idempotent owner.

## Consequences

- `go test ./...` is deterministic without a machine-global CubeSQL SDK.
- SDK provenance, patching, and upgrades stay reviewable in the repository.
- System zlib remains the only external native link dependency for clear/AES
  builds.
- TLS remains explicitly unvalidated because the first build uses
  `CUBESQL_DISABLE_SSL_ENCRYPTION`; enabling TLS requires a separate topology and
  parity review.
- The static archive remains available but is not committed.
- Existing SDK multi-character-constant compiler warnings are visible and are
  not silently suppressed.
