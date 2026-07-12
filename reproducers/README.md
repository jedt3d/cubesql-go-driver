# Upstream CubeSQL SDK reproducers

## Empty BLOB versus SQL NULL

`empty_blob_null.c` demonstrates that CubeSQL Server preserves a zero-length
BLOB but C SDK cursor 060600 exposes it with the same `NULL` pointer and `-1`
length as SQL NULL.

The reproducer:

- reads connection settings only from `CUBESQL_*` environment variables;
- never prints credentials or complete server information;
- never changes registration or license state;
- writes only to `go_cubesql_empty_blob_repro.db`;
- drops that database before and after execution; and
- verifies absence from a newly authenticated connection.

Run it through the credential-safe wrapper against an SDK checkout at the exact
commit pinned in `sources.lock.json`:

```bash
./scripts/run_empty_blob_reproducer.py \
  --env-file ../.env.local \
  --sdk-dir /path/to/sdk/C_SDK
```

The wrapper refuses an SDK checkout at any other commit and records sanitized
evidence in `reports/empty-blob-upstream-reproducer.json`.

Upstream tracking: [cubesql/sdk issue #16](https://github.com/cubesql/sdk/issues/16).
