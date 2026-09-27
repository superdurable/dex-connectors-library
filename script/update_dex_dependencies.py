#!/usr/bin/env python3
# SPDX-License-Identifier: MIT

"""Update stable Dex module and CLI pins used by the Connector library."""

from __future__ import annotations

import argparse
import json
import os
import re
from pathlib import Path


DEX_SDK_MODULE = "github.com/superdurable/dex/sdk-go"
BLOB_CACHE_MODULE = "github.com/superdurable/dex/blob-cache-go"
STABLE_VERSION = re.compile(r"^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
CHECKSUM = re.compile(r"^[0-9a-f]{64}$")


def fail(message: str) -> None:
    raise SystemExit(message)


def canonical_version(value: str, label: str) -> tuple[str, tuple[int, int, int]]:
    match = STABLE_VERSION.fullmatch(value.strip())
    if match is None:
        fail(f"{label} must be a stable semantic version, got: {value}")
    parts = tuple(int(part) for part in match.groups())
    return f"v{parts[0]}.{parts[1]}.{parts[2]}", parts


def canonical_cli_tag(value: str, label: str) -> tuple[str, tuple[int, int, int]]:
    if not value.startswith("cli-"):
        fail(f"{label} must start with cli-, got: {value}")
    version, parts = canonical_version(value.removeprefix("cli-"), label)
    return f"cli-{version}", parts


def module_files(root: Path) -> list[Path]:
    files = [root / "sdkgo" / "go.mod"]
    files.extend(sorted((root / "connectors").glob("**/go.mod")))
    missing = [path for path in files if not path.is_file()]
    if missing:
        fail(f"missing Go module files: {', '.join(str(path) for path in missing)}")
    return files


def module_pattern(module: str) -> re.Pattern[str]:
    return re.compile(
        rf"(?m)^(?P<prefix>[ \t]*(?:require[ \t]+)?{re.escape(module)}[ \t]+)"
        r"(?P<version>v\d+\.\d+\.\d+)"
        r"(?P<suffix>[ \t]*(?://[ \t]*indirect)?[ \t]*)$"
    )


def update_module(
    files: list[Path], module: str, latest: str, latest_parts: tuple[int, int, int]
) -> tuple[str, bool]:
    pattern = module_pattern(module)
    matches: list[tuple[Path, re.Match[str]]] = []
    for path in files:
        match = pattern.search(path.read_text())
        if match is not None:
            matches.append((path, match))
    if not matches:
        fail(f"no Go module pins {module}")
    current_versions = {match.group("version") for _, match in matches}
    if len(current_versions) != 1:
        rendered = ", ".join(sorted(current_versions))
        fail(f"{module} pins are not synchronized: {rendered}")
    current, current_parts = canonical_version(
        current_versions.pop(), f"current {module} version"
    )
    if latest_parts < current_parts:
        fail(f"resolved {module} version {latest} is older than current {current}")
    if latest_parts == current_parts:
        return current, False
    for path, _ in matches:
        content = path.read_text()
        path.write_text(
            pattern.sub(
                lambda match: f'{match.group("prefix")}{latest}{match.group("suffix")}',
                content,
            )
        )
    return current, True


def write_outputs(values: dict[str, str]) -> None:
    output_path = os.environ.get("GITHUB_OUTPUT")
    if output_path:
        with Path(output_path).open("a") as output:
            for key, value in values.items():
                output.write(f"{key}={value}\n")
    print(json.dumps(values, sort_keys=True))


def update(
    root: Path,
    sdk_version: str,
    blob_cache_version: str,
    cli_tag: str,
    cli_checksum: str,
) -> dict[str, str]:
    latest_sdk, latest_sdk_parts = canonical_version(
        sdk_version, "Dex Go SDK version"
    )
    latest_blob_cache, latest_blob_cache_parts = canonical_version(
        blob_cache_version, "Dex Blob Cache version"
    )
    latest_cli, latest_cli_parts = canonical_cli_tag(cli_tag, "Dex CLI tag")
    latest_checksum = cli_checksum.strip().lower()
    if CHECKSUM.fullmatch(latest_checksum) is None:
        fail(f"Dex CLI checksum must be lowercase SHA-256, got: {cli_checksum}")

    files = module_files(root)
    previous_sdk, sdk_changed = update_module(
        files, DEX_SDK_MODULE, latest_sdk, latest_sdk_parts
    )
    previous_blob_cache, blob_cache_changed = update_module(
        files, BLOB_CACHE_MODULE, latest_blob_cache, latest_blob_cache_parts
    )

    cli_path = root / ".dex-compat-version"
    checksum_path = root / "DEX_CLI_LINUX_AMD64_SHA256"
    current_cli, current_cli_parts = canonical_cli_tag(
        cli_path.read_text().strip(), "current Dex CLI tag"
    )
    current_checksum = checksum_path.read_text().strip().lower()
    if CHECKSUM.fullmatch(current_checksum) is None:
        fail("current Dex CLI checksum must be lowercase SHA-256")
    if latest_cli_parts < current_cli_parts:
        fail(f"resolved Dex CLI tag {latest_cli} is older than current {current_cli}")
    if latest_cli_parts == current_cli_parts and latest_checksum != current_checksum:
        fail("published Dex CLI checksum changed without a new release tag")
    cli_changed = latest_cli_parts > current_cli_parts
    if cli_changed:
        cli_path.write_text(latest_cli + "\n")
        checksum_path.write_text(latest_checksum + "\n")

    changed = sdk_changed or blob_cache_changed or cli_changed
    return {
        "changed": str(changed).lower(),
        "sdk_previous": previous_sdk,
        "sdk_latest": latest_sdk,
        "blob_cache_previous": previous_blob_cache,
        "blob_cache_latest": latest_blob_cache,
        "cli_previous": current_cli,
        "cli_latest": latest_cli,
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--sdk-version", required=True)
    parser.add_argument("--blob-cache-version", required=True)
    parser.add_argument("--cli-tag", required=True)
    parser.add_argument("--cli-checksum", required=True)
    arguments = parser.parse_args()
    values = update(
        Path(__file__).resolve().parents[1],
        arguments.sdk_version,
        arguments.blob_cache_version,
        arguments.cli_tag,
        arguments.cli_checksum,
    )
    write_outputs(values)


if __name__ == "__main__":
    main()
