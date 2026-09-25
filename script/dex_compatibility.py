#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

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
SYNTHETIC_VERSION = "v0.0.0"
STABLE_VERSION = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("current", "released", "install-dexcli"))
    parser.add_argument("--dex-version", default=os.environ.get("DEX_CLI_VERSION"))
    parser.add_argument("--output")
    parser.add_argument("--connector-tag", default=os.environ.get("CONNECTOR_RELEASE_TAG"))
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
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
            build_current_artifacts(root, artifacts)
            test_current_examples(root, examples, dexcli, temporary_root)
        else:
            releases = connector_releases(root, args.connector_tag)
            download_released_artifacts(releases, artifacts)
            test_released_examples(releases, examples, dexcli)
        test_dex_web(dex_release, root, artifacts, temporary_root)
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


def manifests(root: Path) -> list[Path]:
    discovered = sorted((root / "connectors").glob("**/connector.yaml"))
    if not discovered:
        raise RuntimeError("no connector manifests found")
    return discovered


def module_path(module_root: Path) -> str:
    first = (module_root / "go.mod").read_text().splitlines()[0]
    if not first.startswith("module "):
        raise RuntimeError(f"invalid go.mod: {module_root / 'go.mod'}")
    return first.removeprefix("module ").strip()


def build_current_artifacts(root: Path, output: Path) -> None:
    commit = run(["git", "rev-parse", "HEAD"], root).stdout.strip()
    run(["npm", "ci"], root / "sdk" / "react")
    run(["npm", "run", "build"], root / "sdk" / "react")
    for manifest in manifests(root):
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
        run(["go", "run", "./cmd/connectorctl", "release-artifact", "--manifest", str(manifest), "--module-path", module_path(connector_root), "--version", SYNTHETIC_VERSION, "--tag", f"{relative}/{SYNTHETIC_VERSION}", "--source-sha", commit, "--output", str(destination / "connector-release.json"), "--digest-output", str(destination / "connector-release.json.sha256"), *arguments], root)
        release = json.loads((destination / "connector-release.json").read_text())
        print(f"Current artifact: connector={release['connectorId']} tag={release['tag']} commit={commit} digest={checksum_entry((destination / 'connector-release.json.sha256').read_text(), 'connector-release.json')}", flush=True)


def create_module_proxy(module_root: Path, proxy: Path) -> tuple[str, str]:
    name = module_path(module_root)
    version_root = proxy / name / "@v"
    version_root.mkdir(parents=True, exist_ok=True)
    (version_root / "list").write_text(SYNTHETIC_VERSION + "\n")
    (version_root / f"{SYNTHETIC_VERSION}.info").write_text(json.dumps({"Version": SYNTHETIC_VERSION, "Time": "2026-01-01T00:00:00Z"}) + "\n")
    shutil.copy2(module_root / "go.mod", version_root / f"{SYNTHETIC_VERSION}.mod")
    with zipfile.ZipFile(version_root / f"{SYNTHETIC_VERSION}.zip", "w", zipfile.ZIP_DEFLATED) as archive:
        for path in sorted(module_root.rglob("*")):
            if not path.is_file() or any(part in {"node_modules", "dist", ".git"} for part in path.parts):
                continue
            relative = path.relative_to(module_root).as_posix()
            archive.write(path, f"{name}@{SYNTHETIC_VERSION}/{relative}")
    return name, SYNTHETIC_VERSION


def flow_examples(root: Path) -> list[Path]:
    examples = []
    for source in sorted((root / "connectors").glob("**/examples/**/flow/*.go")):
        if "GetSteps(" in source.read_text():
            examples.append(source.parent)
    if not examples:
        raise RuntimeError("no Flow examples found")
    return examples


def test_current_examples(root: Path, output: Path, dexcli: Path, temporary_root: Path) -> None:
    proxy = temporary_root / "proxy"
    proxies: dict[Path, tuple[str, str]] = {}
    for manifest in manifests(root):
        proxies[manifest.parent] = create_module_proxy(manifest.parent, proxy)
    for example in flow_examples(root):
        connector_root = next(path for path in proxies if path in example.parents)
        name, version = proxies[connector_root]
        run_visualize(example, output / example.parent.name, dexcli, name, version, proxy.as_uri())


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


def connector_releases(root: Path, only_tag: str | None) -> list[dict[str, object]]:
    releases = github_releases(REPOSITORY)
    current_prefixes = {manifest.parent.relative_to(root).as_posix() for manifest in manifests(root)}
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
            run_visualize(example, output / f"{Path(prefix).name}-{example.parent.name}", dexcli, connector_module, version)


def test_dex_web(dex_release: dict[str, object], root: Path, artifacts: Path, temporary_root: Path) -> None:
    tag = str(dex_release["tag_name"])
    source_archive = temporary_root / "dex-source.tar.gz"
    download(f"https://github.com/{DEX_REPOSITORY}/archive/refs/tags/{tag}.tar.gz", source_archive)
    with tarfile.open(source_archive, "r:gz") as archive:
        archive.extractall(temporary_root, filter="data")
    candidates = [path for path in temporary_root.iterdir() if path.is_dir() and (path / "web" / "go.mod").exists()]
    if len(candidates) != 1:
        raise RuntimeError("Dex source archive has an unexpected layout")
    web = candidates[0] / "web"
    shutil.copy2(root / "test" / "dexcompat" / "external_connector_compat_test.go.txt", web / "external_connector_compat_test.go")
    run(["npm", "ci"], web)
    run(["npm", "run", "build"], web)
    environment = os.environ.copy()
    environment["GOWORK"] = "off"
    environment["GOTOOLCHAIN"] = "auto"
    environment["DEX_CONNECTOR_COMPAT_ARTIFACT_ROOT"] = str(artifacts)
    run(["go", "test", ".", "-run", "TestExternalConnectorCompatibility|TestConnectorOAuth|TestSlackOAuth|TestConnectorConnectionsAPI|TestConnectorConnectionStore", "-count=1", "-v"], web, environment)


def run(arguments: list[str], directory: Path, environment: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    print("+ " + " ".join(arguments), flush=True)
    result = subprocess.run(arguments, cwd=directory, env=environment, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    if result.stdout:
        print(result.stdout, end="", flush=True)
    if result.returncode:
        raise RuntimeError(f"command failed with exit code {result.returncode}: {' '.join(arguments)}")
    return result


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(f"dex compatibility gate failed: {error}", file=sys.stderr)
        raise SystemExit(1)
