#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Exercise current or published Connectors against the latest stable Dex release.

DEX_CLI_VERSION or --dex-version selects an exact Dex CLI release only to reproduce a failure.
"""

from __future__ import annotations

import argparse
import hashlib
import http.client
import json
import os
import platform
import random
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
import zipfile
from pathlib import Path


REPOSITORY = "superdurable/dex-connectors-library"
DEX_REPOSITORY = "superdurable/dex"
SYNTHETIC_VERSION_PREFIX = "v0.0."
STABLE_VERSION = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")
DEX_WEB_COMPATIBILITY_TESTS = Path("test") / "dexcompat"
DEX_WEB_SECURITY_TESTS = ("TestConnectorOAuth", "TestSlackOAuth", "TestConnectorConnectionsAPI", "TestConnectorConnectionStore")
TOP_LEVEL_GO_TEST = re.compile(r"^func (Test\w*)\(\w+ \*testing\.T\)", re.MULTILINE)
CREDENTIAL_ISOLATION_FIXTURE = DEX_WEB_COMPATIBILITY_TESTS / "credential-isolation"
# Dex Web accepts a local release override only under the official module path.
FIXTURE_MODULE_PREFIX = f"github.com/{REPOSITORY}/connectors/dex-compat-fixtures"
CREDENTIAL_ISOLATION_MODULE_PATH = f"{FIXTURE_MODULE_PREFIX}/credential-isolation"
FIXTURE_VERSION = "v0.1.0"
CREDENTIAL_ISOLATION_RELEASE_ENVIRONMENT = "DEX_CONNECTOR_COMPAT_CREDENTIAL_ISOLATION_RELEASE"
DEX_GO_SDK_MODULE = "github.com/superdurable/dex/sdk-go"
CONNECTOR_SDK_DIRECTORY = Path("sdkgo")
CONNECTOR_SDK_MODULE = f"github.com/{REPOSITORY}/sdkgo"
# Vetted one at a time because integration-only and live-only files may declare the same helpers.
GO_VET_BUILD_TAG_SETS = ("", "integration", "live")
GITHUB_REQUEST_ATTEMPTS = 5
TRANSIENT_HTTP_STATUSES = frozenset({429, 500, 502, 503, 504})
# A rate limit whose reset is further away than this fails the request instead of waiting for it.
GITHUB_RATE_LIMIT_MAX_WAIT_SECONDS = 60
GO_MODULE_COPY_IGNORED_NAMES = ("node_modules", "dist", ".git")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("current", "released", "install-dexcli", "latest-go-sdk", "plan"))
    parser.add_argument(
        "--dex-version",
        default=os.environ.get("DEX_CLI_VERSION") or "latest",
        help="Dex CLI release tag such as cli-v1.4.2 for failure reproduction; defaults to the latest stable release",
    )
    parser.add_argument("--output")
    parser.add_argument("--github-output", help="write resolved Dex versions for GitHub Actions jobs")
    parser.add_argument("--connector-tag", default=os.environ.get("CONNECTOR_RELEASE_TAG"))
    parser.add_argument(
        "--connector-directories",
        help="comma-separated repository-relative connector directories; omitted selects every connector",
    )
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    if args.mode == "plan":
        if not args.github_output:
            parser.error("plan requires --github-output")
        write_dex_version_plan(root, args.dex_version, Path(args.github_output))
        return 0
    selected_directories = parse_connector_directories(root, args.connector_directories)
    if args.mode == "latest-go-sdk":
        with tempfile.TemporaryDirectory(prefix="dex-go-sdk-") as temporary:
            verify_latest_dex_go_sdk(root, Path(temporary), selected_directories)
        return 0
    dex_release, expected_digest = dex_release_from_environment(args.dex_version)
    with tempfile.TemporaryDirectory(prefix="dex-compat-") as temporary:
        temporary_root = Path(temporary)
        dexcli = download_dexcli(dex_release, temporary_root, expected_digest)
        if args.mode == "install-dexcli":
            if not args.output:
                parser.error("install-dexcli requires --output")
            output = Path(args.output).resolve()
            install_dexcli(dexcli, output)
            return 0
        artifacts = temporary_root / "connector-artifacts"
        examples = temporary_root / "examples"
        if args.mode == "current":
            build_current_artifacts(root, artifacts, selected_directories)
            test_current_examples(root, examples, dexcli, temporary_root, selected_directories)
        else:
            releases = connector_releases(root, args.connector_tag, selected_directories)
            download_released_artifacts(releases, artifacts)
            test_released_examples(releases, examples, dexcli)
        credential_isolation_release = build_fixture_release(root, CREDENTIAL_ISOLATION_FIXTURE, temporary_root / "credential-isolation-release")
        test_dex_web(dex_release, root, artifacts, credential_isolation_release, temporary_root)
    return 0


def verify_latest_dex_go_sdk(root: Path, temporary_root: Path, connector_directories: list[str] | None) -> None:
    """Builds and vets the Connector SDK and the selected connectors with the latest stable Dex Go SDK.

    Each module is a temporary copy. A connector replaces its released Connector SDK with this checkout's
    source, as an application does when it moves every library to its newest release.
    """
    connector_sdk = copy_go_module(root / CONNECTOR_SDK_DIRECTORY, temporary_root / CONNECTOR_SDK_DIRECTORY)
    version = dex_go_sdk_version_from_environment(connector_sdk)
    print(f"Latest Dex Go SDK: module={DEX_GO_SDK_MODULE} version={version}", flush=True)
    build_and_vet_with_dex_go_sdk(connector_sdk, version)
    failures: list[str] = []
    for manifest in manifests(root, connector_directories):
        connector_directory = manifest.parent.relative_to(root)
        connector = copy_go_module(manifest.parent / "go", temporary_root / connector_directory / "go")
        try:
            run(
                ["go", "mod", "edit", f"-replace={CONNECTOR_SDK_MODULE}={connector_sdk}"],
                connector,
                standalone_go_environment(),
            )
            build_and_vet_with_dex_go_sdk(connector, version)
        except RuntimeError as error:
            failures.append(f"{connector_directory.as_posix()}: {error}")
    if failures:
        raise RuntimeError(f"connectors failed with {DEX_GO_SDK_MODULE}@{version}:\n" + "\n".join(failures))


def copy_go_module(source: Path, destination: Path) -> Path:
    shutil.copytree(source, destination, ignore=shutil.ignore_patterns(*GO_MODULE_COPY_IGNORED_NAMES))
    return destination


def latest_stable_go_module_version(module: str, directory: Path) -> str:
    result = run(["go", "list", "-m", "-json", f"{module}@latest"], directory, standalone_go_environment())
    version = str(json.loads(result.stdout).get("Version", ""))
    if not STABLE_VERSION.fullmatch(version):
        raise RuntimeError(f"latest {module} version is not a stable release: {version or 'none'}")
    return version


def dex_go_sdk_version_from_environment(directory: Path) -> str:
    version = os.environ.get("DEX_GO_SDK_VERSION")
    if version is None:
        return latest_stable_go_module_version(DEX_GO_SDK_MODULE, directory)
    if not version:
        raise RuntimeError("pre-resolved Dex Go SDK plan output is empty")
    if not STABLE_VERSION.fullmatch(version):
        raise RuntimeError(f"invalid pre-resolved Dex Go SDK version: {version}")
    return version


def write_dex_version_plan(root: Path, requested_version: str, github_output: Path) -> None:
    release = resolve_dex_release(requested_version)
    tag = str(release["tag_name"])
    # CI plans and shards currently share ubuntu-latest; this URL is platform-specific.
    archive_name = dexcli_archive_name(tag[4:])
    assets = {str(asset["name"]): asset for asset in release["assets"]}
    archive_url = str(assets[archive_name]["browser_download_url"])
    checksums_url = str(assets["checksums.txt"]["browser_download_url"])
    for name, url in ((archive_name, archive_url), ("checksums.txt", checksums_url)):
        if url != f"https://github.com/{DEX_REPOSITORY}/releases/download/{tag}/{name}":
            raise RuntimeError(f"Dex release asset has an unexpected download URL: {url}")
    with tempfile.TemporaryDirectory(prefix="dex-version-plan-") as temporary:
        checksums = Path(temporary) / "checksums.txt"
        download(checksums_url, checksums)
        digest = checksum_entry(checksums.read_text(), archive_name)
    go_sdk_version = latest_stable_go_module_version(DEX_GO_SDK_MODULE, root / CONNECTOR_SDK_DIRECTORY)
    with github_output.open("a") as output:
        output.write(f"cli_tag={tag}\ncli_asset_url={archive_url}\ncli_sha256={digest}\ngo_sdk_version={go_sdk_version}\n")
    print(f"Dex version plan: cli={tag} sha256={digest} go_sdk={go_sdk_version}", flush=True)


def dex_release_from_environment(requested_version: str) -> tuple[dict[str, object], str | None]:
    tag = os.environ.get("DEX_CLI_RELEASE_TAG")
    archive_url = os.environ.get("DEX_CLI_ASSET_URL")
    digest = os.environ.get("DEX_CLI_SHA256")
    if not any((tag, archive_url, digest)):
        return resolve_dex_release(requested_version), None
    if not all((tag, archive_url, digest)):
        raise RuntimeError("pre-resolved Dex CLI tag, asset URL, and SHA-256 must all be set")
    if not tag.startswith("cli-") or not STABLE_VERSION.fullmatch(tag[4:]):
        raise RuntimeError(f"invalid pre-resolved Dex CLI tag: {tag}")
    archive_name = dexcli_archive_name(tag[4:])
    if archive_url != f"https://github.com/{DEX_REPOSITORY}/releases/download/{tag}/{archive_name}":
        raise RuntimeError(f"invalid pre-resolved Dex CLI asset URL: {archive_url}")
    if not re.fullmatch(r"[0-9a-f]{64}", digest):
        raise RuntimeError("invalid pre-resolved Dex CLI SHA-256")
    if requested_version != "latest" and requested_version not in (tag, tag.removeprefix("cli-")):
        raise RuntimeError(f"pre-resolved Dex CLI {tag} does not match requested {requested_version}")
    return {"tag_name": tag, "assets": [{"name": archive_name, "browser_download_url": archive_url}]}, digest


def build_and_vet_with_dex_go_sdk(module_root: Path, version: str) -> None:
    """Requires the Dex Go SDK version, then builds every package and vets each build-tag file set."""
    environment = standalone_go_environment()
    run(["go", "get", f"{DEX_GO_SDK_MODULE}@{version}"], module_root, environment)
    run(["go", "mod", "tidy"], module_root, environment)
    selected = run(["go", "list", "-m", "-f", "{{.Version}}", DEX_GO_SDK_MODULE], module_root, environment)
    if selected.stdout.strip() != version:
        raise RuntimeError(f"{module_root} selected {DEX_GO_SDK_MODULE}@{selected.stdout.strip()}, not {version}")
    run(["go", "build", "./..."], module_root, environment)
    for tags in GO_VET_BUILD_TAG_SETS:
        run(["go", "vet", f"-tags={tags}", "./..."], module_root, environment)
    print(f"Latest Dex Go SDK: module={module_path(module_root)} version={version} build=ok vet=ok", flush=True)


def standalone_go_environment() -> dict[str, str]:
    return {**os.environ, "GOWORK": "off"}


def github_json(url: str) -> object:
    request = urllib.request.Request(
        url,
        headers={"Accept": "application/vnd.github+json", "User-Agent": "dex-connector-compatibility-gate"},
    )
    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    if token:
        request.add_header("Authorization", f"Bearer {token}")

    def read() -> object:
        with urllib.request.urlopen(request, timeout=60) as response:
            return json.load(response)

    return with_transient_retries(read)


def download(url: str, output: Path) -> None:
    request = urllib.request.Request(url, headers={"User-Agent": "dex-connector-compatibility-gate"})
    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    if token and "api.github.com" in url:
        request.add_header("Authorization", f"Bearer {token}")
        request.add_header("Accept", "application/octet-stream")

    def fetch() -> None:
        with urllib.request.urlopen(request, timeout=120) as response, output.open("wb") as destination:
            shutil.copyfileobj(response, destination)

    try:
        with_transient_retries(fetch)
    except Exception as error:
        raise RuntimeError(f"download failed: {url}: {error}") from error


def with_transient_retries(operation):
    """Runs one GitHub request, retrying rate limits, server errors and dropped connections.

    A release must not fail because GitHub briefly answered 503 or rate-limited
    the many jobs of one run. Other HTTP errors, such as a missing asset or a
    403 that is not a rate limit, fail on the first attempt.
    """
    for attempt in range(1, GITHUB_REQUEST_ATTEMPTS + 1):
        delay = 2 ** attempt
        try:
            return operation()
        except urllib.error.HTTPError as error:
            wait = rate_limit_wait(error)
            if attempt == GITHUB_REQUEST_ATTEMPTS or (wait is None and error.code not in TRANSIENT_HTTP_STATUSES):
                raise
            if wait is not None:
                if wait > GITHUB_RATE_LIMIT_MAX_WAIT_SECONDS:
                    raise RuntimeError(
                        f"GitHub rate limit (HTTP {error.code}): retry in {int(wait)} s, "
                        f"beyond the {GITHUB_RATE_LIMIT_MAX_WAIT_SECONDS} s this gate waits"
                    ) from error
                # Jitter spreads the retries of the jobs that the same limit stopped.
                delay = wait + random.uniform(0, delay)
        except (urllib.error.URLError, http.client.IncompleteRead, ConnectionError, TimeoutError):
            if attempt == GITHUB_REQUEST_ATTEMPTS:
                raise
        time.sleep(delay)
    raise AssertionError("retry loop always returns or raises")


def rate_limit_wait(error: urllib.error.HTTPError) -> float | None:
    """Returns the seconds GitHub asks to wait before retrying, or None when error is not a rate limit.

    GitHub answers a primary or secondary rate limit with 403 or 429 and either
    retry-after or x-ratelimit-remaining: 0 with x-ratelimit-reset (epoch
    seconds); without a reset it asks for at least a minute. A secondary limit
    may carry neither header and only say so in its error message, which also
    asks for at least a minute.
    """
    if error.code not in (403, 429):
        return None
    headers = error.headers or {}
    retry_after = (headers.get("retry-after") or "").strip()
    if retry_after.isdigit():
        return float(retry_after)
    if (headers.get("x-ratelimit-remaining") or "").strip() == "0":
        reset = (headers.get("x-ratelimit-reset") or "").strip()
        return max(0.0, float(reset) - time.time()) if reset.isdigit() else 60.0
    if is_secondary_rate_limit_message(error):
        return 60.0
    return None


def is_secondary_rate_limit_message(error: urllib.error.HTTPError) -> bool:
    """Reports whether GitHub's JSON error message names a secondary rate limit."""
    try:
        body = json.loads(error.read(65536) or b"{}")
    except (OSError, ValueError, AttributeError):
        return False
    message = body.get("message") if isinstance(body, dict) else None
    return isinstance(message, str) and "secondary rate limit" in message.lower()


def stable_key(version: str) -> tuple[int, int, int]:
    match = STABLE_VERSION.fullmatch(version)
    if not match:
        raise ValueError(version)
    return tuple(int(value) for value in match.groups())


def resolve_dex_release(requested_version: str) -> dict[str, object]:
    """Returns the highest stable cli-vX.Y.Z release, or the exact stable release requested for reproduction.

    The tags come from git ls-remote, which does not count against the GitHub
    REST API limit that every job of a CI run shares; only the chosen tag's
    release is read from the API. A release whose archive for this platform or
    checksums.txt is not uploaded yet is skipped, so a release being published
    does not fail the jobs that start meanwhile.
    """
    tags = stable_cli_tags(DEX_REPOSITORY)
    if requested_version != "latest":
        requested_tag = requested_version if requested_version.startswith("cli-") else f"cli-{requested_version}"
        tags = {tag: commit for tag, commit in tags.items() if tag == requested_tag}
    for tag in sorted(tags, key=lambda name: stable_key(name[4:]), reverse=True):
        release = published_release(DEX_REPOSITORY, tag)
        if release is None:
            continue
        uploaded = {str(asset.get("name")) for asset in release.get("assets", [])}
        missing = sorted({dexcli_archive_name(tag[4:]), "checksums.txt"} - uploaded)
        if missing:
            print(f"Dex release {tag} has no {', '.join(missing)} yet; trying the previous release", flush=True)
            continue
        release["resolved_commit"] = tags[tag]
        print(f"Dex release: tag={tag} commit={tags[tag]}", flush=True)
        return release
    raise RuntimeError(f"no stable Dex CLI release found for {requested_version}")


def stable_cli_tags(repository: str) -> dict[str, str]:
    """Returns each stable cli-vX.Y.Z tag of repository with the commit it points to."""
    output = run(["git", "ls-remote", "--tags", f"https://github.com/{repository}.git", "refs/tags/cli-v*"], Path.cwd()).stdout
    targets: dict[str, str] = {}
    peeled: dict[str, str] = {}
    for line in output.splitlines():
        sha, _, reference = line.partition("\t")
        tag = reference.removeprefix("refs/tags/")
        if tag.endswith("^{}"):
            peeled[tag.removesuffix("^{}")] = sha
        else:
            targets[tag] = sha
    return {
        tag: peeled.get(tag, sha) for tag, sha in targets.items()
        if tag.startswith("cli-") and STABLE_VERSION.fullmatch(tag[4:])
    }


def published_release(repository: str, tag: str) -> dict[str, object] | None:
    """Returns tag's published, non-prerelease GitHub release, or None when tag has none."""
    try:
        release = github_json(f"https://api.github.com/repos/{repository}/releases/tags/{tag}")
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return None
        raise
    if not isinstance(release, dict) or release.get("draft") or release.get("prerelease"):
        return None
    return release


def dexcli_archive_name(version: str) -> str:
    """Returns the name of the Dex CLI archive of version for this platform."""
    operating_system = {"Darwin": "darwin", "Linux": "linux"}.get(platform.system())
    architecture = {"x86_64": "amd64", "AMD64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(platform.machine())
    if not operating_system or not architecture:
        raise RuntimeError(f"unsupported platform: {platform.system()} {platform.machine()}")
    return f"dexcli_{version}_{operating_system}_{architecture}.tar.gz"


def download_dexcli(release: dict[str, object], directory: Path, expected_digest: str | None = None) -> Path:
    archive_name = dexcli_archive_name(str(release["tag_name"])[4:])
    assets = {str(asset["name"]): asset for asset in release.get("assets", [])}
    if archive_name not in assets or (expected_digest is None and "checksums.txt" not in assets):
        raise RuntimeError(f"Dex release is missing {archive_name} or checksums.txt")
    archive = directory / archive_name
    download(str(assets[archive_name]["browser_download_url"]), archive)
    if expected_digest is None:
        checksums = directory / "checksums.txt"
        download(str(assets["checksums.txt"]["browser_download_url"]), checksums)
        expected_digest = checksum_entry(checksums.read_text(), archive_name)
    actual = hashlib.sha256(archive.read_bytes()).hexdigest()
    if actual != expected_digest:
        raise RuntimeError(f"Dex CLI checksum mismatch: expected {expected_digest}, got {actual}")
    print(f"Dex CLI: asset={archive_name} sha256={actual}", flush=True)
    with tarfile.open(archive, "r:gz") as archive_file:
        members = [member for member in archive_file.getmembers() if Path(member.name).name == "dexcli" and member.isfile()]
        if len(members) != 1:
            raise RuntimeError("Dex CLI archive must contain exactly one dexcli binary")
        server_members = [member for member in archive_file.getmembers() if Path(member.name).name == "dex-server-zd" and member.isfile()]
        if len(server_members) > 1:
            raise RuntimeError("Dex CLI archive contains multiple dex-server-zd binaries")
        extract_data(archive_file, members + server_members, directory)
        binary = directory / members[0].name
        if server_members:
            server = directory / server_members[0].name
            adjacent_server = binary.with_name("dex-server-zd")
            if server != adjacent_server:
                shutil.copy2(server, adjacent_server)
            adjacent_server.chmod(0o755)
    binary.chmod(0o755)
    return binary


def install_dexcli(binary: Path, output: Path) -> None:
    """Install the CLI and its optional bundled server-zd binary side by side."""
    output.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(binary, output)
    output.chmod(0o755)
    bundled_server = binary.with_name("dex-server-zd")
    if bundled_server.is_file():
        server_output = output.with_name("dex-server-zd")
        shutil.copy2(bundled_server, server_output)
        server_output.chmod(0o755)


def extract_data(archive: tarfile.TarFile, members: list[tarfile.TarInfo], directory: Path) -> None:
    """Extract members with the "data" filter, or its checks on a Python without it (before 3.11.4)."""
    if hasattr(tarfile, "data_filter"):
        archive.extractall(directory, members=members, filter="data")
        return
    root = directory.resolve()
    for member in members:
        target = (root / member.name).resolve()
        if not (member.isfile() or member.isdir()) or member.name.startswith("/") or not target.is_relative_to(root):
            raise RuntimeError(f"archive member {member.name!r} is not a regular file or directory inside the destination")
        member.mode &= 0o755
    archive.extractall(directory, members=members)


def checksum_entry(contents: str, filename: str) -> str:
    for line in contents.splitlines():
        fields = line.split()
        recorded_name = fields[1].lstrip("*").removeprefix("./") if len(fields) == 2 else ""
        if recorded_name == filename and re.fullmatch(r"[0-9a-f]{64}", fields[0]):
            return fields[0]
    raise RuntimeError(f"checksum file has no entry for {filename}")


def parse_connector_directories(root: Path, value: str | None) -> list[str] | None:
    if value is None:
        return None
    requested = [directory for directory in value.split(",") if directory]
    if len(requested) != len(set(requested)):
        raise RuntimeError("connector directory selection contains duplicates")
    available = {
        manifest.parent.relative_to(root).as_posix()
        for manifest in (root / "connectors").glob("**/connector.yaml")
    }
    for directory in requested:
        if directory not in available:
            raise RuntimeError(f"selected directory has no connector manifest: {directory}")
    return requested


def manifests(root: Path, connector_directories: list[str] | None = None) -> list[Path]:
    if connector_directories is None:
        discovered = sorted((root / "connectors").glob("**/connector.yaml"))
    else:
        discovered = [root / directory / "connector.yaml" for directory in connector_directories]
    if not discovered:
        if connector_directories is None:
            raise RuntimeError("no connector manifests found")
        return []
    return discovered


def module_path(module_root: Path) -> str:
    first = (module_root / "go.mod").read_text().splitlines()[0]
    if not first.startswith("module "):
        raise RuntimeError(f"invalid go.mod: {module_root / 'go.mod'}")
    return first.removeprefix("module ").strip()


def build_current_artifacts(root: Path, output: Path, connector_directories: list[str] | None = None) -> None:
    commit = run(["git", "rev-parse", "HEAD"], root).stdout.strip()
    selected_manifests = manifests(root, connector_directories)
    if any((manifest.parent / "ui" / "package-lock.json").exists() for manifest in selected_manifests):
        run(["npm", "ci"], root / "sdk" / "react")
        run(["npm", "run", "build"], root / "sdk" / "react")
    for manifest in selected_manifests:
        connector_root = manifest.parent / "go"
        destination = output / manifest.parent.relative_to(root)
        destination.mkdir(parents=True)
        arguments: list[str] = []
        ui = manifest.parent / "ui"
        if (ui / "package-lock.json").exists():
            run(["npm", "ci"], ui)
            run(["npm", "run", "build"], ui)
            run(["go", "run", "./cmd/connectorctl", "ui-artifact", "--manifest", str(manifest), "--ui-root", str(ui / "dist"), "--output", str(destination / "connector-ui.tgz"), "--digest-output", str(destination / "connector-ui.tgz.sha256")], root)
            arguments = ["--ui-artifact", str(destination / "connector-ui.tgz"), "--ui-digest", str(destination / "connector-ui.tgz.sha256")]
        relative = connector_root.relative_to(root).as_posix()
        version = manifest_version(manifest)
        run(["go", "run", "./cmd/connectorctl", "release-artifact", "--manifest", str(manifest), "--module-path", module_path(connector_root), "--version", version, "--tag", f"{relative}/{version}", "--source-sha", commit, "--output", str(destination / "connector-release.json"), "--digest-output", str(destination / "connector-release.json.sha256"), *arguments], root)
        release = json.loads((destination / "connector-release.json").read_text())
        print(f"Current artifact: connector={release['connectorId']} tag={release['tag']} commit={commit} digest={checksum_entry((destination / 'connector-release.json.sha256').read_text(), 'connector-release.json')}", flush=True)


def manifest_version(manifest: Path) -> str:
    match = re.search(r"(?m)^  version: (v\d+\.\d+\.\d+)\s*$", manifest.read_text())
    if not match:
        raise RuntimeError(f"connector manifest has no stable metadata.version: {manifest}")
    return match.group(1)


def create_module_proxy(module_root: Path, proxy: Path) -> tuple[str, str]:
    name = module_path(module_root)
    files = [
        path for path in sorted(module_root.rglob("*"))
        if path.is_file() and not any(part in GO_MODULE_COPY_IGNORED_NAMES for part in path.relative_to(module_root).parts)
    ]
    digest = hashlib.sha256()
    for file in files:
        digest.update(file.relative_to(module_root).as_posix().encode())
        digest.update(b"\0")
        digest.update(file.read_bytes())
        digest.update(b"\0")
    version = SYNTHETIC_VERSION_PREFIX + str(max(1, int(digest.hexdigest()[:12], 16)))
    version_root = proxy / name / "@v"
    version_root.mkdir(parents=True, exist_ok=True)
    (version_root / "list").write_text(version + "\n")
    (version_root / f"{version}.info").write_text(json.dumps({"Version": version, "Time": "2026-01-01T00:00:00Z"}) + "\n")
    shutil.copy2(module_root / "go.mod", version_root / f"{version}.mod")
    with zipfile.ZipFile(version_root / f"{version}.zip", "w", zipfile.ZIP_DEFLATED) as archive:
        for path in files:
            relative = path.relative_to(module_root).as_posix()
            archive.write(path, f"{name}@{version}/{relative}")
    return name, version


def flow_examples(root: Path, connector_directories: list[str] | None = None) -> list[Path]:
    selected_roots = None if connector_directories is None else [root / directory for directory in connector_directories]
    examples = []
    for source in sorted((root / "connectors").glob("**/examples/**/flow/*.go")):
        if (selected_roots is None or any(connector_root in source.parents for connector_root in selected_roots)) \
                and "GetSteps(" in source.read_text():
            examples.append(source.parent)
    if not examples and connector_directories is None:
        raise RuntimeError("no Flow examples found")
    return examples


def test_current_examples(
    root: Path,
    output: Path,
    dexcli: Path,
    temporary_root: Path,
    connector_directories: list[str] | None = None,
) -> None:
    proxy = temporary_root / "proxy"
    proxies: dict[Path, tuple[str, str]] = {}
    for manifest in manifests(root, connector_directories):
        proxies[manifest.parent] = create_module_proxy(manifest.parent / "go", proxy)
    for example in flow_examples(root, connector_directories):
        connector_root = max((path for path in proxies if path in example.parents), key=lambda path: len(path.parts))
        name, version = proxies[connector_root]
        consumer = output / example_consumer_name(connector_root.relative_to(root).as_posix(), example)
        run_visualize(example, consumer, dexcli, name, version, proxy.as_uri())


def example_consumer_name(connector_directory: str, example: Path) -> str:
    """Names an example's temporary consumer module after its connector directory.

    Several connectors ship an example with the same folder name, such as
    summarize-text, so the example name alone collides.
    """
    connector = connector_directory.removeprefix("connectors/").strip("/").replace("/", "-")
    return f"{connector}-{example.parent.name}"


def run_visualize(source: Path, consumer: Path, dexcli: Path, connector_module: str, version: str, proxy_url: str | None = None) -> None:
    shutil.copytree(source, consumer / "flow")
    run(["go", "mod", "init", f"example.com/dex-compat/{consumer.name}"], consumer)
    run(["go", "mod", "edit", f"-require={connector_module}@{version}"], consumer)
    environment = os.environ.copy()
    environment["GOWORK"] = "off"
    if proxy_url:
        environment["GOPROXY"] = f"{proxy_url},https://proxy.golang.org,direct"
        environment["GONOSUMDB"] = connector_module
    run(["go", "mod", "tidy"], consumer, environment)
    outputs = []
    flow_sources = [path for path in sorted((consumer / "flow").glob("*.go")) if "GetSteps(" in path.read_text()]
    if len(flow_sources) != 1:
        raise RuntimeError(f"expected one Flow source with GetSteps in {source}, found {len(flow_sources)}")
    for run_number in (1, 2):
        destination = consumer / f"definition-{run_number}"
        generated = destination.with_suffix(".json")
        try:
            run([str(dexcli), "visualize", str(flow_sources[0]), "--schema-version", "2.0", "--json", "--out", str(destination)], consumer, environment)
        except RuntimeError:
            if generated.exists():
                print(generated.read_text(), flush=True)
            raise
        outputs.append(generated.read_bytes())
    if outputs[0] != outputs[1]:
        raise RuntimeError(f"visualize output is not deterministic: {source}")
    validate_definition(json.loads(outputs[0]), connector_module, version, source)
    print(f"Visualize: example={source} module={connector_module} version={version} deterministic=true", flush=True)


def validate_definition(definition: object, connector_module: str, version: str, source: Path) -> None:
    if not isinstance(definition, dict) or definition.get("valid") is not True:
        raise RuntimeError(f"schema 2.0 definition is invalid: {source}")
    errors = [diagnostic for diagnostic in definition.get("diagnostics", []) if str(diagnostic.get("severity", "")).lower() == "error"]
    if errors:
        raise RuntimeError(f"schema 2.0 diagnostics contain errors: {source}: {errors}")
    connectors = []
    stack = [definition]
    while stack:
        value = stack.pop()
        if isinstance(value, dict):
            connector = value.get("connector")
            if isinstance(connector, dict) and connector.get("modulePath"):
                connectors.append(connector)
            stack.extend(value.values())
        elif isinstance(value, list):
            stack.extend(value)
    if not connectors:
        raise RuntimeError(f"definition contains no connector nodes: {source}")
    for connector in connectors:
        required = ("connectionName", "operationId", "operationKind")
        if connector.get("modulePath") != connector_module or connector.get("moduleVersion") != version or any(not connector.get(field) for field in required):
            raise RuntimeError(f"invalid connector identity in {source}: {connector}")
    nodes = definition.get("nodes", [])
    edges = definition.get("edges", [])
    connector_nodes = [node for node in nodes if isinstance(node, dict) and node.get("metadata", {}).get("connectorFactory") is True]
    for node in connector_nodes:
        metadata = node.get("metadata", {})
        if not metadata.get("explanation"):
            raise RuntimeError(f"connector Step has no explanation in {source}: {node.get('id')}")
        if not any(edge.get("from") == node.get("id") and edge.get("metadata", {}).get("connectorBranch") is True for edge in edges):
            raise RuntimeError(f"connector Step has no branch targets in {source}: {node.get('id')}")
    source_text = next(path for path in source.glob("*.go") if "GetSteps(" in path.read_text()).read_text()
    if "GetConnectorTriggerBindings" in source_text and not definition.get("v2", {}).get("connectorTriggerBindings"):
        raise RuntimeError(f"connector Trigger bindings are missing in {source}")
    if "ResultAttribute:" in source_text and not any(edge.get("kind") == "resource_write" and edge.get("from") in {node.get("id") for node in connector_nodes} for edge in edges):
        raise RuntimeError(f"connector Result Attribute edge is missing in {source}")
    if "ProgressStream:" in source_text and not any("stream" in str(edge.get("kind", "")).lower() for edge in edges):
        raise RuntimeError(f"connector progress Stream edge is missing in {source}")


def connector_releases(
    root: Path,
    only_tag: str | None,
    connector_directories: list[str] | None = None,
) -> list[dict[str, object]]:
    directories = sorted(manifest.parent.relative_to(root).as_posix() for manifest in manifests(root, connector_directories))
    if only_tag:
        version = only_tag.rsplit("/", 1)[-1]
        if not STABLE_VERSION.fullmatch(version) or not any(
            only_tag in (f"{directory}/{version}", f"{directory}/go/{version}") for directory in directories
        ):
            raise RuntimeError(f"published connector release not found: {only_tag}")
        release = published_release(REPOSITORY, only_tag)
        if release is None or release.get("tag_name") != only_tag:
            raise RuntimeError(f"published connector release not found: {only_tag}")
        return [release]
    tags = stable_connector_tags(directories)
    selected: list[dict[str, object]] = []
    for directory in directories:
        for tag in tags[directory]:
            release = published_release(REPOSITORY, tag)
            if release is None or release.get("tag_name") != tag:
                print(f"Skipping {tag}: no published stable release", flush=True)
            elif not any(asset.get("name") == "connector-release.complete" for asset in release.get("assets", [])):
                print(f"Skipping {tag}: missing connector-release.complete", flush=True)
            else:
                selected.append(release)
                break
        else:
            print(f"No completed release for {directory}; skipping its released compatibility", flush=True)
    return selected


def stable_connector_tags(directories: list[str]) -> dict[str, list[str]]:
    """Lists stable Go tags for each connector, newest first across both tag prefixes."""
    tags = {directory: [] for directory in directories}
    if not directories:
        return tags
    patterns = [f"refs/tags/{directory}/{prefix}v*" for directory in directories for prefix in ("", "go/")]
    output = run(
        ["git", "ls-remote", "--refs", "--tags", f"https://github.com/{REPOSITORY}.git", *patterns],
        Path.cwd(),
        should_print_stdout=False,
    ).stdout
    for line in output.splitlines():
        _, _, reference = line.partition("\t")
        tag = reference.removeprefix("refs/tags/")
        prefix, _, version = tag.rpartition("/")
        directory = prefix if prefix in tags else connector_directory_of_tag(tag)
        if directory in tags and STABLE_VERSION.fullmatch(version) and tag in (f"{directory}/{version}", f"{directory}/go/{version}"):
            tags[directory].append(tag)
    print(f"Listed {sum(len(listed) for listed in tags.values())} stable connector tags", flush=True)
    for directory in directories:
        tags[directory].sort(key=lambda tag: (stable_key(tag.rsplit("/", 1)[1]), "/go/" in tag), reverse=True)
    return tags


def connector_directory_of_tag(tag: str) -> str:
    """Returns the connector directory of a legacy or Go-prefixed connector release tag."""
    return tag.rsplit("/", 1)[0].removesuffix("/go")


def download_released_artifacts(releases: list[dict[str, object]], output: Path) -> None:
    for release in releases:
        tag = str(release["tag_name"])
        # Dex Web compatibility globs connector directories, so both tag prefixes share one layout.
        destination = output / connector_directory_of_tag(tag)
        destination.mkdir(parents=True)
        assets = {str(asset["name"]): asset for asset in release.get("assets", [])}
        required = ["connector-release.json", "connector-release.json.sha256"]
        for name in required:
            if name not in assets:
                raise RuntimeError(f"{tag} is missing {name}")
            download(str(assets[name]["browser_download_url"]), destination / name)
        digest = checksum_entry((destination / "connector-release.json.sha256").read_text(), "connector-release.json")
        if hashlib.sha256((destination / "connector-release.json").read_bytes()).hexdigest() != digest:
            raise RuntimeError(f"{tag} release metadata checksum mismatch")
        metadata = json.loads((destination / "connector-release.json").read_text())
        if metadata.get("tag") != tag:
            raise RuntimeError(f"{tag} release metadata identity mismatch")
        if metadata.get("ui"):
            for name in (metadata["ui"]["artifact"], metadata["ui"]["artifact"] + ".sha256"):
                if name not in assets:
                    raise RuntimeError(f"{tag} is missing {name}")
                download(str(assets[name]["browser_download_url"]), destination / name)
            ui_name = metadata["ui"]["artifact"]
            actual_ui_digest = hashlib.sha256((destination / ui_name).read_bytes()).hexdigest()
            published_ui_digest = checksum_entry((destination / f"{ui_name}.sha256").read_text(), ui_name)
            if actual_ui_digest != metadata["ui"]["sha256"] or actual_ui_digest != published_ui_digest:
                raise RuntimeError(f"{tag} UI checksum mismatch")
        print(f"Released artifact: connector={metadata['connectorId']} tag={tag} commit={metadata['sourceSha']} digest={digest}", flush=True)


def test_released_examples(releases: list[dict[str, object]], output: Path, dexcli: Path) -> None:
    for release in releases:
        tag = str(release["tag_name"])
        prefix, version = tag.rsplit("/", 1)
        connector_module = f"github.com/superdurable/dex-connectors-library/{prefix}"
        module = json.loads(run(["go", "mod", "download", "-json", f"{connector_module}@{version}"], output.parent, {**os.environ, "GOWORK": "off"}).stdout)
        module_root = Path(module["Dir"])
        released_examples = []
        for source in sorted((module_root / "examples").glob("**/flow/*.go")):
            if "GetSteps(" in source.read_text():
                released_examples.append(source.parent)
        for example in released_examples:
            run_visualize(example, output / example_consumer_name(prefix, example), dexcli, connector_module, version)


def fixture_module_path(fixture: Path) -> str:
    return f"{FIXTURE_MODULE_PREFIX}/{fixture.name}"


def build_fixture_release(root: Path, fixture: Path, output: Path) -> Path:
    """Builds a fixture release; fixtures sit outside connectors/, so catalog and example checks skip them."""
    manifest = root / fixture / "connector.yaml"
    output.mkdir(parents=True)
    commit = run(["git", "rev-parse", "HEAD"], root).stdout.strip()
    module_path = fixture_module_path(fixture)
    tag = module_path.removeprefix(f"github.com/{REPOSITORY}/") + "/" + FIXTURE_VERSION
    arguments = ["go", "run", "./cmd/connectorctl", "release-artifact", "--manifest", str(manifest), "--module-path", module_path, "--version", FIXTURE_VERSION, "--tag", tag, "--source-sha", commit, "--output", str(output / "connector-release.json"), "--digest-output", str(output / "connector-release.json.sha256")]
    if (root / fixture / "ui").is_dir():
        ui_artifact = output / "connector-ui.tgz"
        ui_digest = output / "connector-ui.tgz.sha256"
        run(["go", "run", "./cmd/connectorctl", "ui-artifact", "--manifest", str(manifest), "--ui-root", str(root / fixture / "ui"), "--output", str(ui_artifact), "--digest-output", str(ui_digest)], root)
        arguments += ["--ui-artifact", str(ui_artifact), "--ui-digest", str(ui_digest)]
    run(arguments, root)
    print(f"Fixture {fixture.name}: tag={tag} commit={commit} digest={checksum_entry((output / 'connector-release.json.sha256').read_text(), 'connector-release.json')}", flush=True)
    return output


def test_dex_web(
    dex_release: dict[str, object],
    root: Path,
    artifacts: Path,
    credential_isolation_release: Path,
    temporary_root: Path,
) -> None:
    tag = str(dex_release["tag_name"])
    source_archive = temporary_root / "dex-source.tar.gz"
    download(f"https://github.com/{DEX_REPOSITORY}/archive/refs/tags/{tag}.tar.gz", source_archive)
    with tarfile.open(source_archive, "r:gz") as archive:
        extract_data(archive, archive.getmembers(), temporary_root)
    candidates = [path for path in temporary_root.iterdir() if path.is_dir() and (path / "web" / "go.mod").exists()]
    if len(candidates) != 1:
        raise RuntimeError("Dex source archive has an unexpected layout")
    web = candidates[0] / "web"
    compatibility_sources = dex_web_compatibility_sources(root)
    for source in compatibility_sources:
        shutil.copy2(source, web / source.name.removesuffix(".txt"))
    run(["npm", "ci"], web)
    run(["npm", "run", "build"], web)
    environment = os.environ.copy()
    environment["GOWORK"] = "off"
    environment["GOTOOLCHAIN"] = "auto"
    environment["DEX_CONNECTOR_COMPAT_ARTIFACT_ROOT"] = str(artifacts)
    environment[CREDENTIAL_ISOLATION_RELEASE_ENVIRONMENT] = str(credential_isolation_release)
    compatibility_tests = dex_web_compatibility_test_names(compatibility_sources)
    result = run(["go", "test", ".", "-run", "|".join((*compatibility_tests, *DEX_WEB_SECURITY_TESTS)), "-count=1", "-v"], web, environment)
    require_dex_web_tests_passed(result.stdout, compatibility_tests)
    require_dex_web_security_tests_passed(result.stdout, DEX_WEB_SECURITY_TESTS)


def dex_web_compatibility_sources(root: Path) -> list[Path]:
    sources = sorted((root / DEX_WEB_COMPATIBILITY_TESTS).glob("*_test.go.txt"))
    if not sources:
        raise RuntimeError(f"no Dex Web compatibility tests found in {DEX_WEB_COMPATIBILITY_TESTS}")
    return sources


def dex_web_compatibility_test_names(sources: list[Path]) -> list[str]:
    names = []
    for source in sources:
        source_names = TOP_LEVEL_GO_TEST.findall(source.read_text())
        if not source_names:
            raise RuntimeError(f"Dex Web compatibility source declares no top-level tests: {source.name}")
        names.extend(source_names)
    return names


def require_dex_web_tests_passed(output: str, names: list[str]) -> None:
    """Fails when a compatibility test did not pass, including when -run selected nothing."""
    for name in names:
        if not re.search(rf"^--- PASS: {re.escape(name)} \(", output, re.MULTILINE):
            raise RuntimeError(f"Dex Web compatibility test did not pass: {name}")


def require_dex_web_security_tests_passed(output: str, prefixes: tuple[str, ...]) -> None:
    """Fails when a Dex rename leaves a security test prefix selecting nothing."""
    for prefix in prefixes:
        if not re.search(rf"^--- PASS: {re.escape(prefix)}\w* \(", output, re.MULTILINE):
            raise RuntimeError(f"no Dex Web security test named {prefix}* passed")


def run(
    arguments: list[str],
    directory: Path,
    environment: dict[str, str] | None = None,
    should_print_stdout: bool = True,
) -> subprocess.CompletedProcess[str]:
    print("+ " + " ".join(arguments), flush=True)
    result = subprocess.run(arguments, cwd=directory, env=environment, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.stdout and should_print_stdout:
        print(result.stdout, end="", flush=True)
    if result.stderr:
        print(result.stderr, end="", file=sys.stderr, flush=True)
    if result.returncode:
        raise RuntimeError(f"command failed with exit code {result.returncode}: {' '.join(arguments)}")
    return result


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(f"dex compatibility gate failed: {error}", file=sys.stderr)
        raise SystemExit(1)
