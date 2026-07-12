// Package cubesql registers the CubeSQL database/sql driver.
//
// Context methods check cancellation before entering the CubeSQL C SDK, but
// the SDK cannot interrupt an in-flight operation. SDK 060600 also reports a
// zero-length BLOB and SQL NULL identically on reads; both limitations remain
// explicit until upstream behavior changes.
package cubesql
