# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Check the connector Go directory migration against the branch merge base."""

import argparse
import hashlib
import json
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


def git_blob_hash(contents: bytes) -> str:
    return hashlib.sha1(f"blob {len(contents)}\0".encode() + contents).hexdigest()


def scrub_manifest_version(contents: bytes) -> bytes:
    return re.sub(rb"(?m)^  version: v\d+\.\d+\.\d+\s*$", b"  version: VERSION", contents, count=1)


def scrub_install_lines(contents: str) -> str:
    lines = contents.split("\n")
    if any(line.startswith("Moved from ") for line in lines[:8]):
        index = 1
        while index < len(lines) and (not lines[index] or lines[index].startswith(("Moved from ", "Install the Go module with "))):
            index += 1
        lines = [lines[0], "", *lines[index:]]
    return "\n".join(line for line in lines
                     if "go get " + MODULE_PREFIX + "connectors/" not in line
                     and not line.startswith("Moved from "))


def rewrite_readme_links(contents: str, connector: str, moved_paths: set[str]) -> str:
    def rewrite(match: re.Match[str]) -> str:
        target = match.group(1)
        if target.startswith(("http:", "https:", "#", "/")):
            return match.group(0)
        path, _, fragment = target.partition("#")
        if f"{connector}/{path}" in moved_paths or any(moved.startswith(f"{connector}/{path}/") for moved in moved_paths):
            return f"](go/{path}{'#' + fragment if fragment else ''})"
        return match.group(0)
    return re.sub(r"\]\(([^)]+)\)", rewrite, contents)


def rewrite_moved_readme_links(contents: str, new_path: Path) -> str:
    def rewrite(match: re.Match[str]) -> str:
        target = match.group(1)
        if target.startswith(("http:", "https:", "#", "/")):
            return match.group(0)
        path, _, fragment = target.partition("#")
        if not (new_path.parent / path).exists() and (new_path.parent / ".." / path).exists():
            return f"](../{path}{'#' + fragment if fragment else ''})"
        return match.group(0)
    return re.sub(r"\]\(([^)]+)\)", rewrite, contents)


def rewrite_go_commands(contents: str, connector: str) -> str:
    contents = re.sub(r"(?m)^(cd [^\n]*?)(" + re.escape(connector) + r")(?=[\" ]|$)",
                      lambda match: match.group(1) + connector + "/go", contents)
    return re.sub(r"(?m)^(.*--flow-rendering-dir[^\n]*?)" + re.escape(connector) + r"/build",
                  lambda match: match.group(1) + connector + "/go/build", contents)


def verify_connector(directory: str, baseline: str, old_files: dict[str, str]) -> tuple[int, list[str]]:
    errors = []
    moved_paths = {path for path in old_files if path.startswith(directory + "/")
                   and path[len(directory) + 1:].split("/", 1)[0] not in SHARED_ENTRIES | {"go"}}
    module_before = MODULE_PREFIX + directory
    module_after = module_before + "/go"
    manifest_path = Path(directory) / "connector.yaml"
    version_match = re.search(rb"(?m)^  version: (v\d+\.\d+\.\d+)$", manifest_path.read_bytes())
    if version_match is None:
        raise ValueError(f"{manifest_path}: missing metadata.version")
    version = version_match.group(1)
    if not manifest_path.is_file():
        errors.append(f"{manifest_path}: missing")
    for old_path, blob_hash in old_files.items():
        if not old_path.startswith(directory + "/"):
            continue
        relative = old_path[len(directory) + 1:]
        if relative.split("/", 1)[0] not in SHARED_ENTRIES | {"go"}:
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
            expected = rewrite_go_commands(expected, directory)
            matches = scrub_install_lines(expected) == scrub_install_lines(current.decode())
        elif relative.endswith("dex-connector.json"):
            old_document = json.loads(previous)
            new_document = json.loads(current)
            old_document["releaseTag"] = new_document.get("releaseTag")
            matches = old_document == new_document
        elif relative.split("/", 1)[0] in SHARED_ENTRIES:
            matches = False
        elif Path(relative).name.startswith("zz_generated"):
            matches = True
        else:
            expected = previous.replace(module_before.encode(), module_after.encode())
            if relative.endswith(".go") and "/" not in relative:
                expected = expected.replace(b'os.ReadFile("connector.yaml")', b'os.ReadFile("../connector.yaml")')
                expected = expected.replace(b'readTextFile(t, "connector.yaml")', b'readTextFile(t, "../connector.yaml")')
                expected = expected.replace(b'readTextFile(t, "README.md")', b'readTextFile(t, "../README.md")')
                if relative == "manifest_test.go":
                    expected = re.sub(rb'require\.Equal\(t, "v\d+\.\d+\.\d+", manifest\.Metadata\.Version\)',
                                      b'require.Equal(t, "' + version + b'", manifest.Metadata.Version)', expected)
                expected = expected.replace(b'os.ReadFile("README.md")', b'os.ReadFile("../README.md")')
                expected = expected.replace(b'"README.md", filepath.Join("examples"', b'"../README.md", filepath.Join("examples"')
                expected = expected.replace(b'os.Stat("ui")', b'os.Stat("../ui")')
                expected = expected.replace(b'os.Stat("ui/', b'os.Stat("../ui/')
                expected = expected.replace(b'os.ReadFile("ui/', b'os.ReadFile("../ui/')
            elif relative.endswith(".go") and relative.startswith("examples/"):
                expected = expected.replace(b'filepath.Join("..", "..", "README.md")', b'filepath.Join("..", "..", "..", "README.md")')
                if relative.endswith("readme_test.go"):
                    expected = expected.replace(b'filepath.Dir(filepath.Dir(filepath.Dir(moduleRoot)))',
                                                b'filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(moduleRoot))))')
                    expected = expected.replace((b'The first block runs from ' + directory.encode() + b','),
                                                (b'The first block runs from ' + directory.encode() + b'/go,'))
            matches = expected == current
            if not matches and relative.endswith("README.md"):
                expected_text = expected.decode()
                expected_text = expected_text.replace(directory + "/examples/", directory + "/go/examples/")
                expected_text = re.sub(re.escape(directory) + r"(/examples/[A-Za-z0-9._/-]+)?@v\d+\.\d+\.\d+",
                                       lambda match: directory + "/go" + (match.group(1) or "") + "@" + version.decode(), expected_text)
                expected_text = re.sub(re.escape(module_after) + r"(/examples/[A-Za-z0-9._/-]+)?@v\d+\.\d+\.\d+",
                                       lambda match: module_after + (match.group(1) or "") + "@" + version.decode(), expected_text)
                expected_text = re.sub(re.escape(directory) + r"/v\d+\.\d+\.\d+", directory + "/go/" + version.decode(), expected_text)
                expected_text = re.sub(r"--version v\d+\.\d+\.\d+", "--version " + version.decode(), expected_text)
                expected_text = re.sub(r"Connector `v\d+\.\d+\.\d+`", "Connector `" + version.decode() + "`", expected_text)
                expected_text = re.sub(r"Before `v\d+\.\d+\.\d+` is published", "Before `" + version.decode() + "` is published", expected_text)
                expected_text = rewrite_go_commands(expected_text, directory)
                expected_text = rewrite_moved_readme_links(expected_text, new_path)
                matches = expected_text == current.decode()
        if not matches:
            errors.append(f"{new_path}: changed beyond permitted migration edits")
    root = Path(directory)
    if (root / "go.mod").exists() or (root / "go.sum").exists():
        errors.append(f"{directory}: root Go module files remain")
    if list(root.glob("*.go")):
        errors.append(f"{directory}: root Go files remain")
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
