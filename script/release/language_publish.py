# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Select language releases, ship Python first releases on GitHub, and upload them to PyPI later."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import posixpath
import re
import subprocess
import sys
import time
import tomllib
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

if __package__:
    from .github_release_inventory import github_json, next_page_url, release_inventory
else:
    from github_release_inventory import github_json, next_page_url, release_inventory

PYPI_FIRST_PUBLISH_LIMIT = 3
GITHUB_FIRST_RELEASE_LIMIT = 15
PYPI_RETRY_DELAYS = (20, 60, 120)
LOOKUP_RETRY_DELAYS = (1, 3, 7)
MAXIMUM_GITHUB_MATRIX_JOBS = 256
LANGUAGE_RELEASE_MANIFEST = "language-release.json"
PENDING_PYPI_NOTES_START = "<!-- pypi-upload-pending:start -->"
PENDING_PYPI_NOTES_END = "<!-- pypi-upload-pending:end -->"
STABLE_VERSION_PATTERN = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")
PYTHON_TAG_SEPARATOR = "/python/"
SOURCE_SHA_PATTERN = re.compile(r"[0-9a-f]{40}")


def has_registry_project(language: str, package: str) -> bool:
    if language == "python":
        url = f"https://pypi.org/pypi/{urllib.parse.quote(package)}/json"
    elif language == "typescript":
        url = f"https://registry.npmjs.org/{urllib.parse.quote(package, safe='')}"
    else:
        raise ValueError(f"unsupported language: {language}")
    return has_registry_url(url, f"{language} project {package}")


def has_registry_url(url: str, description: str, *, sleep=time.sleep) -> bool:
    return request_registry(url, description, lambda _response: True, sleep=sleep) is not None


def read_registry_json(url: str, description: str, *, sleep=time.sleep) -> object | None:
    """Return the registry's JSON document, or None when the registry reports 404."""
    return request_registry(url, description, json.load, sleep=sleep)


def request_registry(url: str, description: str, read_response, *, sleep=time.sleep):
    request = urllib.request.Request(url, headers={"User-Agent": "dex-connector-release/1"})
    for attempt in range(len(LOOKUP_RETRY_DELAYS) + 1):
        try:
            with urllib.request.urlopen(request, timeout=20) as response:
                return read_response(response)
        except urllib.error.HTTPError as error:
            if error.code == 404:
                return None
            if error.code != 429 and error.code < 500:
                raise RuntimeError(f"registry lookup failed for {description}: HTTP {error.code}") from error
            reason = f"HTTP {error.code}"
        except (urllib.error.URLError, TimeoutError) as error:
            reason = str(error)
        except (json.JSONDecodeError, UnicodeDecodeError) as error:
            raise RuntimeError(f"registry lookup failed for {description}: invalid JSON") from error
        if attempt == len(LOOKUP_RETRY_DELAYS):
            raise RuntimeError(f"registry lookup failed for {description} after retries: {reason}")
        sleep(LOOKUP_RETRY_DELAYS[attempt])
    raise AssertionError("unreachable")


def pypi_public_version(version: str) -> str:
    return version.removeprefix("v").replace("-rc.", "rc")


def has_wheel_and_sdist(files: object) -> bool:
    if not isinstance(files, list):
        return False
    package_types = {entry.get("packagetype") for entry in files if isinstance(entry, dict)}
    return {"bdist_wheel", "sdist"} <= package_types


def has_pypi_release_files(package: str, version: str, *, read=read_registry_json) -> bool:
    """Report whether PyPI serves both the wheel and the sdist of one release."""
    url = (f"https://pypi.org/pypi/{urllib.parse.quote(package, safe='')}/"
           f"{urllib.parse.quote(pypi_public_version(version), safe='')}/json")
    release = read(url, f"PyPI release {package} {version}")
    if release is None:
        return False
    if not isinstance(release, dict):
        raise RuntimeError(f"PyPI release lookup returned invalid data for {package} {version}")
    return has_wheel_and_sdist(release.get("urls"))


def list_completed_language_tags() -> set[str]:
    inventory_path = os.environ.get("GH_RELEASE_INVENTORY_PATH")
    inventory = (json.loads(Path(inventory_path).read_text(encoding="utf-8"))
                 if inventory_path else release_inventory())
    if inventory.get("_inventory_unavailable"):
        raise RuntimeError("GitHub release inventory is unavailable")
    return {tag for tag, assets in inventory.items() if "language-release.complete" in assets}


def has_run_project_limit_artifact(repository: str, run_id: str, *, fetch=github_json) -> bool:
    artifact_name = f"pypi-project-limit-{run_id}"
    url = f"https://api.github.com/repos/{repository}/actions/runs/{run_id}/artifacts?per_page=100"
    while url:
        response, links = fetch(url)
        if not isinstance(response, dict):
            raise RuntimeError("PyPI project-limit artifact lookup returned invalid data")
        if any(artifact.get("name") == artifact_name for artifact in response.get("artifacts", [])):
            return True
        url = next_page_url(links)
    return False


def is_release_complete(item: dict[str, str], completed_tags: set[str]) -> bool:
    return item["tag_prefix"] + item["version"] in completed_tags


def select_release_matrices(items: list[dict[str, str]], first_publish_limit: int,
                            *, github_release_limit: int = GITHUB_FIRST_RELEASE_LIMIT,
                            completed_tags: set[str] | None = None,
                            release_created_at: dict[str, str] | None = None,
                            release_package=None,
                            project_exists=has_registry_project,
                            pypi_release_exists=has_pypi_release_files,
                            pending_upload_directory: str | None = None) -> dict[str, object]:
    """Queue Python first releases for GitHub first and completed releases missing from PyPI for upload.

    ``pending_upload_directory`` limits pending uploads to that exact connector directory.
    """
    if first_publish_limit < 1:
        raise ValueError("PyPI first publish limit must be positive")
    if github_release_limit < 1:
        raise ValueError("GitHub first release limit must be positive")
    if pending_upload_directory is not None and not is_connector_directory(pending_upload_directory):
        raise ValueError(f"pending upload directory is not a connector directory: {pending_upload_directory}")
    completed = completed_tags or set()
    updates: list[dict[str, str]] = []
    first: list[dict[str, str]] = []
    github_first: list[dict[str, str]] = []
    deferred: list[dict[str, str]] = []
    pypi_pending = select_pending_pypi_uploads(
        completed, first_publish_limit, deferred, release_created_at=release_created_at or {},
        release_package=release_package or read_release_package, project_exists=project_exists,
        pypi_release_exists=pypi_release_exists, pending_upload_directory=pending_upload_directory)
    github_first_count = 0
    for item in items:
        if is_release_complete(item, completed):
            continue
        try:
            has_project = project_exists(item["language"], item["package"])
        except RuntimeError as error:
            print(f"::warning::{error}; deferring {item['package']} without blocking Go releases", file=sys.stderr)
            item["defer_reason"] = "registry_lookup_failure"
            deferred.append(item)
            continue
        if has_project:
            updates.append(item)
        elif item["language"] != "python":
            item["first_publish"] = True
            first.append(item)
        elif github_first_count >= github_release_limit:
            item["defer_reason"] = "github_release_cap"
            deferred.append(item)
        else:
            item["first_publish"] = True
            item["pypi_deferred_reason"] = "github_first_release"
            github_first.append(item)
            github_first_count += 1
    defer_beyond_matrix_limit([updates, first, github_first], deferred)
    defer_beyond_matrix_limit([pypi_pending], deferred)
    for index, item in enumerate(updates + first + github_first):
        item["artifact_index"] = index
    return {"updates": updates, "first": first, "github_first": github_first, "pypi_pending": pypi_pending,
            "deferred": deferred,
            "pending_count": len(updates) + len(first) + len(github_first) + len(pypi_pending) + len(deferred)}


def select_pending_pypi_uploads(completed_tags: set[str], first_publish_limit: int, deferred: list[dict[str, str]],
                                *, release_created_at: dict[str, str], release_package, project_exists,
                                pypi_release_exists, pending_upload_directory: str | None) -> list[dict[str, str]]:
    """Queue completed Python releases missing from PyPI, oldest GitHub release first, optionally for one directory."""
    python_releases = {tag: release for tag in completed_tags
                       if (release := split_python_connector_tag(tag)) is not None
                       and pending_upload_directory in (None, release[0])}
    # A tag without a recorded creation time sorts after every dated tag.
    python_tags = sorted(python_releases, key=lambda tag: (release_created_at.get(tag) or "￿", tag))
    pending: list[dict[str, str]] = []
    allocated_new_projects: set[str] = set()
    capped_new_projects: set[str] = set()
    for tag in python_tags:
        directory, version = python_releases[tag]
        item: dict[str, object] = {"directory": directory, "tag_prefix": f"{directory}/python/", "version": version,
                                   "language": "python", "github_released": True}
        try:
            item["package"] = release_package(directory, tag)
            if pypi_release_exists(item["package"], version):
                continue
            has_project = project_exists("python", item["package"])
        except (RuntimeError, ValueError) as error:
            print(f"::warning::{error}; checking the PyPI upload of {tag} again next run", file=sys.stderr)
            item["defer_reason"] = "registry_lookup_failure"
            deferred.append(item)
            continue
        package = item["package"]
        if has_project:
            pending.append(item)
        elif package in allocated_new_projects:
            # A later version of a project already queued in this run needs no extra project slot.
            item["first_publish"] = True
            pending.append(item)
        elif package in capped_new_projects or len(allocated_new_projects) >= first_publish_limit:
            capped_new_projects.add(package)
            item["defer_reason"] = "first_publish_cap"
            deferred.append(item)
        else:
            allocated_new_projects.add(package)
            item["first_publish"] = True
            pending.append(item)
    return pending


def split_python_connector_tag(tag: str) -> tuple[str, str] | None:
    """Return the connector directory and stable version of a Python connector release tag, or None."""
    directory, separator, version = tag.rpartition(PYTHON_TAG_SEPARATOR)
    if not separator or not STABLE_VERSION_PATTERN.fullmatch(version) or not is_connector_directory(directory):
        return None
    return directory, version


def is_connector_directory(directory: str) -> bool:
    """Apply the rules of connectorctl validateConnectorDirectory."""
    if (not directory or directory.startswith("/") or posixpath.normpath(directory) != directory
            or "\\" in directory):
        return False
    if not directory.startswith("connectors/") or not directory.removeprefix("connectors/"):
        return False
    return all(segment not in ("", ".", "..") for segment in directory.split("/"))


def read_release_package(directory: str, tag: str) -> str:
    """Read the Python distribution name from the release tag's own pyproject.toml."""
    reachable = subprocess.run(("git", "merge-base", "--is-ancestor", tag, "HEAD"), capture_output=True, check=False)
    if reachable.returncode != 0:
        raise RuntimeError(f"release tag is not reachable from HEAD: {tag}")
    pyproject = subprocess.run(("git", "show", f"{tag}:{directory}/python/pyproject.toml"),
                               capture_output=True, text=True, check=False)
    if pyproject.returncode != 0:
        raise RuntimeError(f"cannot read {directory}/python/pyproject.toml at {tag}")
    name = tomllib.loads(pyproject.stdout).get("project", {}).get("name")
    if not isinstance(name, str) or not name:
        raise ValueError(f"{directory}/python/pyproject.toml at {tag} has no project name")
    return name


def read_release_created_at() -> dict[str, str]:
    """Return GitHub release creation times by tag, or an empty map when the plan recorded none."""
    path = os.environ.get("GH_RELEASE_CREATED_AT_PATH")
    if not path or not Path(path).is_file():
        return {}
    created_at = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(created_at, dict) or any(not isinstance(value, str) for value in created_at.values()):
        raise ValueError("GitHub release creation times must map tags to timestamps")
    return created_at


def defer_beyond_matrix_limit(queues: list[list[dict[str, str]]], deferred: list[dict[str, str]]) -> None:
    """Keep earlier queues whole and defer later entries once one matrix reaches the GitHub job limit."""
    remaining = MAXIMUM_GITHUB_MATRIX_JOBS
    for queue in queues:
        for item in queue[remaining:]:
            item["defer_reason"] = "matrix_limit"
            deferred.append(item)
        del queue[remaining:]
        remaining -= len(queue)


def write_selection_outputs(output_path: Path, selection: dict[str, object], go_matrix: str, go_count: str) -> None:
    built = selection["updates"] + selection["first"] + selection["github_first"]
    with output_path.open("a", encoding="utf-8") as output:
        output.write(f"matrix={go_matrix}\ncount={go_count}\n")
        for name in ("updates", "first", "github_first", "pypi_pending", "deferred"):
            items = selection[name]
            output.write(f"{name}_matrix={json.dumps({'include': items}, separators=(',', ':'))}\n")
            output.write(f"{name}_count={len(items)}\n")
        output.write(f"language_count={len(built)}\n")
        output.write(f"language_matrix={json.dumps({'include': built}, separators=(',', ':'))}\n")
        output.write(f"pending_count={selection['pending_count']}\n")


def is_project_creation_limit(message: str) -> bool:
    return "429" in message and "too many new projects created" in message.lower()


def publish_pypi(assets: list[str], *, delays: tuple[int, ...] = PYPI_RETRY_DELAYS,
                 run=subprocess.run, sleep=time.sleep) -> str:
    command = ("uv", "publish", "--check-url", "https://pypi.org/simple/", *assets)
    for attempt in range(len(delays) + 1):
        result = run(command, capture_output=True, text=True, check=False)
        token = os.environ.get("UV_PUBLISH_TOKEN", "")
        message = (result.stdout + result.stderr).replace(token, "[redacted]") if token else result.stdout + result.stderr
        if result.returncode == 0:
            print(message)
            return "published"
        if not is_project_creation_limit(message):
            raise RuntimeError(f"PyPI upload failed: {message.strip()}")
        if attempt == len(delays):
            print("::notice::Deferred first PyPI publish: PyPI project-creation limit (429 Too many new projects created); the next scheduled run will retry.")
            return "deferred"
        print(f"::warning::PyPI project-creation limit; retrying after {delays[attempt]} seconds")
        sleep(delays[attempt])
    raise AssertionError("unreachable")


def pending_pypi_notes_block(repository: str, tag: str, package: str, version: str, directory: str,
                             wheel_name: str) -> str:
    """Describe GitHub install commands in words that stay true after the PyPI upload."""
    repository_url = f"https://github.com/{repository}"
    wheel_url = f"{repository_url}/releases/download/{urllib.parse.quote(tag, safe='')}/{urllib.parse.quote(wheel_name)}"
    public_version = pypi_public_version(version)
    return "\n".join([
        PENDING_PYPI_NOTES_START,
        "## Install",
        "",
        "This release reached GitHub before PyPI: PyPI limits how many new projects one account can",
        "create each day, so the release workflow uploads these exact archives to PyPI when the limit",
        f"allows. If `pip install {package}=={public_version}` cannot find this version yet, install the",
        "wheel attached to this release:",
        "",
        "```sh",
        f"pip install {wheel_url}",
        "```",
        "",
        "Or build the same version from its release tag:",
        "",
        "```sh",
        f'pip install "{package} @ git+{repository_url}@{tag}#subdirectory={directory}/python"',
        "```",
        PENDING_PYPI_NOTES_END,
        "",
    ])


def add_pending_pypi_notes(notes: str, block: str) -> str:
    """Append the pending PyPI block, replacing an earlier one so reruns stay idempotent."""
    return without_pending_pypi_notes(notes).rstrip("\n") + "\n\n" + block


def published_pypi_notes(notes: str, package: str, version: str) -> str:
    """Replace the pending PyPI block with the registry install command; leave other notes unchanged."""
    if PENDING_PYPI_NOTES_START not in notes:
        return notes
    install = "\n".join(["## Install", "", "```sh", f"pip install {package}=={pypi_public_version(version)}", "```", ""])
    return without_pending_pypi_notes(notes).rstrip("\n") + "\n\n" + install


def without_pending_pypi_notes(notes: str) -> str:
    start = notes.find(PENDING_PYPI_NOTES_START)
    if start < 0:
        return notes
    end = notes.find(PENDING_PYPI_NOTES_END, start)
    if end < 0:
        raise ValueError("release notes have an unterminated pending PyPI block")
    return notes[:start] + notes[end + len(PENDING_PYPI_NOTES_END):].lstrip("\n")


def verify_release_archives(directory: Path, tag: str, package: str, version: str, source_sha: str) -> list[Path]:
    """Check downloaded release files against language-release.json; return the wheel and sdist paths."""
    manifest_path = directory / LANGUAGE_RELEASE_MANIFEST
    manifest_bytes = manifest_path.read_bytes()
    try:
        manifest = json.loads(manifest_bytes)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ValueError(f"{LANGUAGE_RELEASE_MANIFEST} is invalid JSON") from error
    if not isinstance(manifest, dict):
        raise ValueError(f"{LANGUAGE_RELEASE_MANIFEST} must be an object")
    expected_identity = {"language": "python", "package": package, "version": version, "tag": tag}
    actual_identity = {name: manifest.get(name) for name in expected_identity}
    if actual_identity != expected_identity:
        raise ValueError(f"{LANGUAGE_RELEASE_MANIFEST} identity {actual_identity} differs from {expected_identity}")
    if not SOURCE_SHA_PATTERN.fullmatch(source_sha):
        raise ValueError(f"expected source SHA must be a full commit SHA: {source_sha!r}")
    manifest_source_sha = manifest.get("sourceSha")
    if not isinstance(manifest_source_sha, str) or not SOURCE_SHA_PATTERN.fullmatch(manifest_source_sha):
        raise ValueError(f"{LANGUAGE_RELEASE_MANIFEST} sourceSha must be a full commit SHA")
    if manifest_source_sha != source_sha:
        raise ValueError(f"{LANGUAGE_RELEASE_MANIFEST} sourceSha {manifest_source_sha} differs from tag commit {source_sha}")
    artifacts = manifest.get("artifacts")
    if (not isinstance(artifacts, dict) or len(artifacts) != 2 or
            any(not isinstance(name, str) or not isinstance(digest, str) or "/" in name or "\\" in name
                or name.startswith(".") for name, digest in artifacts.items())):
        raise ValueError(f"{LANGUAGE_RELEASE_MANIFEST} must list two archive digests")
    wheels = [name for name in artifacts if name.endswith(".whl")]
    sdists = [name for name in artifacts if name.endswith(".tar.gz")]
    if len(wheels) != 1 or len(sdists) != 1:
        raise ValueError(f"{LANGUAGE_RELEASE_MANIFEST} must list one wheel and one sdist")
    expected_names = set(artifacts) | {name + ".sha256" for name in artifacts} | {
        LANGUAGE_RELEASE_MANIFEST, LANGUAGE_RELEASE_MANIFEST + ".sha256"}
    entries = list(directory.iterdir())
    if any(entry.is_symlink() or not entry.is_file() for entry in entries):
        raise ValueError("downloaded release directory contains non-regular files")
    if {entry.name for entry in entries} != expected_names:
        raise ValueError("downloaded release directory differs from the release manifest")
    expected_manifest_digest = f"{hashlib.sha256(manifest_bytes).hexdigest()}  {LANGUAGE_RELEASE_MANIFEST}\n"
    if (directory / (LANGUAGE_RELEASE_MANIFEST + ".sha256")).read_text(encoding="utf-8") != expected_manifest_digest:
        raise ValueError(f"{LANGUAGE_RELEASE_MANIFEST}.sha256 differs from the downloaded manifest")
    for name, digest in artifacts.items():
        actual_digest = hashlib.sha256((directory / name).read_bytes()).hexdigest()
        if actual_digest != digest:
            raise ValueError(f"{name} SHA-256 differs from {LANGUAGE_RELEASE_MANIFEST}")
        if (directory / (name + ".sha256")).read_text(encoding="utf-8") != f"{digest}  {name}\n":
            raise ValueError(f"{name}.sha256 differs from {LANGUAGE_RELEASE_MANIFEST}")
    return [directory / wheels[0], directory / sdists[0]]


def release_mutation_state(asset_names: list[str] | None, built_source_sha: str, trusted_source_sha: str,
                           remote_tag_sha: str) -> str:
    """Return absent, incomplete, or complete; complete wins before source checks so stale reruns never modify it."""
    if asset_names is not None and "language-release.complete" in asset_names:
        return "complete"
    if not SOURCE_SHA_PATTERN.fullmatch(trusted_source_sha):
        raise ValueError(f"release source must be a full commit SHA: {trusted_source_sha!r}")
    if built_source_sha != trusted_source_sha:
        raise ValueError(f"build artifact source {built_source_sha} differs from release source {trusted_source_sha}; "
                         "rerun the whole workflow instead of a stale publish job")
    if remote_tag_sha and remote_tag_sha != trusted_source_sha:
        raise ValueError(f"remote tag commit {remote_tag_sha} differs from release source {trusted_source_sha}")
    return "absent" if asset_names is None else "incomplete"


def verify_existing_archives(existing_directory: Path, dist_directory: Path) -> None:
    """Require archives already on an incomplete GitHub release to equal the rebuilt archives byte for byte."""
    for existing in existing_directory.iterdir():
        rebuilt = dist_directory / existing.name
        if existing.is_symlink() or not existing.is_file() or not rebuilt.is_file():
            raise ValueError(f"incomplete GitHub release has {existing.name}, which this build did not produce")
        if hashlib.sha256(existing.read_bytes()).digest() != hashlib.sha256(rebuilt.read_bytes()).digest():
            raise ValueError(f"incomplete GitHub release has a different {existing.name}; delete that release to "
                             "rebuild it, because a GitHub-first release has no registry copy to compare")


def pypi_published_language_tags(items: list[dict[str, str]], completed_tags: set[str], *,
                                 release_exists=has_pypi_release_files) -> set[str]:
    """Return each package's latest completed Python tag that PyPI serves; a failed lookup omits it."""
    published: set[str] = set()
    for item in items:
        if item["language"] != "python":
            continue
        prefix = item["tag_prefix"]
        versions = [tag[len(prefix):] for tag in completed_tags
                    if tag.startswith(prefix) and STABLE_VERSION_PATTERN.fullmatch(tag[len(prefix):])]
        if not versions:
            continue
        latest = max(versions, key=lambda version: tuple(int(part) for part in version[1:].split(".")))
        try:
            if release_exists(item["package"], latest):
                published.add(prefix + latest)
        except RuntimeError as error:
            print(f"::warning::{error}; the catalog shows the GitHub install for {prefix + latest}", file=sys.stderr)
    return published


def summarize(expected: list[dict[str, str]], results_directory: Path, deferred: list[dict[str, str]]) -> dict[str, object]:
    results: dict[str, dict[str, str]] = {}
    for result_file in results_directory.rglob("release-result.json"):
        result = json.loads(result_file.read_text(encoding="utf-8"))
        tag = result["tag"]
        if tag in results:
            raise ValueError(f"duplicate release result: {tag}")
        results[tag] = result
    published: list[str] = []
    pypi_pending: list[str] = []
    pypi_pending_reasons: dict[str, str] = {}
    deferred_tags: list[str] = []
    deferred_reasons: dict[str, str] = {}
    failed: list[str] = []
    for item in deferred:
        tag = item["tag_prefix"] + item["version"]
        if item.get("github_released"):
            pypi_pending.append(tag)
            pypi_pending_reasons[tag] = item.get("defer_reason", "unspecified")
        else:
            deferred_tags.append(tag)
            deferred_reasons[tag] = item.get("defer_reason", "unspecified")
    for item in expected:
        tag = item["tag_prefix"] + item["version"]
        result = results.get(tag, {"status": "failed"})
        status = result["status"]
        if status == "published":
            published.append(tag)
        elif status == "pypi_pending" or (status == "deferred" and item.get("github_released")):
            pypi_pending.append(tag)
            pypi_pending_reasons[tag] = result.get("reason") or "unspecified"
        elif status == "deferred":
            deferred_tags.append(tag)
            deferred_reasons[tag] = result.get("reason") or "unspecified"
        else:
            failed.append(tag)
    return {"published": published, "pypi_pending": pypi_pending, "pypi_pending_reasons": pypi_pending_reasons,
            "deferred": deferred_tags, "deferred_reasons": deferred_reasons, "failed": failed,
            "pending_count": len(pypi_pending) + len(deferred_tags) + len(failed)}


def write_step_summary(path: Path, summary: dict[str, object]) -> None:
    with path.open("a", encoding="utf-8") as output:
        output.write("## Language releases\n\n")
        for status, reasons in (("published", None), ("pypi_pending", "pypi_pending_reasons"),
                                ("deferred", "deferred_reasons"), ("failed", None)):
            tags = summary[status]
            labels = [f"{tag} ({summary[reasons][tag]})" for tag in tags] if reasons else tags
            output.write(f"- {status}: {len(tags)} — {', '.join(labels) or 'none'}\n")
        output.write(f"- pending: {summary['pending_count']}\n")


def read_language_matrix(matrix_output: Path) -> tuple[dict[str, str], list[dict[str, str]]]:
    entries = dict(line.split("=", 1) for line in matrix_output.read_text(encoding="utf-8").splitlines())
    return entries, json.loads(entries["language_matrix"])["include"]


def main() -> int:
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    select_parser = commands.add_parser("select")
    select_parser.add_argument("--matrix-output", type=Path, required=True)
    select_parser.add_argument("--github-output", type=Path, required=True)
    select_parser.add_argument("--first-publish-limit", type=int, default=PYPI_FIRST_PUBLISH_LIMIT)
    select_parser.add_argument("--github-release-limit", type=int, default=GITHUB_FIRST_RELEASE_LIMIT)
    select_parser.add_argument("--pending-upload-directory",
                               help="upload only this connector directory's pending PyPI versions")
    completed_parser = commands.add_parser("completed-language-tags")
    completed_parser.add_argument("--output", type=Path, required=True)
    pypi_tags_parser = commands.add_parser("pypi-published-language-tags")
    pypi_tags_parser.add_argument("--matrix-output", type=Path, required=True)
    pypi_tags_parser.add_argument("--completed-language-tags", type=Path, required=True)
    pypi_tags_parser.add_argument("--output", type=Path, required=True)
    limit_parser = commands.add_parser("check-project-limit")
    limit_parser.add_argument("--repository", required=True)
    limit_parser.add_argument("--run-id", required=True)
    limit_parser.add_argument("--github-output", type=Path, required=True)
    publish_parser = commands.add_parser("publish-pypi")
    publish_parser.add_argument("--github-output", type=Path, required=True)
    publish_parser.add_argument("assets", nargs="+")
    pending_notes_parser = commands.add_parser("pending-pypi-notes")
    pending_notes_parser.add_argument("--notes", type=Path, required=True)
    pending_notes_parser.add_argument("--repository", required=True)
    pending_notes_parser.add_argument("--tag", required=True)
    pending_notes_parser.add_argument("--package", required=True)
    pending_notes_parser.add_argument("--version", required=True)
    pending_notes_parser.add_argument("--directory", required=True)
    pending_notes_parser.add_argument("--wheel", type=Path, required=True)
    published_notes_parser = commands.add_parser("published-pypi-notes")
    published_notes_parser.add_argument("--notes", type=Path, required=True)
    published_notes_parser.add_argument("--output", type=Path, required=True)
    published_notes_parser.add_argument("--package", required=True)
    published_notes_parser.add_argument("--version", required=True)
    archives_parser = commands.add_parser("verify-release-archives")
    archives_parser.add_argument("--directory", type=Path, required=True)
    archives_parser.add_argument("--tag", required=True)
    archives_parser.add_argument("--package", required=True)
    archives_parser.add_argument("--version", required=True)
    archives_parser.add_argument("--source-sha", required=True)
    mutation_parser = commands.add_parser("check-release-mutation")
    mutation_parser.add_argument("--release-assets", required=True, help="JSON asset-name list, or null")
    mutation_parser.add_argument("--built-plan", type=Path, required=True)
    mutation_parser.add_argument("--trusted-plan", type=Path, required=True)
    mutation_parser.add_argument("--remote-tag-sha", default="")
    mutation_parser.add_argument("--github-output", type=Path, required=True)
    existing_parser = commands.add_parser("verify-existing-archives")
    existing_parser.add_argument("--existing", type=Path, required=True)
    existing_parser.add_argument("--dist", type=Path, required=True)
    summary_parser = commands.add_parser("summarize")
    summary_parser.add_argument("--selection", type=Path, required=True)
    summary_parser.add_argument("--results-directory", type=Path, required=True)
    summary_parser.add_argument("--github-output", type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.command == "select":
            entries, items = read_language_matrix(args.matrix_output)
            try:
                completed_tags = list_completed_language_tags()
            except RuntimeError as error:
                print(f"::warning::{error}; deferring language selection without blocking Go releases", file=sys.stderr)
                for item in items:
                    item["defer_reason"] = "release_inventory_unavailable"
                selection = {"updates": [], "first": [], "github_first": [], "pypi_pending": [], "deferred": items,
                             "pending_count": len(items)}
            else:
                selection = select_release_matrices(items, args.first_publish_limit,
                                                    github_release_limit=args.github_release_limit,
                                                    completed_tags=completed_tags,
                                                    release_created_at=read_release_created_at(),
                                                    pending_upload_directory=args.pending_upload_directory)
            write_selection_outputs(args.github_output, selection, entries["matrix"], entries["count"])
            Path(os.environ.get("LANGUAGE_SELECTION_PATH", "/tmp/language-selection.json")).write_text(
                json.dumps(selection) + "\n", encoding="utf-8")
            print(f"Language release selection: {len(selection['updates'])} updates, "
                  f"{len(selection['first'])} first publishes, {len(selection['github_first'])} GitHub-first "
                  f"Python releases, {len(selection['pypi_pending'])} pending PyPI uploads, "
                  f"{len(selection['deferred'])} deferred")
        elif args.command == "completed-language-tags":
            completed_tags = list_completed_language_tags()
            args.output.write_text("\n".join(sorted(completed_tags)) + "\n", encoding="utf-8")
        elif args.command == "pypi-published-language-tags":
            _, items = read_language_matrix(args.matrix_output)
            completed_tags = {line for line in args.completed_language_tags.read_text(encoding="utf-8").splitlines() if line}
            published = pypi_published_language_tags(items, completed_tags)
            args.output.write_text("".join(f"{tag}\n" for tag in sorted(published)), encoding="utf-8")
        elif args.command == "check-project-limit":
            if has_run_project_limit_artifact(args.repository, args.run_id):
                with args.github_output.open("a", encoding="utf-8") as output:
                    output.write("status=deferred\nreason=project_creation_limit\n")
                print("::notice::Deferred: this run has already hit the PyPI project-creation limit.")
        elif args.command == "publish-pypi":
            status = publish_pypi(args.assets)
            with args.github_output.open("a", encoding="utf-8") as output:
                output.write(f"status={status}\n")
        elif args.command == "pending-pypi-notes":
            block = pending_pypi_notes_block(args.repository, args.tag, args.package, args.version, args.directory,
                                             args.wheel.name)
            args.notes.write_text(add_pending_pypi_notes(args.notes.read_text(encoding="utf-8"), block), encoding="utf-8")
        elif args.command == "published-pypi-notes":
            args.output.write_text(published_pypi_notes(args.notes.read_text(encoding="utf-8"), args.package,
                                                        args.version), encoding="utf-8")
        elif args.command == "verify-release-archives":
            archives = verify_release_archives(args.directory, args.tag, args.package, args.version, args.source_sha)
            print("\n".join(str(archive) for archive in archives))
        elif args.command == "check-release-mutation":
            asset_names = json.loads(args.release_assets)
            if asset_names is not None and (not isinstance(asset_names, list)
                                            or any(not isinstance(name, str) for name in asset_names)):
                raise ValueError("release assets must be a JSON list of names or null")
            state = release_mutation_state(
                asset_names, json.loads(args.built_plan.read_text(encoding="utf-8"))["source_sha"],
                json.loads(args.trusted_plan.read_text(encoding="utf-8"))["source_sha"], args.remote_tag_sha)
            has_archives = any(name.endswith((".whl", ".tar.gz", ".tgz")) for name in asset_names or [])
            with args.github_output.open("a", encoding="utf-8") as output:
                output.write(f"release_state={state}\nhas_archives={str(has_archives).lower()}\n")
            print(f"Release state: {state}")
        elif args.command == "verify-existing-archives":
            verify_existing_archives(args.existing, args.dist)
        else:
            selection = json.loads(args.selection.read_text(encoding="utf-8"))
            expected = (selection["updates"] + selection["first"] + selection.get("github_first", [])
                        + selection.get("pypi_pending", []))
            summary = summarize(expected, args.results_directory, selection["deferred"])
            with args.github_output.open("a", encoding="utf-8") as output:
                output.write(f"result={json.dumps(summary, separators=(',', ':'))}\n")
                output.write(f"pending_count={summary['pending_count']}\n")
                output.write(f"published_count={len(summary['published'])}\n")
            write_step_summary(Path(os.environ["GITHUB_STEP_SUMMARY"]), summary)
            print(json.dumps(summary))
    except (OSError, ValueError, RuntimeError, KeyError, json.JSONDecodeError) as error:
        print(error, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
