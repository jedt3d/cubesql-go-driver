#!/usr/bin/env python3
"""Build and run the credential-safe upstream empty-BLOB reproducer."""

from __future__ import annotations

import argparse
import datetime
import json
import os
import pathlib
import re
import subprocess
import tempfile


ALLOWED_KEYS = {
    "CUBESQL_HOST",
    "CUBESQL_PORT",
    "CUBESQL_USERNAME",
    "CUBESQL_PASSWORD",
    "CUBESQL_TIMEOUT",
}
SDK_SOURCES = (
    "cubesql.c",
    "crypt/pseudorandom.c",
    "crypt/aescrypt.c",
    "crypt/aeskey.c",
    "crypt/aestab.c",
    "crypt/base64.c",
    "crypt/sha1.c",
)


def load_env_file(path: pathlib.Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for number, raw_line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            raise ValueError(f"invalid environment record on line {number}")
        key, value = line.split("=", 1)
        key = key.strip()
        if key not in ALLOWED_KEYS or not re.fullmatch(r"[A-Z][A-Z0-9_]*", key):
            continue
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        values[key] = value
    return values


def redact(text: str, values: dict[str, str]) -> str:
    for key in ("CUBESQL_PASSWORD", "CUBESQL_USERNAME"):
        value = values.get(key)
        if value:
            text = text.replace(value, "[redacted]")
    return text


def pinned_sdk_commit(root: pathlib.Path) -> str:
    lock = json.loads((root / "sources.lock.json").read_text(encoding="utf-8"))
    return next(item["commit"] for item in lock["sources"] if item["name"] == "cubesql-sdk")


def sdk_commit(sdk_dir: pathlib.Path) -> str:
    result = subprocess.run(
        ["git", "-C", str(sdk_dir), "rev-parse", "HEAD"],
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout.strip()


def write_report(path: pathlib.Path, report: dict[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")


def main() -> int:
    root = pathlib.Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser()
    parser.add_argument("--env-file", type=pathlib.Path, required=True)
    parser.add_argument("--sdk-dir", type=pathlib.Path, default=root / "third_party/cubesql-sdk")
    parser.add_argument(
        "--output",
        type=pathlib.Path,
        default=root / "reports/empty-blob-upstream-reproducer.json",
    )
    args = parser.parse_args()

    values = load_env_file(args.env_file)
    if not values.get("CUBESQL_USERNAME") or "CUBESQL_PASSWORD" not in values:
        parser.error("CubeSQL integration credentials are incomplete")

    sdk_dir = args.sdk_dir.resolve()
    expected_commit = pinned_sdk_commit(root)
    actual_commit = sdk_commit(sdk_dir)
    if actual_commit != expected_commit:
        parser.error("SDK source does not match the pinned commit")

    environment = os.environ.copy()
    environment.update(values)
    source = root / "reproducers/empty_blob_null.c"
    report: dict[str, object] = {
        "recorded_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "sdk_commit": actual_commit,
        "sdk_header_version": "060600",
        "sandbox_database": "go_cubesql_empty_blob_repro.db",
        "credentials_logged": False,
        "original_upstream_integration_executed": False,
    }

    with tempfile.TemporaryDirectory(prefix="cubesql-empty-blob-") as temporary:
        binary = pathlib.Path(temporary) / "empty_blob_null"
        command = [
            os.environ.get("CC", "cc"),
            "-O1",
            "-g",
            "-DCUBESQL_DISABLE_SSL_ENCRYPTION",
            f"-I{sdk_dir}",
            f"-I{sdk_dir / 'crypt'}",
            str(source),
            *(str(sdk_dir / item) for item in SDK_SOURCES),
            "-lz",
            "-o",
            str(binary),
        ]
        compile_result = subprocess.run(command, capture_output=True, text=True, check=False)
        report["compile"] = {
            "exit_code": compile_result.returncode,
            "stderr": redact(compile_result.stderr, values),
        }
        if compile_result.returncode != 0:
            report["passed"] = False
            write_report(args.output, report)
            return compile_result.returncode

        run_result = subprocess.run(
            [str(binary)],
            env=environment,
            capture_output=True,
            text=True,
            timeout=30,
            check=False,
        )
        stdout = redact(run_result.stdout, values)
        stderr = redact(run_result.stderr, values)
        print(stdout, end="")
        if stderr:
            print(stderr, end="", file=os.sys.stderr)
        reproduced = "bug_reproduced=1" in stdout
        cleanup_verified = "cleanup_verified=1" in stdout
        report["run"] = {
            "exit_code": run_result.returncode,
            "stdout": stdout,
            "stderr": stderr,
            "bug_reproduced": reproduced,
            "cleanup_verified": cleanup_verified,
        }
        report["passed"] = run_result.returncode == 0 and reproduced and cleanup_verified
        write_report(args.output, report)
        return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
