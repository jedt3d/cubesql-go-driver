//go:build cubesql_static

// Package static is the Phase 1 prebuilt-library topology spike.
package static

/*
#cgo CFLAGS: -DCUBESQL_DISABLE_SSL_ENCRYPTION -I${SRCDIR}/../../../third_party/cubesql-sdk
#cgo LDFLAGS: ${SRCDIR}/../../../build/static/libcubesqlclient.a -lz
#include "cubesql.h"
*/
import "C"

// Version returns the version compiled into the prebuilt CubeSQL client archive.
func Version() string {
	return C.GoString(C.cubesql_version())
}
