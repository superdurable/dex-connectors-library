# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Reject first-release TypeScript examples that import their unpublished package."""

import argparse
import json
import re
import subprocess
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
IMPORT_SPECIFIER_PATTERN = re.compile(r"\b(?:from|import|require)\s*(?:\(\s*)?['\"]([^'\"]+)['\"]")
STABLE_RELEASE_TAG_PATTERN = re.compile(r".*/v\d+\.\d+\.\d+")


def find_unpublished_package_imports(package_directory: Path, package_name: str) -> list[str]:
    """Find example imports that depend on the package already existing on npm."""
    example_directory = package_directory / "examples"
    if not example_directory.is_dir():
        return []

    failures = []
    for example_path in sorted(example_directory.rglob("*")):
        if example_path.suffix not in {".ts", ".tsx", ".mts", ".cts"}:
            continue
        source = example_path.read_text()
        for match in IMPORT_SPECIFIER_PATTERN.finditer(source):
            line_number = source.count("\n", 0, match.start()) + 1
            line_start = source.rfind("\n", 0, match.start()) + 1
            if source[line_start:match.start()].lstrip().startswith(("//", "/*", "*")):
                continue
            specifier = match.group(1)
            if specifier == package_name or specifier.startswith(f"{package_name}/"):
                failures.append(f"{example_path}:{line_number}: imports unpublished {specifier}")
    return failures


def has_reachable_stable_release(package_directory: Path) -> bool:
    """Return whether this package already has a stable release in the branch history."""
    tag_prefix = package_directory.relative_to(REPOSITORY_ROOT).as_posix()
    result = subprocess.run(
        ["git", "tag", "--merged", "HEAD", "--list", f"{tag_prefix}/v*"],
        cwd=REPOSITORY_ROOT,
        check=True,
        capture_output=True,
        text=True,
    )
    return any(STABLE_RELEASE_TAG_PATTERN.fullmatch(tag) for tag in result.stdout.splitlines())


def main() -> int:
    """Check first-release examples in one connector TypeScript package."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("package_directory", type=Path)
    arguments = parser.parse_args()

    package_directory = arguments.package_directory.resolve()
    try:
        package_directory.relative_to(REPOSITORY_ROOT)
    except ValueError:
        parser.error("package directory must be inside this repository")
    if package_directory.name != "typescript":
        parser.error("package directory must end in typescript")
    package_name = json.loads((package_directory / "package.json").read_text())["name"]
    if has_reachable_stable_release(package_directory):
        return 0

    failures = find_unpublished_package_imports(package_directory, package_name)
    for failure in failures:
        print(failure)
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
