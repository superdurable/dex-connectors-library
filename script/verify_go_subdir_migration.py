# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Check the connector Go directory migration against the branch merge base."""

import argparse
import hashlib
import json
import os
import re
import subprocess
from pathlib import Path


MODULE_PREFIX = "github.com/superdurable/dex-connectors-library/"
SHARED_ENTRIES = {"connector.yaml", "README.md", "logo.svg", "ui", "python", "typescript"}
GENERATED_NAMES = {"dex-connector.json"}


def git(*args: str) -> bytes:
    return subprocess.check_output(("git", *args))


def catalog_directories(path: Path) -> list[str]:
    directories = [line.strip()[2:] for line in path.read_text().splitlines()
                   if line.strip().startswith("- connectors/")]
    if not directories:
        raise ValueError("catalog contains no connectors")
    return directories


def baseline_files(revision: str) -> dict[str, str]:
    result = {}
    for record in git("ls-tree", "-r", "-z", revision, "--", "connectors/").split(b"\0"):
        if not record:
            continue
        metadata, path = record.split(b"\t", 1)
        result[path.decode()] = metadata.split()[2].decode()
    return result


def expected_connector_files(old_files: dict[str, str], directories: list[str]) -> set[str]:
    expected = set()
    directories_by_depth = sorted(directories, key=len, reverse=True)
    for path in old_files:
        directory = next((directory for directory in directories_by_depth
                          if path.startswith(directory + "/")), None)
        if directory is None:
            expected.add(path)
            continue
        relative = path[len(directory) + 1:]
        if relative.split("/", 1)[0] in SHARED_ENTRIES | {"go"}:
            expected.add(path)
        else:
            expected.add(f"{directory}/go/{relative}")
    return expected


def current_connector_files() -> set[str]:
    current = set()
    for raw_path in git("ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "connectors/").split(b"\0"):
        if not raw_path:
            continue
        path = raw_path.decode()
        if Path(path).exists() or Path(path).is_symlink():
            current.add(path)
    return current


def connector_file_set_errors(expected: set[str], current: set[str]) -> list[str]:
    return ([f"{path}: unexpected new connector file" for path in sorted(current - expected)]
            + [f"{path}: expected connector file is missing" for path in sorted(expected - current)])


def git_blob_hash(contents: bytes) -> str:
    return hashlib.sha1(f"blob {len(contents)}\0".encode() + contents).hexdigest()


def scrub_manifest_version(contents: bytes) -> bytes:
    return re.sub(rb"(?m)^  version: v\d+\.\d+\.\d+\s*$", b"  version: VERSION", contents, count=1)


def scrub_install_lines(contents: str) -> str:
    return "\n".join(line for line in contents.split("\n")
                     if "go get " + MODULE_PREFIX + "connectors/" not in line
                     and not line.startswith("Moved from "))


def rewrite_readme_links(contents: str, connector: str, moved_paths: set[str]) -> str:
    def rewrite(match: re.Match[str]) -> str:
        target = match.group(1)
        if target.startswith(("http:", "https:", "#", "/")):
            return match.group(0)
        path, _, fragment = target.partition("#")
        if f"{connector}/{path}" in moved_paths:
            return f"](go/{path}{'#' + fragment if fragment else ''})"
        return match.group(0)
    return re.sub(r"\]\(([^)]+)\)", rewrite, contents)


def verify_connector(directory: str, baseline: str, old_files: dict[str, str]) -> tuple[int, list[str]]:
    errors = []
    has_generated_changes = False
    moved_paths = {path for path in old_files if path.startswith(directory + "/")
                   and path[len(directory) + 1:].split("/", 1)[0] not in SHARED_ENTRIES}
    module_before = MODULE_PREFIX + directory
    module_after = module_before + "/go"
    manifest_path = Path(directory) / "connector.yaml"
    if not manifest_path.is_file():
        errors.append(f"{manifest_path}: missing")
    for old_path, blob_hash in old_files.items():
        if not old_path.startswith(directory + "/"):
            continue
        relative = old_path[len(directory) + 1:]
        if relative.split("/", 1)[0] not in SHARED_ENTRIES:
            new_path = Path(directory) / "go" / relative
            if Path(old_path).exists():
                errors.append(f"{old_path}: old Go path remains")
        else:
            new_path = Path(old_path)
        if not new_path.is_file():
            errors.append(f"{new_path}: missing migrated file")
            continue
        current = new_path.read_bytes()
        if git_blob_hash(current) == blob_hash:
            continue
        previous = git("show", f"{baseline}:{old_path}")
        if relative == "connector.yaml":
            matches = scrub_manifest_version(previous) == scrub_manifest_version(current)
        elif relative == "README.md":
            expected = rewrite_readme_links(previous.decode(), directory, moved_paths)
            expected = expected.replace(module_before, module_after)
            matches = scrub_install_lines(expected) == scrub_install_lines(current.decode())
        elif Path(relative).name in GENERATED_NAMES:
            has_generated_changes = True
            old_document = json.loads(previous)
            new_document = json.loads(current)
            old_document["releaseTag"] = new_document.get("releaseTag")
            matches = old_document == new_document
        elif relative.split("/", 1)[0] in SHARED_ENTRIES:
            matches = False
        elif Path(relative).name.startswith("zz_generated"):
            has_generated_changes = True
            matches = True
        else:
            expected = previous.replace(module_before.encode(), module_after.encode())
            matches = expected == current
            if not matches and relative.endswith("README.md"):
                matches = scrub_install_lines(expected.decode()) == scrub_install_lines(current.decode())
        if not matches:
            errors.append(f"{new_path}: changed beyond permitted migration edits")
    root = Path(directory)
    if (root / "go.mod").exists() or (root / "go.sum").exists():
        errors.append(f"{directory}: root Go module files remain")
    if list(root.glob("*.go")):
        errors.append(f"{directory}: root Go files remain")
    if has_generated_changes:
        command = subprocess.run(("go", "run", "./cmd/connectorctl", "generate", "--check", str(manifest_path)),
                                 env={**os.environ, "GOWORK": "off"}, capture_output=True, text=True, check=False)
        if command.returncode != 0:
            errors.append(f"{directory}: connectorctl generate --check failed: {(command.stderr or command.stdout).strip()}")
    return len(moved_paths), errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", default="origin/main", help="revision used to find the merge base")
    options = parser.parse_args()
    baseline = git("merge-base", "HEAD", options.base).decode().strip()
    files = baseline_files(baseline)
    moved = 0
    errors = []
    directories = catalog_directories(Path("catalog.yaml"))
    errors.extend(connector_file_set_errors(expected_connector_files(files, directories), current_connector_files()))
    if errors:
        for error in errors:
            print(error)
        return 1
    for directory in directories:
        connector_moved, connector_errors = verify_connector(directory, baseline, files)
        moved += connector_moved
        errors.extend(connector_errors)
    print(f"Go subdirectory migration: {len(directories)} connectors, {moved} moved files, {len(errors)} violations; merge base {baseline[:12]}")
    for error in errors:
        print(error)
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
