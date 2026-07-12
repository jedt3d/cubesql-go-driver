# CubeSQL Go driver

Go module: `github.com/jedt3d/cubesql-go-driver`

Status: Phase 1 build-topology evaluation. This repository does not yet contain
an accepted Go driver API and must not be described as working based on build or
link tests alone.

The implementation wraps the official CubeSQL C SDK pinned in
`sources.lock.json`. Linux x86_64/glibc/cgo and Ubuntu 26.04 are the first
compatibility and reference targets.

## Current topology checks

```bash
go test ./internal/topology/vendored
./scripts/build_static_sdk.sh
go test -tags cubesql_static ./internal/topology/static
```

Both spikes compile the SDK with `CUBESQL_DISABLE_SSL_ENCRYPTION` and system
zlib. TLS remains an explicit later parity gate.
