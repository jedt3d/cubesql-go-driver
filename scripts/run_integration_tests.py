#!/usr/bin/env python3
"""Run CubeSQL integration tests without evaluating or printing env data."""

from __future__ import annotations

import argparse
import os
import pathlib
import re
import subprocess


ALLOWED_KEYS = {
    "CUBESQL_HOST",
    "CUBESQL_PORT",
    "CUBESQL_USERNAME",
    "CUBESQL_PASSWORD",
    "CUBESQL_TIMEOUT",
}


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


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--env-file", type=pathlib.Path, required=True)
    parser.add_argument(
        "--mode", choices=("normal", "race", "cgocheck2", "asan"), default="normal"
    )
    parser.add_argument(
        "--package", choices=("all", "core", "csdk", "database"), default="all"
    )
    parser.add_argument("--run", help="Go test name regular expression")
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args()

    if not args.env_file.is_file():
        parser.error("environment file does not exist")
    values = load_env_file(args.env_file)
    if not values.get("CUBESQL_USERNAME") or "CUBESQL_PASSWORD" not in values:
        parser.error("CubeSQL integration credentials are incomplete")

    environment = os.environ.copy()
    environment.update(values)
    command = ["go", "test", "-count=1", "-tags", "integration"]
    if args.verbose:
        command.append("-v")
    if args.run:
        command.extend(("-run", args.run))
    if args.mode == "race":
        command.append("-race")
    elif args.mode == "asan":
        command.append("-asan")
    elif args.mode == "cgocheck2":
        environment["GOEXPERIMENT"] = "cgocheck2"
    packages = {
        "all": ("./internal/csdk", "./cubesql", "./database/cubesql"),
        "core": ("./cubesql",),
        "csdk": ("./internal/csdk",),
        "database": ("./database/cubesql",),
    }
    command.extend(packages[args.package])
    return subprocess.run(command, env=environment, check=False).returncode


if __name__ == "__main__":
    raise SystemExit(main())
