#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Exercise current or published Connectors against a pinned or latest Dex release."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
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
# One fixture per named authentication shape; the credential isolation fixture covers the unnamed shape.
CONNECTION_RECORD_FIXTURES = (
    DEX_WEB_COMPATIBILITY_TESTS / "connection-records-single",
    DEX_WEB_COMPATIBILITY_TESTS / "connection-records-multiple",
)
CONNECTION_RECORD_RELEASES_ENVIRONMENT = "DEX_CONNECTOR_COMPAT_CONNECTION_RECORD_RELEASES"
CONNECTION_RECORD_OUTPUT_ENVIRONMENT = "DEX_CONNECTOR_COMPAT_CONNECTION_RECORDS_OUTPUT"
SDK_CONNECTION_RECORD_ENVIRONMENT = "DEX_CONNECTOR_COMPAT_CONNECTION_RECORDS"
SDK_CONNECTION_RECORD_TEST = "TestDexWebWrittenConnectionRecords"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("current", "released", "install-dexcli"))
    parser.add_argument("--dex-version", default=os.environ.get("DEX_CLI_VERSION"))
    parser.add_argument("--output")
    parser.add_argument("--connector-tag", default=os.environ.get("CONNECTOR_RELEASE_TAG"))
    parser.add_argument(
        "--connector-directories",
        help="comma-separated repository-relative connector directories; omitted selects every connector",
    )
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    selected_directories = parse_connector_directories(root, args.connector_directories)
    configured_version = args.dex_version or (root / ".dex-compat-version").read_text().strip()
    dex_release = resolve_dex_release(configured_version)
    with tempfile.TemporaryDirectory(prefix="dex-compat-") as temporary:
        temporary_root = Path(temporary)
        dexcli = download_dexcli(dex_release, temporary_root)
        if args.mode == "install-dexcli":
            if not args.output:
                parser.error("install-dexcli requires --output")
            output = Path(args.output).resolve()
            output.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(dexcli, output)
            output.chmod(0o755)
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
        connection_record_releases = [
            build_fixture_release(root, fixture, temporary_root / f"{fixture.name}-release") for fixture in CONNECTION_RECORD_FIXTURES
        ]
        test_dex_web(dex_release, root, artifacts, credential_isolation_release, connection_record_releases, temporary_root)
    return 0


def github_json(url: str) -> object:
    request = urllib.request.Request(
        url,
        headers={"Accept": "application/vnd.github+json", "User-Agent": "dex-connector-compatibility-gate"},
    )
    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    if token:
        request.add_header("Authorization", f"Bearer {token}")
    with urllib.request.urlopen(request, timeout=60) as response:
        return json.load(response)


def download(url: str, output: Path) -> None:
    request = urllib.request.Request(url, headers={"User-Agent": "dex-connector-compatibility-gate"})
    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    if token and "api.github.com" in url:
        request.add_header("Authorization", f"Bearer {token}")
        request.add_header("Accept", "application/octet-stream")
    try:
        with urllib.request.urlopen(request, timeout=120) as response, output.open("wb") as destination:
            shutil.copyfileobj(response, destination)
    except Exception as error:
        raise RuntimeError(f"download failed: {url}: {error}") from error


def github_releases(repository: str) -> list[dict[str, object]]:
    releases: list[dict[str, object]] = []
    for page in range(1, 101):
        batch = github_json(f"https://api.github.com/repos/{repository}/releases?per_page=100&page={page}")
        if not isinstance(batch, list):
            raise RuntimeError(f"GitHub returned an invalid release list for {repository}")
        releases.extend(batch)
        if len(batch) < 100:
            return releases
    raise RuntimeError(f"GitHub release pagination exceeded 100 pages for {repository}")


def stable_key(version: str) -> tuple[int, int, int]:
    match = STABLE_VERSION.fullmatch(version)
    if not match:
        raise ValueError(version)
    return tuple(int(value) for value in match.groups())


def resolve_dex_release(version_override: str | None) -> dict[str, object]:
    releases = github_releases(DEX_REPOSITORY)
    candidates = [
        release for release in releases
        if not release.get("draft") and not release.get("prerelease")
        and str(release.get("tag_name", "")).startswith("cli-v")
        and STABLE_VERSION.fullmatch(str(release["tag_name"])[4:])
    ]
    requested = version_override
    if requested and requested != "latest":
        requested = requested if requested.startswith("cli-") else f"cli-{requested}"
        candidates = [release for release in candidates if release["tag_name"] == requested]
    if not candidates:
        raise RuntimeError(f"no stable Dex CLI release found for {version_override or 'latest'}")
    release = max(candidates, key=lambda value: stable_key(str(value["tag_name"])[4:]))
    reference = github_json(f"https://api.github.com/repos/{DEX_REPOSITORY}/git/ref/tags/{release['tag_name']}")
    git_object = reference["object"]
    if git_object["type"] == "tag":
        git_object = github_json(git_object["url"])["object"]
    release["resolved_commit"] = git_object["sha"]
    print(f"Dex release: tag={release['tag_name']} commit={release['resolved_commit']}", flush=True)
    return release


def download_dexcli(release: dict[str, object], directory: Path) -> Path:
    version = str(release["tag_name"])[4:]
    operating_system = {"Darwin": "darwin", "Linux": "linux"}.get(platform.system())
    architecture = {"x86_64": "amd64", "AMD64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(platform.machine())
    if not operating_system or not architecture:
        raise RuntimeError(f"unsupported platform: {platform.system()} {platform.machine()}")
    archive_name = f"dexcli_{version}_{operating_system}_{architecture}.tar.gz"
    assets = {str(asset["name"]): asset for asset in release.get("assets", [])}
    if archive_name not in assets or "checksums.txt" not in assets:
        raise RuntimeError(f"Dex release is missing {archive_name} or checksums.txt")
    archive = directory / archive_name
    checksums = directory / "checksums.txt"
    download(str(assets[archive_name]["browser_download_url"]), archive)
    download(str(assets["checksums.txt"]["browser_download_url"]), checksums)
    expected = checksum_entry(checksums.read_text(), archive_name)
    actual = hashlib.sha256(archive.read_bytes()).hexdigest()
    if actual != expected:
        raise RuntimeError(f"Dex CLI checksum mismatch: expected {expected}, got {actual}")
    print(f"Dex CLI: asset={archive_name} sha256={actual}", flush=True)
    with tarfile.open(archive, "r:gz") as archive_file:
        members = [member for member in archive_file.getmembers() if Path(member.name).name == "dexcli" and member.isfile()]
        if len(members) != 1:
            raise RuntimeError("Dex CLI archive must contain exactly one dexcli binary")
        archive_file.extract(members[0], directory, filter="data")
        binary = directory / members[0].name
    binary.chmod(0o755)
    return binary


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
        connector_root = manifest.parent
        destination = output / connector_root.relative_to(root)
        destination.mkdir(parents=True)
        arguments: list[str] = []
        ui = connector_root / "ui"
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
        if path.is_file() and not any(part in {"node_modules", "dist", ".git"} for part in path.parts)
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
        proxies[manifest.parent] = create_module_proxy(manifest.parent, proxy)
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
    releases = github_releases(REPOSITORY)
    current_prefixes = {
        manifest.parent.relative_to(root).as_posix()
        for manifest in manifests(root, connector_directories)
    }
    stable = [
        release for release in releases
        if not release.get("draft") and not release.get("prerelease")
        and re.search(r"/v\d+\.\d+\.\d+$", str(release.get("tag_name", "")))
        and str(release["tag_name"]).rsplit("/", 1)[0] in current_prefixes
    ]
    if only_tag:
        selected = [release for release in stable if release["tag_name"] == only_tag]
        if not selected:
            raise RuntimeError(f"published connector release not found: {only_tag}")
        return selected
    latest: dict[str, dict[str, object]] = {}
    for release in stable:
        prefix, version = str(release["tag_name"]).rsplit("/", 1)
        if prefix not in latest or stable_key(version) > stable_key(str(latest[prefix]["tag_name"]).rsplit("/", 1)[1]):
            latest[prefix] = release
    return [latest[prefix] for prefix in sorted(latest)]


def download_released_artifacts(releases: list[dict[str, object]], output: Path) -> None:
    for release in releases:
        tag = str(release["tag_name"])
        destination = output / tag.rsplit("/", 1)[0]
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
    connection_record_releases: list[Path],
    temporary_root: Path,
) -> None:
    tag = str(dex_release["tag_name"])
    source_archive = temporary_root / "dex-source.tar.gz"
    download(f"https://github.com/{DEX_REPOSITORY}/archive/refs/tags/{tag}.tar.gz", source_archive)
    with tarfile.open(source_archive, "r:gz") as archive:
        archive.extractall(temporary_root, filter="data")
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
    connection_records = temporary_root / "connection-records"
    environment[CONNECTION_RECORD_RELEASES_ENVIRONMENT] = os.pathsep.join(str(release) for release in connection_record_releases)
    environment[CONNECTION_RECORD_OUTPUT_ENVIRONMENT] = str(connection_records)
    compatibility_tests = dex_web_compatibility_test_names(compatibility_sources)
    result = run(["go", "test", ".", "-run", "|".join((*compatibility_tests, *DEX_WEB_SECURITY_TESTS)), "-count=1", "-v"], web, environment)
    require_dex_web_tests_passed(result.stdout, compatibility_tests)
    require_dex_web_security_tests_passed(result.stdout, DEX_WEB_SECURITY_TESTS)
    test_sdk_reads_dex_web_connection_records(root, connection_records)


def test_sdk_reads_dex_web_connection_records(root: Path, connection_records: Path) -> None:
    """Loads and refreshes the files Dex Web just wrote with this checkout's SDK."""
    environment = os.environ.copy()
    environment["GOWORK"] = "off"
    environment[SDK_CONNECTION_RECORD_ENVIRONMENT] = str(connection_records)
    result = run(["go", "test", "-tags=dexcompat", "./localconfig", "-run", f"^{SDK_CONNECTION_RECORD_TEST}$", "-count=1", "-v"], root / "sdkgo", environment)
    require_dex_web_tests_passed(result.stdout, [SDK_CONNECTION_RECORD_TEST])


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


def run(arguments: list[str], directory: Path, environment: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    print("+ " + " ".join(arguments), flush=True)
    result = subprocess.run(arguments, cwd=directory, env=environment, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.stdout:
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
