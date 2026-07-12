#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SDK="$ROOT/third_party/cubesql-sdk"
OUT="$ROOT/build/static"
OBJ="$OUT/obj"
CC_BIN="${CC:-cc}"
AR_BIN="${AR:-ar}"

cflags=(
  -O2
  -fPIC
  -DCUBESQL_DISABLE_SSL_ENCRYPTION
  -I"$SDK"
  -I"$SDK/crypt"
)
if [[ "${CUBESQL_STATIC_ASAN:-0}" == "1" ]]; then
  cflags+=(-fsanitize=address -fno-omit-frame-pointer)
fi

mkdir -p "$OBJ"

sources=(
  "$SDK/cubesql.c"
  "$SDK/crypt/pseudorandom.c"
  "$SDK/crypt/aescrypt.c"
  "$SDK/crypt/aeskey.c"
  "$SDK/crypt/aestab.c"
  "$SDK/crypt/base64.c"
  "$SDK/crypt/sha1.c"
)

objects=()
for source in "${sources[@]}"; do
  object="$OBJ/$(basename "${source%.c}").o"
  "$CC_BIN" "${cflags[@]}" -c "$source" -o "$object"
  objects+=("$object")
done

rm -f "$OUT/libcubesqlclient.a"
"$AR_BIN" rcs "$OUT/libcubesqlclient.a" "${objects[@]}"
sha256sum "$OUT/libcubesqlclient.a"
echo "$OUT/libcubesqlclient.a"
