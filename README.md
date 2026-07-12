# CubeSQL Go driver

Go module: `github.com/jedt3d/cubesql-go-driver`

Status: Phase 2 private C shim and ownership model. The native boundary passes
normal, race, `GOEXPERIMENT=cgocheck2`, and ASan integration tests against the
reference host. This repository still does not contain an accepted public Go or
`database/sql` API and must not be described as a working Go driver yet.

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
`Close` calls. Context cancellation, trace callbacks, TLS, and internal pooling
are not implemented.

The pinned SDK/server combination cannot safely round-trip an empty BLOB bind:
empty bind calls are rejected, and Server 5.9.6 persists SQL literal `X''` as
NULL. The SDK's one-shot zeroblob bind also fails parity and is rejected;
prepared-statement zeroblob is verified. These are explicit compatibility
boundaries for the later public API.
