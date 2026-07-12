.PHONY: test-topologies test-vendored test-static

test-topologies: test-vendored test-static

test-vendored:
	go test -count=1 ./internal/topology/vendored

test-static:
	./scripts/build_static_sdk.sh
	go test -count=1 -tags cubesql_static ./internal/topology/static
