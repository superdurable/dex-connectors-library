# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Plan, build, package, and index releases of the Dex Connectors MCP server in connectorsmcp/."""

from __future__ import annotations

import argparse
import gzip
import hashlib
import io
import json
import os
import re
import subprocess
import sys
import tarfile
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable, Iterable

if __package__:
    from .github_release_inventory import REPOSITORY, github_json, next_page_url
else:
    sys.path.insert(0, str(Path(__file__).parent))
    from github_release_inventory import REPOSITORY, github_json, next_page_url

TAG_PREFIX = "connectorsmcp/v"
RELEASE_COMPLETE_ASSET = "connectors-mcp-release.complete"
RELEASE_LOCK_ASSET = "connectors-mcp-release.json"
CHECKSUMS_ASSET = "checksums.txt"
LOCK_SCHEMA_VERSION = "dex-connectors-mcp/release/v1"
INDEX_SCHEMA_VERSION = "dex-connectors-mcp/release-index/v1"
BINARY_NAME = "dex-connectors-mcp"
SERVER_MODULE_PATH = "github.com/superdurable/dex-connectors-library/connectorsmcp"
SDK_MODULE_PATH = "github.com/superdurable/dex-connectors-library/sdkgo"
DEX_SDK_MODULE_PATH = "github.com/superdurable/dex/sdk-go"
REGISTRY_PATH = Path("cmd/dex-connectors-mcp/zz_generated_registry.go")
# Windows waits for a LockFileEx call-state lock; see connectorsmcp/README.md.
PLATFORMS = (("darwin", "arm64"), ("darwin", "amd64"), ("linux", "amd64"), ("linux", "arm64"))
SERVER_VERSION_PATTERN = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
SERIES_PATTERN = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
CONNECTOR_VERSION_PATTERN = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
CONNECTOR_ID_PATTERN = re.compile(r"^[a-z][a-z0-9-]{1,62}$")
GO_IDENTIFIER_PATTERN = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
SHA256_PATTERN = re.compile(r"^[0-9a-f]{64}$")
# Identifiers the generated registry already imports or declares.
RESERVED_REGISTRY_IDENTIFIERS = frozenset({"main", "registry", "sdkgo", "projectconfig", "linkedConnectors"})


class ConnectorsMCPReleaseError(RuntimeError):
    """The release cannot proceed without a person's decision or a fix."""


class ReleaseDownloadError(RuntimeError):
    """A release asset could not be downloaded; the index fails instead of leaving the release out."""


@dataclass(frozen=True)
class LinkedConnector:
    """One connector module version that a server release links."""

    connector_id: str
    display_name: str
    module: str
    version: str
    package_name: str
    tools: tuple[str, ...]

    @staticmethod
    def from_json(data: object, source: str) -> LinkedConnector:
        if not isinstance(data, dict):
            raise ConnectorsMCPReleaseError(f"{source}: a connector entry must be an object")
        connector = LinkedConnector(
            connector_id=require_string(data, "connectorId", source),
            display_name=require_string(data, "displayName", source, allow_empty=True),
            module=require_string(data, "module", source),
            version=require_string(data, "version", source),
            package_name=require_string(data, "packageName", source),
            tools=tuple(require_string_list(data, "tools", source)),
        )
        if not CONNECTOR_ID_PATTERN.fullmatch(connector.connector_id):
            raise ConnectorsMCPReleaseError(f"{source}: connector ID {connector.connector_id!r} is invalid")
        if not CONNECTOR_VERSION_PATTERN.fullmatch(connector.version):
            raise ConnectorsMCPReleaseError(f"{source}: {connector.connector_id} version {connector.version!r} is not a stable release")
        if not GO_IDENTIFIER_PATTERN.fullmatch(connector.package_name):
            raise ConnectorsMCPReleaseError(f"{source}: {connector.connector_id} package name {connector.package_name!r} is invalid")
        return connector

    def to_json(self) -> dict[str, object]:
        return {"connectorId": self.connector_id, "displayName": self.display_name, "module": self.module,
                "version": self.version, "packageName": self.package_name, "tools": list(self.tools)}


@dataclass(frozen=True)
class PreviousRelease:
    """The latest complete server release, read from its connectors-mcp-release.json."""

    version: str
    tag: str
    server_source_tree: str
    connectors: dict[str, LinkedConnector]


@dataclass(frozen=True)
class ReleaseState:
    """The latest complete server release and a newer tag whose release an earlier run left incomplete."""

    previous_tag: str | None
    repair_tag: str | None


@dataclass
class ReleasePlan:
    """What this run releases, before the hold rule."""

    is_release_required: bool
    reason: str
    version: str | None
    tag: str | None
    source_commit: str
    server_source_tree: str
    previous: PreviousRelease | None
    desired: list[LinkedConnector]


@dataclass
class HeldConnector:
    """A previously included connector kept at its previous version because its newer release failed."""

    connector_id: str
    held_version: str
    rejected_version: str


@dataclass
class ReleaseSet:
    """The connector set that passed the build, with the changes the hold rule set aside."""

    connectors: list[LinkedConnector]
    held: list[HeldConnector] = field(default_factory=list)
    excluded: list[LinkedConnector] = field(default_factory=list)


def require_string(data: dict, key: str, source: str, *, allow_empty: bool = False) -> str:
    value = data.get(key)
    if not isinstance(value, str) or (not allow_empty and not value):
        raise ConnectorsMCPReleaseError(f"{source}: {key} must be a non-empty string")
    return value


def require_string_list(data: dict, key: str, source: str) -> list[str]:
    value = data.get(key)
    if not isinstance(value, list) or not all(isinstance(item, str) and item for item in value):
        raise ConnectorsMCPReleaseError(f"{source}: {key} must be a list of non-empty strings")
    return value


def server_version_key(version: str) -> tuple[int, int, int]:
    match = SERVER_VERSION_PATTERN.fullmatch(version)
    if match is None:
        raise ConnectorsMCPReleaseError(f"server version {version!r} is not MAJOR.MINOR.PATCH")
    return int(match.group(1)), int(match.group(2)), int(match.group(3))


def connector_version_key(version: str) -> tuple[int, int, int]:
    match = CONNECTOR_VERSION_PATTERN.fullmatch(version)
    if match is None:
        raise ConnectorsMCPReleaseError(f"connector version {version!r} is not vMAJOR.MINOR.PATCH")
    return int(match.group(1)), int(match.group(2)), int(match.group(3))


def server_version_of_tag(tag: str) -> str | None:
    if not tag.startswith(TAG_PREFIX):
        return None
    version = tag[len(TAG_PREFIX):]
    return version if SERVER_VERSION_PATTERN.fullmatch(version) else None


def next_server_version(series: str, previous_version: str | None) -> str:
    """Returns the next patch in series, or the series' first release when the series is new."""
    match = SERIES_PATTERN.fullmatch(series)
    if match is None:
        raise ConnectorsMCPReleaseError(f"release series {series!r} must be vMAJOR.MINOR")
    series_key = (int(match.group(1)), int(match.group(2)))
    if previous_version is None:
        return f"{series_key[0]}.{series_key[1]}.0"
    major, minor, patch = server_version_key(previous_version)
    if (major, minor) == series_key:
        return f"{major}.{minor}.{patch + 1}"
    if (major, minor) > series_key:
        raise ConnectorsMCPReleaseError(f"release series {series} is behind the latest release {previous_version}")
    return f"{series_key[0]}.{series_key[1]}.0"


def release_state(inventory: dict[str, object], tags: Iterable[str]) -> ReleaseState:
    """Finds the latest complete release and the newest tag above it whose release is incomplete."""
    if inventory.get("_inventory_unavailable"):
        raise ConnectorsMCPReleaseError("the release inventory is unavailable")
    complete = [tag for tag, assets in inventory.items()
                if server_version_of_tag(tag) and isinstance(assets, list) and RELEASE_COMPLETE_ASSET in assets]
    previous_tag = max(complete, key=lambda tag: server_version_key(server_version_of_tag(tag)), default=None)
    previous_key = server_version_key(server_version_of_tag(previous_tag)) if previous_tag else None
    incomplete = [tag for tag in tags if server_version_of_tag(tag) and tag not in complete and
                  (previous_key is None or server_version_key(server_version_of_tag(tag)) > previous_key)]
    repair_tag = max(incomplete, key=lambda tag: server_version_key(server_version_of_tag(tag)), default=None)
    return ReleaseState(previous_tag, repair_tag)


def read_previous_release(lock: dict[str, object], expected_tag: str) -> PreviousRelease:
    source = f"{RELEASE_LOCK_ASSET} of {expected_tag}"
    validate_release_lock(lock, expected_tag)
    connectors = [LinkedConnector.from_json(entry, source) for entry in lock["connectors"]]
    return PreviousRelease(version=lock["version"], tag=expected_tag, server_source_tree=lock["serverSourceTree"],
                           connectors={connector.connector_id: connector for connector in connectors})


def validate_release_lock(lock: object, expected_tag: str) -> None:
    """Rejects a connectors-mcp-release.json that does not describe expected_tag completely."""
    source = f"{RELEASE_LOCK_ASSET} of {expected_tag}"
    if not isinstance(lock, dict) or lock.get("schemaVersion") != LOCK_SCHEMA_VERSION:
        raise ConnectorsMCPReleaseError(f"{source}: schemaVersion must be {LOCK_SCHEMA_VERSION}")
    version = require_string(lock, "version", source)
    if lock.get("tag") != expected_tag or TAG_PREFIX + version != expected_tag:
        raise ConnectorsMCPReleaseError(f"{source}: version and tag do not name {expected_tag}")
    server_version_key(version)
    launch_contract = lock.get("launchContract")
    if type(launch_contract) is not int or launch_contract < 1:
        raise ConnectorsMCPReleaseError(f"{source}: launchContract must be a positive integer")
    require_string(lock, "serverSourceTree", source)
    connectors = lock.get("connectors")
    if not isinstance(connectors, list):
        raise ConnectorsMCPReleaseError(f"{source}: connectors must be a list")
    connector_ids = [LinkedConnector.from_json(entry, source).connector_id for entry in connectors]
    if len(set(connector_ids)) != len(connector_ids):
        raise ConnectorsMCPReleaseError(f"{source}: a connector is listed twice")
    assets = lock.get("assets")
    if not isinstance(assets, dict) or set(assets) != {platform_key(*platform) for platform in PLATFORMS}:
        raise ConnectorsMCPReleaseError(f"{source}: assets must name exactly {', '.join(platform_key(*p) for p in PLATFORMS)}")
    for platform in PLATFORMS:
        asset = assets[platform_key(*platform)]
        if not isinstance(asset, dict) or asset.get("name") != archive_name(version, *platform) \
                or not isinstance(asset.get("sha256"), str) or not SHA256_PATTERN.fullmatch(asset["sha256"]):
            raise ConnectorsMCPReleaseError(f"{source}: asset {platform_key(*platform)} needs its archive name and SHA-256")


def plan_release(inventory: dict[str, object], previous_lock: dict[str, object] | None, previous_tag: str | None) -> ReleasePlan:
    """Plans a release when the connector set or server source changed; never drops or downgrades silently."""
    if (previous_lock is None) != (previous_tag is None):
        raise ConnectorsMCPReleaseError("the previous release tag and its lock must be given together")
    series = require_string(inventory, "series", "inventory")
    withdrawn = set(require_string_list(inventory, "withdrawnConnectors", "inventory"))
    source_commit = require_string(inventory, "sourceCommit", "inventory")
    server_source_tree = require_string(inventory, "serverSourceTree", "inventory")
    entries = inventory.get("connectors")
    if not isinstance(entries, list):
        raise ConnectorsMCPReleaseError("inventory: connectors must be a list")
    candidates = [LinkedConnector.from_json(entry, "inventory") for entry in entries]
    if len({candidate.connector_id for candidate in candidates}) != len(candidates):
        raise ConnectorsMCPReleaseError("inventory: a connector is listed twice")
    desired = {candidate.connector_id: candidate for candidate in candidates if candidate.connector_id not in withdrawn}
    previous = read_previous_release(previous_lock, previous_tag) if previous_lock is not None else None
    if previous is not None:
        for connector_id, included in sorted(previous.connectors.items()):
            if connector_id in withdrawn:
                continue
            if connector_id not in desired:
                raise ConnectorsMCPReleaseError(
                    f"{connector_id} {included.version} is in {previous.tag} but its latest complete Go release no longer "
                    "offers agent tools; add it to withdrawnConnectors in connectorsmcp/release.yaml to remove it deliberately")
            if connector_version_key(desired[connector_id].version) < connector_version_key(included.version):
                raise ConnectorsMCPReleaseError(
                    f"{connector_id} would move from {included.version} in {previous.tag} to the older "
                    f"{desired[connector_id].version}; a release never downgrades a connector")
    ordered = [desired[connector_id] for connector_id in sorted(desired)]
    plan = ReleasePlan(is_release_required=False, reason="", version=None, tag=None, source_commit=source_commit,
                       server_source_tree=server_source_tree, previous=previous, desired=ordered)
    # Withdrawing the last linked connector still releases, so the index stops advertising it.
    if not ordered and (previous is None or not previous.connectors):
        plan.reason = "no connector release offers agent tools"
        return plan
    if previous is not None and connector_versions(ordered) == connector_versions(previous.connectors.values()) \
            and previous.server_source_tree == server_source_tree:
        plan.reason = f"the connector set and the server source match {previous.tag}"
        return plan
    plan.is_release_required = True
    plan.version = next_server_version(series, previous.version if previous else None)
    plan.tag = TAG_PREFIX + plan.version
    plan.reason = "the connector set or the server source changed" if previous else "first release"
    return plan


def connector_versions(connectors: Iterable[LinkedConnector]) -> dict[str, str]:
    return {connector.connector_id: connector.version for connector in connectors}


def select_release_set(plan: ReleasePlan, evaluate: Callable[[list[LinkedConnector]], bool]) -> ReleaseSet:
    """Applies the hold rule; docs/versioning-and-releases.md#connectors-mcp-server-releases explains it."""
    desired = {connector.connector_id: connector for connector in plan.desired}
    if evaluate(sorted_connectors(desired.values())):
        return ReleaseSet(connectors=sorted_connectors(desired.values()))
    previous = plan.previous.connectors if plan.previous else {}
    accepted: dict[str, LinkedConnector] = {}
    changes: list[LinkedConnector] = []
    for connector_id in sorted(desired):
        candidate = desired[connector_id]
        included = previous.get(connector_id)
        if included is not None and included.version == candidate.version:
            accepted[connector_id] = candidate
            continue
        if included is not None:
            accepted[connector_id] = included
        changes.append(candidate)
    if not evaluate(sorted_connectors(accepted.values())):
        raise ConnectorsMCPReleaseError(
            "the previously included connector versions do not build or pass tests with this server source; "
            "a release never drops or downgrades them, so fix the server or the connectors")
    result = ReleaseSet(connectors=[])
    for candidate in changes:
        trial = {**accepted, candidate.connector_id: candidate}
        if evaluate(sorted_connectors(trial.values())):
            accepted = trial
        elif candidate.connector_id in previous:
            result.held.append(HeldConnector(candidate.connector_id, previous[candidate.connector_id].version, candidate.version))
        else:
            result.excluded.append(candidate)
    result.connectors = sorted_connectors(accepted.values())
    return result


def sorted_connectors(connectors: Iterable[LinkedConnector]) -> list[LinkedConnector]:
    return sorted(connectors, key=lambda connector: connector.connector_id)


def is_release_required_after_hold(plan: ReleasePlan, release_set: ReleaseSet) -> bool:
    if plan.previous is None:
        return bool(release_set.connectors)
    return connector_versions(release_set.connectors) != connector_versions(plan.previous.connectors.values()) \
        or plan.previous.server_source_tree != plan.server_source_tree


def registry_import_names(connectors: list[LinkedConnector]) -> dict[str, str]:
    """Names each connector import by its package, adding a number when two packages share a name."""
    used = set(RESERVED_REGISTRY_IDENTIFIERS)
    names: dict[str, str] = {}
    for connector in sorted_connectors(connectors):
        name = connector.package_name
        suffix = 2
        while name in used:
            name = f"{connector.package_name}{suffix}"
            suffix += 1
        used.add(name)
        names[connector.connector_id] = name
    return names


def generate_registry_source(connectors: list[LinkedConnector], *, format_source: Callable[[str], str]) -> str:
    """Returns the gofmt-formatted zz_generated_registry.go that links connectors."""
    header = ("// Code generated by the Connectors MCP release workflow; DO NOT EDIT.\n"
              "// Copyright (c) 2026 Super Durable\n// SPDX-License-Identifier: MIT\n\npackage main\n\n")
    if not connectors:
        return format_source(header + 'import "github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"\n\n'
                             "// linkedConnectors is empty on main; a release commit replaces this file and go.mod.\n"
                             "func linkedConnectors() []registry.LinkedConnector {\n\treturn nil\n}\n")
    names = registry_import_names(connectors)
    imports = sorted([(connector.module, names[connector.connector_id]) for connector in connectors] + [
        (SERVER_MODULE_PATH + "/internal/registry", ""), (SDK_MODULE_PATH, ""), (SDK_MODULE_PATH + "/projectconfig", "")])
    lines = [header, "import (\n"]
    # A connector module path ends in /go, so every connector import names its package.
    lines += [f"\t{name} {go_string(path)}\n" if name else f"\t{go_string(path)}\n" for path, name in imports]
    lines.append(")\n\n// linkedConnectors lists the connector releases this server release links, sorted by connector ID.\n")
    lines.append("func linkedConnectors() []registry.LinkedConnector {\n\treturn []registry.LinkedConnector{\n")
    for connector in sorted_connectors(connectors):
        name = names[connector.connector_id]
        lines.append(
            "\t\t{\n"
            f"\t\t\tConnectorID: {go_string(connector.connector_id)},\n"
            f"\t\t\tDisplayName: {go_string(connector.display_name)},\n"
            f"\t\t\tModulePath: {go_string(connector.module)},\n"
            f"\t\t\tModuleVersion: {go_string(connector.version)},\n"
            f"\t\t\tExposedAgentTools: {name}.ExposedAgentTools(),\n"
            "\t\t\tOpenAgentTools: func(project *projectconfig.LoadedProject, connectionName string) ([]sdkgo.AgentTool, error) {\n"
            f"\t\t\t\tconnection, err := {name}.NewProjectConnection(project, connectionName)\n"
            "\t\t\t\tif err != nil {\n\t\t\t\t\treturn nil, err\n\t\t\t\t}\n"
            "\t\t\t\treturn connection.AgentTools()\n"
            "\t\t\t},\n"
            "\t\t},\n")
    lines.append("\t}\n}\n")
    return format_source("".join(lines))


def go_string(value: str) -> str:
    # JSON string escapes are a subset of Go's interpreted string literal escapes.
    return json.dumps(value, ensure_ascii=True)


REGISTRY_ENTRY_PATTERN = re.compile(
    r'ConnectorID:\s*("(?:[^"\\]|\\.)*"),\s*DisplayName:\s*("(?:[^"\\]|\\.)*"),\s*'
    r'ModulePath:\s*("(?:[^"\\]|\\.)*"),\s*ModuleVersion:\s*("(?:[^"\\]|\\.)*"),\s*'
    r'ExposedAgentTools:\s*([A-Za-z_][A-Za-z0-9_]*)\.ExposedAgentTools\(\)')


def read_registry_entries(source: str) -> list[dict[str, str]]:
    """Reads connectorId, displayName, module, and version back from a generated registry."""
    return [{"connectorId": json.loads(match.group(1)), "displayName": json.loads(match.group(2)),
             "module": json.loads(match.group(3)), "version": json.loads(match.group(4))}
            for match in REGISTRY_ENTRY_PATTERN.finditer(source)]


def verify_version_report(report: object, connectors: list[dict[str, object]], expected_version: str | None) -> int:
    """Checks version --output json against the linked connectors and returns the launch contract."""
    if not isinstance(report, dict):
        raise ConnectorsMCPReleaseError("version --output json did not print an object")
    launch_contract = report.get("launchContract")
    if type(launch_contract) is not int or launch_contract < 1:
        raise ConnectorsMCPReleaseError("the binary reports no positive integer launch contract")
    if expected_version is not None and report.get("version") != expected_version:
        raise ConnectorsMCPReleaseError(f"the binary reports version {report.get('version')!r}, not {expected_version}")
    linked = report.get("connectors")
    if not isinstance(linked, list) or not all(isinstance(entry, dict) for entry in linked):
        raise ConnectorsMCPReleaseError("the binary reports no connector list")
    keys = ("connectorId", "module", "version")
    expected = [{key: connector[key] for key in keys} for connector in sorted(connectors, key=lambda entry: str(entry["connectorId"]))]
    actual = [{key: entry.get(key) for key in keys} for entry in linked]
    if actual != expected:
        raise ConnectorsMCPReleaseError(f"the binary links {json.dumps(actual)}, expected {json.dumps(expected)}")
    expected_tool_counts = {str(connector["connectorId"]): len(connector["tools"]) for connector in connectors}
    for entry in linked:
        tools = entry.get("tools")
        # Each release's generated code owns its tool names; only their count and prefix are checked.
        if not isinstance(tools, list) or len(tools) != expected_tool_counts[entry["connectorId"]] or not all(
                isinstance(tool, str) and tool.startswith(entry["connectorId"] + "_") for tool in tools):
            raise ConnectorsMCPReleaseError(f"{entry['connectorId']} links tools {tools!r}, expected "
                                            f"{expected_tool_counts[entry['connectorId']]} named {entry['connectorId']}_*")
    return launch_contract


def verify_resolved_versions(build: ServerBuild, connectors: list[dict[str, object]]) -> None:
    """Rejects a build where minimal version selection moved a connector off its registry version."""
    if not connectors:
        return
    listed = build.go("list", "-m", "-f", "{{.Path}} {{.Version}}", *[str(connector["module"]) for connector in connectors]).split()
    resolved = dict(zip(listed[0::2], listed[1::2]))
    for connector in connectors:
        if resolved.get(str(connector["module"])) != connector["version"]:
            raise ConnectorsMCPReleaseError(
                f"{connector['module']} resolves to {resolved.get(str(connector['module']))}, not {connector['version']}; "
                "another linked module requires a newer release")


class BuildStepError(RuntimeError):
    """A go command or a smoke check failed for one candidate connector set."""


class ServerBuild:
    """Runs go commands for the server module with GOWORK=off; tests replace run."""

    def __init__(self, module_directory: Path, work_directory: Path, *,
                 run: Callable[..., subprocess.CompletedProcess] = subprocess.run) -> None:
        self.module_directory = module_directory
        self.work_directory = work_directory
        self.run = run

    def go(self, *arguments: str, environment: dict[str, str] | None = None) -> str:
        command = ["go", *arguments]
        result = self.run(command, cwd=self.module_directory, capture_output=True, text=True,
                          env={**os.environ, "GOWORK": "off", **(environment or {})})
        if result.returncode != 0:
            raise BuildStepError(f"{' '.join(command)} failed:\n{result.stdout}{result.stderr}")
        return result.stdout

    def build_binary(self, output: Path, version: str | None, goos: str | None = None, goarch: str | None = None) -> Path:
        flags = "-s -w" + (f" -X main.version={version}" if version else "")
        environment = {"CGO_ENABLED": "0"}
        if goos and goarch:
            environment |= {"GOOS": goos, "GOARCH": goarch}
        self.go("build", "-trimpath", "-ldflags", flags, "-o", str(output), "./cmd/dex-connectors-mcp", environment=environment)
        return output

    def version_report(self, binary: Path) -> object:
        result = self.run([str(binary), "version", "--output", "json"], capture_output=True, text=True)
        if result.returncode != 0:
            raise BuildStepError(f"{binary} version failed:\n{result.stdout}{result.stderr}")
        try:
            return json.loads(result.stdout)
        except json.JSONDecodeError as error:
            raise BuildStepError(f"{binary} version printed invalid JSON: {error}") from error


class ReleaseSetEvaluator:
    """Generates, vets, builds, smoke-checks, and tests the server for one connector set."""

    def __init__(self, build: ServerBuild, *, format_source: Callable[[str], str], log: Callable[[str], None] = print) -> None:
        self.build = build
        self.format_source = format_source
        self.log = log
        self.tracked_paths = [build.module_directory / "go.mod", build.module_directory / "go.sum",
                              build.module_directory / REGISTRY_PATH]
        self.original_files = {path: path.read_bytes() if path.exists() else None for path in self.tracked_paths}
        self.passing_files: dict[tuple[tuple[str, str], ...], dict[Path, bytes | None]] = {}

    def __call__(self, connectors: list[LinkedConnector]) -> bool:
        key = tuple(sorted(connector_versions(connectors).items()))
        self.log(f"::group::Build with {', '.join(f'{connector_id}@{version}' for connector_id, version in key) or 'no connector'}")
        try:
            self.write_files(self.original_files)
            (self.build.module_directory / REGISTRY_PATH).write_text(
                generate_registry_source(connectors, format_source=self.format_source), encoding="utf-8")
            if connectors:
                self.build.go("mod", "edit", *[f"-require={connector.module}@{connector.version}" for connector in connectors])
            self.build.go("mod", "tidy")
            verify_resolved_versions(self.build, [connector.to_json() for connector in connectors])
            self.build.go("vet", "./...")
            binary = self.build.build_binary(self.build.work_directory / "evaluate" / BINARY_NAME, None)
            verify_version_report(self.build.version_report(binary), [connector.to_json() for connector in connectors], None)
            self.build.go("test", "-race", "-count=1", "./...")
        except (BuildStepError, ConnectorsMCPReleaseError) as error:
            self.log(str(error))
            self.log("::endgroup::")
            return False
        self.passing_files[key] = {path: path.read_bytes() if path.exists() else None for path in self.tracked_paths}
        self.log("::endgroup::")
        return True

    def apply(self, connectors: list[LinkedConnector]) -> None:
        """Restores the files of a set that passed, so the release commit holds exactly what was tested."""
        self.write_files(self.passing_files[tuple(sorted(connector_versions(connectors).items()))])

    def write_files(self, files: dict[Path, bytes | None]) -> None:
        for path, contents in files.items():
            if contents is None:
                path.unlink(missing_ok=True)
            else:
                path.write_bytes(contents)


def gofmt_source(source: str) -> str:
    result = subprocess.run(["gofmt"], input=source, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise ConnectorsMCPReleaseError(f"gofmt rejected the generated registry:\n{result.stderr}")
    return result.stdout


def platform_key(goos: str, goarch: str) -> str:
    return f"{goos}-{goarch}"


def archive_name(version: str, goos: str, goarch: str) -> str:
    return f"{BINARY_NAME}_{version}_{goos}_{goarch}.tar.gz"


def write_release_archive(path: Path, binary: Path, license_path: Path, readme_path: Path, modified_at: int) -> None:
    """Writes a reproducible tar.gz whose top level holds the binary (0755), LICENSE, and README.md."""
    entries = ((BINARY_NAME, binary, 0o755), ("LICENSE", license_path, 0o644), ("README.md", readme_path, 0o644))
    with path.open("wb") as raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed, \
            tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
        for name, source, mode in entries:
            contents = source.read_bytes()
            info = tarfile.TarInfo(name)
            info.size = len(contents)
            info.mode = mode
            info.mtime = modified_at
            info.type = tarfile.REGTYPE
            info.uid = info.gid = 0
            info.uname = info.gname = ""
            archive.addfile(info, io.BytesIO(contents))


def sha256_file(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git_output(repository: Path, *arguments: str) -> str:
    result = subprocess.run(["git", *arguments], cwd=repository, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise ConnectorsMCPReleaseError(f"git {' '.join(arguments)} failed: {result.stderr.strip()}")
    return result.stdout.strip()


def package_release(*, repository: Path, module_directory: Path, version: str, tag: str, output_directory: Path,
                    work_directory: Path, build: ServerBuild, held: list[HeldConnector] | None = None,
                    excluded: list[LinkedConnector] | None = None) -> dict[str, object]:
    """Builds the archives, lock, checksums, and notes of the release commit checked out at HEAD."""
    if TAG_PREFIX + version != tag:
        raise ConnectorsMCPReleaseError(f"version {version} does not match tag {tag}")
    release_commit = git_output(repository, "rev-parse", "HEAD^{commit}")
    if git_output(repository, "rev-parse", f"{tag}^{{commit}}") != release_commit:
        raise ConnectorsMCPReleaseError(f"{tag} does not name the checked-out release commit")
    main_commit = git_output(repository, "rev-parse", "HEAD^1")
    server_source_tree = git_output(repository, "rev-parse", "HEAD^1:connectorsmcp")
    modified_at = int(git_output(repository, "show", "-s", "--format=%ct", "HEAD"))
    entries = read_registry_entries((module_directory / REGISTRY_PATH).read_text(encoding="utf-8"))
    host_binary = build.build_binary(work_directory / "host" / BINARY_NAME, version)
    report = build.version_report(host_binary)
    tools_by_connector = {entry.get("connectorId"): entry.get("tools") for entry in report.get("connectors", [])
                          if isinstance(entry, dict)} if isinstance(report, dict) else {}
    connectors = []
    for entry in entries:
        package_name = build.go("list", "-f", "{{.Name}}", entry["module"]).strip()
        connectors.append(LinkedConnector.from_json({**entry, "packageName": package_name,
                                                     "tools": tools_by_connector.get(entry["connectorId"])}, tag))
    launch_contract = verify_version_report(report, [connector.to_json() for connector in connectors], version)
    verify_resolved_versions(build, [connector.to_json() for connector in connectors])
    module_versions = build.go("list", "-m", "-f", "{{.Path}} {{.Version}}", SDK_MODULE_PATH, DEX_SDK_MODULE_PATH).split()
    resolved = dict(zip(module_versions[0::2], module_versions[1::2]))
    output_directory.mkdir(parents=True, exist_ok=True)
    assets: dict[str, dict[str, str]] = {}
    for goos, goarch in PLATFORMS:
        binary = build.build_binary(work_directory / platform_key(goos, goarch) / BINARY_NAME, version, goos, goarch)
        name = archive_name(version, goos, goarch)
        write_release_archive(output_directory / name, binary, repository / "LICENSE", module_directory / "README.md", modified_at)
        assets[platform_key(goos, goarch)] = {"name": name, "sha256": sha256_file(output_directory / name)}
    lock = {
        "schemaVersion": LOCK_SCHEMA_VERSION, "version": version, "tag": tag, "launchContract": launch_contract,
        "sourceCommit": release_commit, "mainCommit": main_commit, "serverSourceTree": server_source_tree,
        "goVersion": build.go("env", "GOVERSION").strip(), "sdkgoVersion": resolved.get(SDK_MODULE_PATH, ""),
        "dexSdkGoVersion": resolved.get(DEX_SDK_MODULE_PATH, ""),
        "connectors": [connector.to_json() for connector in connectors], "assets": assets,
    }
    validate_release_lock(lock, tag)
    (output_directory / RELEASE_LOCK_ASSET).write_text(json.dumps(lock, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    checksummed = sorted([asset["name"] for asset in assets.values()] + [RELEASE_LOCK_ASSET])
    (output_directory / CHECKSUMS_ASSET).write_text(
        "".join(f"{sha256_file(output_directory / name)}  {name}\n" for name in checksummed), encoding="utf-8")
    (work_directory / "release-notes.md").write_text(release_notes(lock, held or [], excluded or []), encoding="utf-8")
    return lock


def release_notes(lock: dict[str, object], held: list[HeldConnector], excluded: list[LinkedConnector]) -> str:
    lines = [f"Dex Connectors MCP server {lock['version']} (launch contract {lock['launchContract']}) links these "
             "connector releases:", "", "| Connector | Go module | Tools |", "|---|---|---|"]
    if not lock["connectors"]:
        lines = [f"Dex Connectors MCP server {lock['version']} (launch contract {lock['launchContract']}) links no "
                 "connector release; every linked connector was withdrawn."]
    for connector in lock["connectors"]:
        tools = ", ".join(f"`{tool}`" for tool in connector["tools"])
        lines.append(f"| {connector['connectorId']} | `{connector['module']}@{connector['version']}` | {tools} |")
    lines += ["", f"Built from main commit {lock['mainCommit']} with {lock['goVersion']}, "
                  f"sdkgo {lock['sdkgoVersion']}, and Dex Go SDK {lock['dexSdkGoVersion']}."]
    if held:
        lines += ["", "Held at the previously released version because the newer release did not build or pass tests:", ""]
        lines += [f"- {entry.connector_id}: {entry.held_version} (not {entry.rejected_version})" for entry in held]
    if excluded:
        lines += ["", "Not included because the release did not build or pass tests:", ""]
        lines += [f"- {connector.connector_id} {connector.version}" for connector in excluded]
    lines += ["", f"Verify an archive with `{CHECKSUMS_ASSET}`, or install the same build with "
                  f"`go install {SERVER_MODULE_PATH}/cmd/{BINARY_NAME}@v{lock['version']}`."]
    return "\n".join(lines) + "\n"


def build_release_index(releases: list[dict[str, object]], fetch_bytes: Callable[[str], bytes],
                        warn: Callable[[str], None] = lambda message: print(f"::warning::{message}", flush=True)) -> dict[str, object]:
    """Builds connectors-mcp/index.json from GitHub releases that carry connectors-mcp-release.complete.

    An older release that cannot be verified is left out with a warning; the newest one fails the index.
    """
    complete = []
    for release in releases:
        tag = release.get("tag_name")
        version = server_version_of_tag(tag) if isinstance(tag, str) else None
        if version is None or release.get("draft") or release.get("prerelease"):
            continue
        assets = {asset.get("name"): asset for asset in release.get("assets", []) if isinstance(asset, dict)}
        if RELEASE_COMPLETE_ASSET in assets:
            complete.append((server_version_key(version), release, assets))
    complete.sort(key=lambda item: item[0], reverse=True)
    entries = []
    for position, (_, release, assets) in enumerate(complete):
        tag = release["tag_name"]
        try:
            entries.append(release_index_entry(release, assets, fetch_bytes))
        except (ConnectorsMCPReleaseError, ReleaseDownloadError) as error:
            if position == 0:
                raise ConnectorsMCPReleaseError(f"the newest Connectors MCP release {tag} cannot be indexed: {error}") from error
            warn(f"Connectors MCP release {tag} is left out of the index: {error}")
    return {"schemaVersion": INDEX_SCHEMA_VERSION, "releases": entries}


def release_index_entry(release: dict[str, object], assets: dict[str, dict],
                        fetch_bytes: Callable[[str], bytes]) -> dict[str, object]:
    """Returns one index entry after checking each archive against GitHub's digest or its downloaded bytes."""
    tag = str(release["tag_name"])
    if RELEASE_LOCK_ASSET not in assets:
        raise ConnectorsMCPReleaseError(f"{tag} has no {RELEASE_LOCK_ASSET}")
    try:
        lock = json.loads(fetch_bytes(str(assets[RELEASE_LOCK_ASSET]["browser_download_url"])))
    except json.JSONDecodeError as error:
        raise ConnectorsMCPReleaseError(f"{tag} {RELEASE_LOCK_ASSET} is not JSON: {error}") from error
    validate_release_lock(lock, tag)
    published_at = release.get("published_at")
    if not isinstance(published_at, str) or not published_at:
        raise ConnectorsMCPReleaseError(f"{tag} has no publication time")
    index_assets = {}
    for goos, goarch in PLATFORMS:
        locked = lock["assets"][platform_key(goos, goarch)]
        asset = assets.get(locked["name"])
        if asset is None or not isinstance(asset.get("browser_download_url"), str):
            raise ConnectorsMCPReleaseError(f"{tag} has no {locked['name']} asset")
        digest = asset.get("digest")
        if isinstance(digest, str) and digest.startswith("sha256:"):
            actual = digest.removeprefix("sha256:")
        else:
            actual = hashlib.sha256(fetch_bytes(asset["browser_download_url"])).hexdigest()
        if actual != locked["sha256"]:
            raise ConnectorsMCPReleaseError(f"{tag} {locked['name']} has SHA-256 {actual}, not the locked {locked['sha256']}")
        index_assets[platform_key(goos, goarch)] = {"url": asset["browser_download_url"], "sha256": locked["sha256"]}
    return {
        "version": lock["version"], "launchContract": lock["launchContract"], "tag": tag, "publishedAt": published_at,
        "assets": index_assets,
        "connectors": [{"connectorId": connector["connectorId"], "module": connector["module"],
                        "version": connector["version"], "tools": connector["tools"]} for connector in lock["connectors"]],
    }


def list_github_releases(*, fetch=github_json) -> list[dict[str, object]]:
    url = f"https://api.github.com/repos/{REPOSITORY}/releases?per_page=100"
    releases: list[dict[str, object]] = []
    while url:
        page, links = fetch(url)
        if not isinstance(page, list):
            raise ConnectorsMCPReleaseError("GitHub returned an invalid release page")
        releases.extend(release for release in page if isinstance(release, dict))
        url = next_page_url(links)
    return releases


def download_bytes(url: str, *, attempts: int = 3, sleep=time.sleep) -> bytes:
    request = urllib.request.Request(url, headers={"User-Agent": "dex-connectors-mcp-release-index"})
    for attempt in range(1, attempts + 1):
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                return response.read()
        except (urllib.error.URLError, TimeoutError) as error:
            if attempt == attempts:
                raise ReleaseDownloadError(f"download {url} failed: {error}") from error
            sleep(2 ** attempt)
    raise AssertionError("unreachable")


def write_github_output(path: Path | None, values: dict[str, str]) -> None:
    if path is None:
        return
    with path.open("a", encoding="utf-8") as output:
        output.write("".join(f"{key}={value}\n" for key, value in values.items()))


def read_json(path: Path) -> object:
    return json.loads(path.read_text(encoding="utf-8"))


def plan_to_json(plan: ReleasePlan) -> dict[str, object]:
    previous = None
    if plan.previous is not None:
        previous = {"version": plan.previous.version, "tag": plan.previous.tag,
                    "serverSourceTree": plan.previous.server_source_tree,
                    "connectors": [connector.to_json() for connector in sorted_connectors(plan.previous.connectors.values())]}
    return {"isReleaseRequired": plan.is_release_required, "reason": plan.reason, "version": plan.version, "tag": plan.tag,
            "sourceCommit": plan.source_commit, "serverSourceTree": plan.server_source_tree, "previous": previous,
            "desired": [connector.to_json() for connector in plan.desired]}


def plan_from_json(data: dict[str, object]) -> ReleasePlan:
    previous_data = data.get("previous")
    previous = None
    if isinstance(previous_data, dict):
        connectors = [LinkedConnector.from_json(entry, "plan") for entry in previous_data["connectors"]]
        previous = PreviousRelease(version=previous_data["version"], tag=previous_data["tag"],
                                   server_source_tree=previous_data["serverSourceTree"],
                                   connectors={connector.connector_id: connector for connector in connectors})
    return ReleasePlan(is_release_required=bool(data["isReleaseRequired"]), reason=str(data["reason"]),
                       version=data.get("version"), tag=data.get("tag"), source_commit=str(data["sourceCommit"]),
                       server_source_tree=str(data["serverSourceTree"]), previous=previous,
                       desired=[LinkedConnector.from_json(entry, "plan") for entry in data["desired"]])


def release_set_to_json(release_set: ReleaseSet, is_release_required: bool) -> dict[str, object]:
    return {"isReleaseRequired": is_release_required,
            "connectors": [connector.to_json() for connector in release_set.connectors],
            "held": [{"connectorId": entry.connector_id, "heldVersion": entry.held_version,
                      "rejectedVersion": entry.rejected_version} for entry in release_set.held],
            "excluded": [connector.to_json() for connector in release_set.excluded]}


def command_state(args: argparse.Namespace) -> None:
    inventory = read_json(args.release_inventory)
    if not isinstance(inventory, dict):
        raise ConnectorsMCPReleaseError("the release inventory must be an object")
    tags = git_output(args.repository, "tag", "--list", TAG_PREFIX + "*").split()
    state = release_state(inventory, tags)
    print(f"Connectors MCP: previous complete release {state.previous_tag or 'none'}; "
          f"incomplete release to repair {state.repair_tag or 'none'}", flush=True)
    write_github_output(args.github_output, {
        "previous_tag": state.previous_tag or "", "repair_tag": state.repair_tag or "",
        "repair_version": server_version_of_tag(state.repair_tag) if state.repair_tag else ""})


def command_plan(args: argparse.Namespace) -> None:
    inventory = read_json(args.inventory)
    previous_lock = read_json(args.previous_lock) if args.previous_lock else None
    plan = plan_release(inventory, previous_lock, args.previous_tag or None)
    args.output.write_text(json.dumps(plan_to_json(plan), indent=2) + "\n", encoding="utf-8")
    print(f"Connectors MCP: {'release ' + str(plan.tag) if plan.is_release_required else 'no release'}: {plan.reason}", flush=True)
    write_github_output(args.github_output, {"release_required": str(plan.is_release_required).lower(),
                                             "version": plan.version or "", "tag": plan.tag or ""})


def command_build(args: argparse.Namespace) -> None:
    plan = plan_from_json(read_json(args.plan))
    if not plan.is_release_required:
        raise ConnectorsMCPReleaseError("the plan requires no release")
    evaluator = ReleaseSetEvaluator(ServerBuild(args.module_directory, args.work_directory), format_source=gofmt_source)
    release_set = select_release_set(plan, evaluator)
    evaluator.apply(release_set.connectors)
    for entry in release_set.held:
        print(f"::warning::{entry.connector_id} stays at {entry.held_version}: {entry.rejected_version} did not build or pass tests")
    for connector in release_set.excluded:
        print(f"::warning::{connector.connector_id} {connector.version} is not included: it did not build or pass tests")
    is_required = is_release_required_after_hold(plan, release_set)
    if not is_required:
        print("::warning::Every change was held or excluded, so no Connectors MCP release is needed", flush=True)
    args.result.write_text(json.dumps(release_set_to_json(release_set, is_required), indent=2) + "\n", encoding="utf-8")
    write_github_output(args.github_output, {"release_required": str(is_required).lower()})


def command_package(args: argparse.Namespace) -> None:
    held: list[HeldConnector] = []
    excluded: list[LinkedConnector] = []
    if args.build_result:
        result = read_json(args.build_result)
        held = [HeldConnector(entry["connectorId"], entry["heldVersion"], entry["rejectedVersion"]) for entry in result["held"]]
        excluded = [LinkedConnector.from_json(entry, "build result") for entry in result["excluded"]]
    package_release(repository=args.repository, module_directory=args.module_directory, version=args.version, tag=args.tag,
                    output_directory=args.output_directory, work_directory=args.work_directory,
                    build=ServerBuild(args.module_directory, args.work_directory), held=held, excluded=excluded)


def command_index(args: argparse.Namespace) -> None:
    index = build_release_index(list_github_releases(), download_bytes)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(index, indent=2) + "\n", encoding="utf-8")
    print(f"Connectors MCP index: {len(index['releases'])} releases", flush=True)


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    state = commands.add_parser("state", help="find the latest complete release and an incomplete one to repair")
    state.add_argument("--release-inventory", type=Path, required=True)
    state.add_argument("--repository", type=Path, default=Path("."))
    state.add_argument("--github-output", type=Path)
    state.set_defaults(handler=command_state)
    plan = commands.add_parser("plan", help="decide whether the connector set or server source needs a release")
    plan.add_argument("--inventory", type=Path, required=True, help="connectorctl connectors-mcp-inventory output")
    plan.add_argument("--previous-lock", type=Path)
    plan.add_argument("--previous-tag")
    plan.add_argument("--output", type=Path, required=True)
    plan.add_argument("--github-output", type=Path)
    plan.set_defaults(handler=command_plan)
    build = commands.add_parser("build", help="apply the hold rule and leave the passing registry and go.mod in place")
    build.add_argument("--plan", type=Path, required=True)
    build.add_argument("--module-directory", type=Path, required=True)
    build.add_argument("--work-directory", type=Path, required=True)
    build.add_argument("--result", type=Path, required=True)
    build.add_argument("--github-output", type=Path)
    build.set_defaults(handler=command_build)
    package = commands.add_parser("package", help="build the release archives, lock, checksums, and notes")
    package.add_argument("--repository", type=Path, default=Path("."))
    package.add_argument("--module-directory", type=Path, required=True)
    package.add_argument("--version", required=True)
    package.add_argument("--tag", required=True)
    package.add_argument("--output-directory", type=Path, required=True)
    package.add_argument("--work-directory", type=Path, required=True)
    package.add_argument("--build-result", type=Path)
    package.set_defaults(handler=command_package)
    index = commands.add_parser("index", help="write connectors-mcp/index.json from complete GitHub releases")
    index.add_argument("--output", type=Path, required=True)
    index.set_defaults(handler=command_index)
    args = parser.parse_args(argv)
    try:
        args.handler(args)
    except ConnectorsMCPReleaseError as error:
        print(f"::error::{error}", file=sys.stderr, flush=True)
        raise SystemExit(1) from error


if __name__ == "__main__":
    main()
