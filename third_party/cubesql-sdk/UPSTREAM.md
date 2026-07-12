# CubeSQL C SDK provenance

- Repository: `https://github.com/cubesql/sdk.git`
- Commit: `997c73702f8c1ac5e26972a469eeed19ae05618e`
- Header version: `060600` (6.6.0)
- License: MIT; see `LICENSE`

The vendored source includes the reviewed ownership fix in
`patches/cubesql-sdk-060600-inbuffer-error-reuse.patch`. The exact unpatched SDK
leaks two 20-byte receive buffers in the executed large-result and server-error
integration paths. The patch frees the old heap buffer before the SDK redirects
`db->inbuffer` to its struct-owned static error buffer.

The AES implementation files retain their upstream copyright and license
notices. The build links the system zlib rather than vendoring the SDK's old zlib
copy.
