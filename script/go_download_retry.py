# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Warm the Go module cache and toolchain before tests, retrying only transient network failures.

Usage: python3 script/go_download_retry.py [--tags TAGS] [--without-tests] [--package PATTERN]... <module directory>
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
import time
from collections.abc import Callable, Sequence
from pathlib import Path

GO_DOWNLOAD_ATTEMPTS = 3
GO_DOWNLOAD_INITIAL_DELAY_SECONDS = 5.0

# A permanent failure can also mention a network error, so these markers win over the transient ones.
PERMANENT_GO_DOWNLOAD_FAILURE_MARKERS = (
    "checksum mismatch",
    "security error",
    "404 not found",
    "410 gone",
    "unknown revision",
    "invalid version",
    "missing go.sum entry",
    "malformed module path",
    "x509:",
    "certificate",
)
TRANSIENT_GO_DOWNLOAD_FAILURE_MARKERS = (
    "stream error",
    "internal_error",
    "unexpected eof",
    "connection reset",
    "connection refused",
    "connection lost",
    "tls handshake timeout",
    "i/o timeout",
    "client.timeout exceeded",
    "dial tcp",
    "temporary failure in name resolution",
    "server misbehaving",
    "goaway",
    "500 internal server error",
    "502 bad gateway",
    "503 service unavailable",
    "504 gateway timeout",
)

CommandRunner = Callable[[Sequence[str], Path, dict[str, str]], subprocess.CompletedProcess[str]]


def is_transient_go_download_failure(output: str) -> bool:
    """Reports whether Go's output names a network failure that a later attempt may not repeat."""
    lowered = output.lower()
    if any(marker in lowered for marker in PERMANENT_GO_DOWNLOAD_FAILURE_MARKERS):
        return False
    return any(marker in lowered for marker in TRANSIENT_GO_DOWNLOAD_FAILURE_MARKERS)


def run_go_command(arguments: Sequence[str], directory: Path, environment: dict[str, str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(list(arguments), cwd=directory, env=environment, text=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)


def run_go_download_with_retries(
    arguments: Sequence[str],
    directory: Path,
    environment: dict[str, str],
    *,
    attempts: int = GO_DOWNLOAD_ATTEMPTS,
    initial_delay_seconds: float = GO_DOWNLOAD_INITIAL_DELAY_SECONDS,
    sleep: Callable[[float], None] | None = None,
    runner: CommandRunner = run_go_command,
) -> subprocess.CompletedProcess[str]:
    """Runs a Go command whose network reads may fail transiently, retrying them with doubling delays.

    The command must be safe to repeat, such as `go list -deps`, `go get`, or `go mod tidy`; it may
    write go.mod or go.sum. Returns the last attempt's result; the caller judges its return code.
    Never pass a command that tests, builds, or vets: retrying it could hide a flaky test.
    """
    if attempts < 1:
        raise ValueError("attempts must be at least 1")
    for attempt in range(1, attempts + 1):
        result = runner(arguments, directory, environment)
        if result.returncode == 0 or attempt == attempts or not is_transient_go_download_failure(result.stdout + result.stderr):
            return result
        delay = initial_delay_seconds * 2 ** (attempt - 1)
        print(f"{' '.join(arguments)} in {directory} failed with a transient network error "
              f"(attempt {attempt} of {attempts}); retrying in {delay:g} s:\n{result.stderr.strip()}",
              file=sys.stderr, flush=True)
        (sleep or time.sleep)(delay)
    raise AssertionError("retry loop always returns")


def go_package_prewarm_command(packages: Sequence[str], build_tags: str, should_include_tests: bool) -> list[str]:
    """Returns the `go list -deps` command that loads exactly what the matching build or test loads.

    `go mod download` would fetch the whole module graph, more than a test needs, and fail offline.
    Without -e, a missing go.sum entry fails here as it would in the test.
    """
    command = ["go", "list", "-deps"]
    if should_include_tests:
        command.append("-test")
    if build_tags:
        command.append(f"-tags={build_tags}")
    return [*command, *packages]


def prewarm_go_packages(
    directory: Path,
    environment: dict[str, str],
    *,
    packages: Sequence[str] = ("./...",),
    build_tags: str = "",
    should_include_tests: bool = True,
    sleep: Callable[[float], None] | None = None,
    runner: CommandRunner = run_go_command,
) -> subprocess.CompletedProcess[str]:
    """Downloads the toolchain and the modules that building or testing packages in directory loads.

    Returns the last `go list` result; its stdout lists packages and is not meant for display.
    """
    command = go_package_prewarm_command(packages, build_tags, should_include_tests)
    return run_go_download_with_retries(command, directory, environment, sleep=sleep, runner=runner)


def main(arguments: Sequence[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("directory", type=Path)
    parser.add_argument("--package", action="append", dest="packages", help="package pattern (default ./...)")
    parser.add_argument("--tags", default="", help="build tags of the later test")
    parser.add_argument("--without-tests", action="store_true", help="load only what a build needs")
    options = parser.parse_args(arguments)
    environment = {**os.environ, "GOWORK": "off"}
    result = prewarm_go_packages(options.directory, environment, packages=options.packages or ("./...",),
                                 build_tags=options.tags, should_include_tests=not options.without_tests)
    if result.returncode != 0:
        print(result.stderr, end="", file=sys.stderr, flush=True)
        print(f"{' '.join(result.args)} failed in {options.directory} with exit code {result.returncode}", file=sys.stderr)
    return result.returncode


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
