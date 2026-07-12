// Package vendored is a Phase 1 build-topology spike.
//
// It is not the production Go binding and intentionally exposes only the SDK
// version needed to prove deterministic cgo compilation and linking.
package vendored

/*
#cgo CFLAGS: -DCUBESQL_DISABLE_SSL_ENCRYPTION -I${SRCDIR}/../../../third_party/cubesql-sdk -I${SRCDIR}/../../../third_party/cubesql-sdk/crypt
#cgo LDFLAGS: -lz
#include "cubesql.h"
*/
import "C"

// Version returns the version compiled into the official CubeSQL C SDK.
func Version() string {
	return C.GoString(C.cubesql_version())
}
