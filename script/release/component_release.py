# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import argparse
import email.parser
import hashlib
import json
import os
import re
import stat
import subprocess
import sys
import tarfile
import time
import tomllib
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from dataclasses import asdict, dataclass
from pathlib import Path

VERSION_PATTERN = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
RELEASE_ARTIFACT_VERSION_PATTERN = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-rc\.[1-9][0-9]*)?$")
LIBRARY_MODULE_PATH = "github.com/superdurable/dex-connectors-library"
CONNECTOR_MODULE_PATH_PREFIX = f"{LIBRARY_MODULE_PATH}/connectors/"
CONNECTOR_RELEASE_COMPLETE_ASSET = "connector-release.complete"
NPM_REPOSITORY_URL = "git+https://github.com/superdurable/dex-connectors-library.git"


@dataclass(frozen=True)
class ReleasePlan:
    component_path: str
    module_path: str
    baseline_tag: str
    baseline_version: str
    bump: str
    version: str
    tag: str
    source_sha: str
    commits: tuple[str, ...]


@dataclass(frozen=True)
class ConnectorDependency:
    module_path: str
    version: str
    tag: str


class DependencyReleaseLookup:
    """Reads tags, GitHub releases, and module downloads; tests override it to stay offline."""

    def __init__(self, main_ref: str) -> None:
        self.main_ref = main_ref

    def has_tag(self, tag: str) -> bool:
        return git("show-ref", "--verify", "--quiet", f"refs/tags/{tag}", check=False).returncode == 0

    def is_tag_reachable_from_main(self, tag: str) -> bool:
        if git("rev-parse", "--verify", "--quiet", f"{self.main_ref}^{{commit}}", check=False).returncode != 0:
            raise ValueError(f"main branch ref is missing: {self.main_ref}; fetch it or pass --main-ref")
        return git("merge-base", "--is-ancestor", tag, self.main_ref, check=False).returncode == 0

    def release_asset_names(self, tag: str, allow_missing: bool = False) -> frozenset[str]:
        gh_binary = os.environ.get("GH_BIN", "gh")
        result = subprocess.run(
            (gh_binary, "release", "view", tag, "--json", "assets", "--jq", ".assets[].name"),
            check=False,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        if result.returncode != 0:
            if allow_missing and result.stderr.strip() == "release not found":
                return frozenset()
            raise ValueError(f"cannot read the GitHub release for {tag}: {result.stderr.strip()}")
        return frozenset(line for line in result.stdout.splitlines() if line)

    def is_language_sdk_published(self, language: str, version: str) -> bool:
        if language == "python":
            try:
                with urllib.request.urlopen(f"https://pypi.org/pypi/dex-connectors-sdk/{version}/json", timeout=15) as response:
                    return response.status == 200
            except urllib.error.HTTPError as error:
                if error.code == 404:
                    return False
                raise ValueError(f"PyPI SDK lookup failed: HTTP {error.code}") from error
            except urllib.error.URLError as error:
                raise ValueError(f"PyPI SDK lookup failed: {error.reason}") from error
        result = subprocess.run(("npm", "view", f"@superdurable/connectors-sdk@{version}", "version", "--json",
                                 "--registry", "https://registry.npmjs.org"),
                                capture_output=True, text=True, check=False)
        if result.returncode == 0:
            return json.loads(result.stdout) == version
        if "E404" in result.stderr:
            return False
        raise ValueError("npm SDK lookup failed")

    def download_module_directly(self, component_path: Path, module: str, version: str) -> None:
        """Runs go mod download in the connector module; Go adds any missing go.sum entry."""
        environment = dict(os.environ)
        environment["GOWORK"] = "off"
        environment["GOPROXY"] = "direct"
        environment["GONOSUMDB"] = LIBRARY_MODULE_PATH
        result = subprocess.run(
            ("go", "mod", "download", f"{module}@{version}"),
            cwd=component_path,
            env=environment,
            check=False,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        if result.returncode != 0:
            raise ValueError(
                f"{module}@{version} is not downloadable with GOWORK=off GOPROXY=direct: {result.stderr.strip()}"
            )


def git(*arguments: str, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ("git", *arguments),
        check=check,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )


def parse_version(version: str) -> tuple[int, int, int]:
    match = VERSION_PATTERN.fullmatch(version)
    if match is None:
        raise ValueError(f"invalid stable semantic version: {version}")
    return tuple(int(part) for part in match.groups())


def next_version(baseline: str, bump: str) -> str:
    if bump not in {"major", "minor", "patch"}:
        raise ValueError(f"invalid version bump: {bump}")
    if not baseline:
        if bump != "minor":
            raise ValueError("the first component release must use the minor bump")
        return "v0.1.0"
    major, minor, patch = parse_version(baseline)
    if bump == "major":
        return f"v{major + 1}.0.0"
    if bump == "minor":
        return f"v{major}.{minor + 1}.0"
    return f"v{major}.{minor}.{patch + 1}"


def version_bump(baseline: str, target: str) -> str:
    """Classifies a declared version against the component's latest release; a declared version may skip versions."""
    target_parts = parse_version(target)
    if not baseline:
        return "minor"
    baseline_parts = parse_version(baseline)
    if target_parts <= baseline_parts:
        raise ValueError(f"{target} must be after {baseline}")
    if target_parts[0] > baseline_parts[0]:
        return "major"
    if target_parts[1] > baseline_parts[1]:
        return "minor"
    return "patch"


def validate_release_ref(ref: str) -> None:
    if ref != "refs/heads/main":
        raise ValueError("component releases can run only from main")


def module_path(component_path: Path) -> str:
    if (component_path / "pyproject.toml").is_file():
        project = tomllib.loads((component_path / "pyproject.toml").read_text(encoding="utf-8")).get("project", {})
        if not isinstance(project.get("name"), str):
            raise ValueError(f"{component_path}/pyproject.toml has no project name")
        return project["name"]
    if (component_path / "package.json").is_file():
        return json.loads((component_path / "package.json").read_text(encoding="utf-8"))["name"]
    go_mod = component_path / "go.mod"
    for line in go_mod.read_text(encoding="utf-8").splitlines():
        if line.startswith("module "):
            return line.removeprefix("module ").strip()
    raise ValueError(f"{go_mod} does not declare a module")


def declared_package_version(component_path: Path) -> str:
    if (component_path / "package.json").is_file():
        return json.loads((component_path / "package.json").read_text(encoding="utf-8"))["version"]
    project_section = (component_path / "pyproject.toml").read_text(encoding="utf-8").split("[project]", 1)
    if len(project_section) != 2:
        raise ValueError(f"{component_path}/pyproject.toml has no [project] section")
    match = re.search(r'''^version\s*=\s*["']([^"']+)["']\s*(?:#.*)?$''', project_section[1].split("\n[", 1)[0], re.MULTILINE)
    if match is None:
        raise ValueError(f"{component_path}/pyproject.toml has no project version")
    return match.group(1)


def validate_npm_publish_manifest(package_json: Path, manifest: dict[str, object]) -> None:
    if not isinstance(manifest, dict):
        raise ValueError(f"{package_json} must contain a JSON object")
    if manifest.get("private") is True:
        raise ValueError(f"{package_json} is private and cannot be published; remove private before release")
    package_directory = package_json.parent
    if package_directory.name == "sdkts":
        expected_directory = "sdkts"
    elif "connectors" in package_directory.parts:
        connector_index = package_directory.parts.index("connectors")
        expected_directory = "/".join(package_directory.parts[connector_index:])
    else:
        raise ValueError(f"{package_json} is outside a TypeScript SDK or connector directory")
    repository = manifest.get("repository")
    if (not isinstance(repository, dict) or repository.get("type") != "git"
            or repository.get("url") != NPM_REPOSITORY_URL
            or repository.get("directory") != expected_directory):
        raise ValueError(f"{package_json} repository must declare type git, url {NPM_REPOSITORY_URL}, "
                         f"and directory {expected_directory} for npm provenance")


def reachable_tags(tag_prefix: str) -> list[tuple[tuple[int, int, int], str, str]]:
    prefixes = [tag_prefix]
    if tag_prefix.startswith("connectors/") and tag_prefix.endswith("/go/"):
        prefixes.append(tag_prefix.removesuffix("go/"))
    candidates: list[tuple[tuple[int, int, int], str, str]] = []
    for prefix in prefixes:
        result = git(
            "for-each-ref",
            "--merged=HEAD",
            "--format=%(refname:short)",
            f"refs/tags/{prefix}*",
        )
        for tag in result.stdout.splitlines():
            version = tag.removeprefix(prefix)
            try:
                key = parse_version(version)
            except ValueError:
                continue
            candidates.append((key, tag, version))
    return sorted(candidates)


def latest_reachable_tag(tag_prefix: str) -> tuple[str, str]:
    candidates = reachable_tags(tag_prefix)
    if not candidates:
        return "", ""
    _, tag, version = candidates[-1]
    return tag, version


def component_commits(component_path: str, baseline_tag: str, release_ref: str = "HEAD") -> tuple[str, ...]:
    revision = f"{baseline_tag}..{release_ref}" if baseline_tag else release_ref
    paths = [component_path]
    if component_path.startswith("connectors/") and component_path.endswith("/go"):
        paths.append(str(Path(component_path).parent))
    if component_path.startswith("connectors/") and component_path.endswith(("/python", "/typescript")):
        paths.append(str(Path(component_path).parent / "connector.yaml"))
    result = git(
        "log",
        "--first-parent",
        "--format=%H",
        revision,
        "--",
        *paths,
    )
    return tuple(line for line in result.stdout.splitlines() if line)


def create_plan(
    component_path: str,
    tag_prefix: str,
    bump: str = "",
    target_version: str = "",
) -> ReleasePlan:
    path = Path(component_path)
    if not path.is_dir() or not any((path / name).is_file() for name in ("go.mod", "pyproject.toml", "package.json")):
        raise ValueError(f"component has no supported package manifest: {component_path}")
    package_json = path / "package.json"
    if package_json.is_file():
        validate_npm_publish_manifest(package_json, json.loads(package_json.read_text(encoding="utf-8")))
    if bool(bump) == bool(target_version):
        raise ValueError("exactly one of bump or target version is required")
    candidates = reachable_tags(tag_prefix)
    latest_tag = candidates[-1][1] if candidates else ""
    latest_version = candidates[-1][2] if candidates else ""
    if target_version:
        version = target_version
        target_key = parse_version(version)
        target_is_reachable = any(candidate[0] == target_key for candidate in candidates)
        if latest_version and target_key < parse_version(latest_version) and not target_is_reachable:
            raise ValueError(f"target version {version} is behind latest release {latest_version}")
        if target_is_reachable:
            previous = [candidate for candidate in candidates if candidate[0] < target_key]
            baseline_tag = previous[-1][1] if previous else ""
            baseline_version = previous[-1][2] if previous else ""
            release_ref = tag_prefix + version
        else:
            baseline_tag = latest_tag
            baseline_version = latest_version
            release_ref = "HEAD"
        bump = version_bump(baseline_version, version)
    else:
        baseline_tag = latest_tag
        baseline_version = latest_version
        version = next_version(baseline_version, bump)
        release_ref = "HEAD"
        target_is_reachable = False
    commits = component_commits(component_path, baseline_tag, release_ref)
    if not commits:
        raise ValueError(f"{component_path} has no changes since {baseline_tag or 'repository creation'}")
    tag = tag_prefix + version
    tag_exists = git("show-ref", "--verify", "--quiet", f"refs/tags/{tag}", check=False).returncode == 0
    if tag_exists and not target_is_reachable:
        raise ValueError(f"release tag already exists: {tag}")
    source_sha = git("rev-parse", f"{tag}^{{commit}}" if target_is_reachable else "HEAD").stdout.strip()
    module = module_path(path)
    if not (path / "go.mod").is_file() and declared_package_version(path) != version.removeprefix("v"):
        raise ValueError(f"{path} package version must equal planned release {version}")
    major = parse_version(version)[0]
    if (path / "go.mod").is_file() and major >= 2 and not module.endswith(f"/v{major}"):
        raise ValueError(f"module path must end in /v{major} before releasing {version}")
    return ReleasePlan(
        component_path=component_path,
        module_path=module,
        baseline_tag=baseline_tag,
        baseline_version=baseline_version,
        bump=bump,
        version=version,
        tag=tag,
        source_sha=source_sha,
        commits=commits,
    )


def commit_message(commit: str) -> str:
    return git("show", "-s", "--format=%B", commit).stdout.strip()


def associated_pull_request(repository: str, commit: str) -> dict[str, object] | None:
    gh_binary = os.environ.get("GH_BIN", "gh")
    result = subprocess.run(
        (
            gh_binary,
            "api",
            f"repos/{repository}/commits/{commit}/pulls",
            "--method",
            "GET",
        ),
        check=False,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if result.returncode != 0:
        return None
    pulls = json.loads(result.stdout)
    return pulls[0] if pulls else None


def release_notes(plan: ReleasePlan, repository: str) -> tuple[str, bool]:
    breaking: list[str] = []
    changes: list[str] = []
    entries: dict[str, tuple[str, bool]] = {}
    for commit in plan.commits:
        message = commit_message(commit)
        pull = associated_pull_request(repository, commit) if repository else None
        if pull is None:
            title = message.splitlines()[0]
            body = message
            url = f"https://github.com/{repository}/commit/{commit}" if repository else commit
            author = ""
        else:
            title = str(pull.get("title", ""))
            body = str(pull.get("body") or "")
            url = str(pull.get("html_url", ""))
            user = pull.get("user") or {}
            author = f" by @{user.get('login')}" if isinstance(user, dict) and user.get("login") else ""
        is_breaking = "(breaking)" in title or "(breaking)" in body or "(breaking)" in message
        clean_title = title.replace("(breaking)", "").strip()
        entry = f"- [{clean_title}]({url}){author}"
        key = url if pull is not None and url else commit
        previous = entries.get(key)
        entries[key] = (entry, is_breaking or (previous[1] if previous else False))
    for entry, is_breaking in entries.values():
        (breaking if is_breaking else changes).append(entry)
    if (plan.component_path.startswith("connectors/") and plan.component_path.endswith("/go")
            and plan.baseline_tag == str(Path(plan.component_path).parent) + "/" + plan.baseline_version):
        previous_module = f"{LIBRARY_MODULE_PATH}/{Path(plan.component_path).parent.as_posix()}"
        breaking.insert(0, f"- Moved from `{previous_module}` to `{plan.module_path}`.")
    sections = ["## Breaking Changes", "", *(breaking or ["None."]), "", "## Changes", "", *(changes or ["None."]), ""]
    return "\n".join(sections), bool(breaking)


def validate_breaking_bump(plan: ReleasePlan, has_breaking_changes: bool) -> None:
    if not has_breaking_changes:
        return
    baseline_major = parse_version(plan.baseline_version)[0] if plan.baseline_version else 0
    if baseline_major == 0 and plan.bump == "patch":
        raise ValueError("a v0 breaking release cannot use a patch bump")
    is_go_module_migration = (
        plan.component_path.startswith("connectors/")
        and plan.component_path.endswith("/go")
        and plan.baseline_tag == str(Path(plan.component_path).parent) + "/" + plan.baseline_version
        and plan.bump == "minor"
        and plan.version == next_version(plan.baseline_version, "minor")
    )
    if baseline_major >= 1 and plan.bump != "major" and not is_go_module_migration:
        raise ValueError("a v1+ breaking release requires a major bump")


def write_github_output(path: Path, plan: ReleasePlan) -> None:
    values = {"version": plan.version, "tag": plan.tag, "baseline_tag": plan.baseline_tag,
              "module_path": plan.module_path, "source_sha": plan.source_sha}
    if any(ord(character) < 32 or ord(character) == 127 for value in values.values() for character in value):
        raise ValueError("GitHub output values cannot contain control characters")
    path.write_text("".join(f"{name}={value}\n" for name, value in values.items()), encoding="utf-8")


def write_language_release_manifest(
    output: Path, language: str, package: str, version: str, tag: str, source_sha: str, assets: list[Path]
) -> None:
    if language not in {"python", "typescript"}:
        raise ValueError("language release requires Python or TypeScript")
    if not RELEASE_ARTIFACT_VERSION_PATTERN.fullmatch(version) or not tag.endswith("/" + version):
        raise ValueError("language release tag and version disagree")
    verify_language_artifacts(language, package, version, assets)
    artifact_digests: dict[str, str] = {}
    for asset in assets:
        digest = hashlib.sha256(asset.read_bytes()).hexdigest()
        artifact_digests[asset.name] = digest
        asset.with_name(asset.name + ".sha256").write_text(f"{digest}  {asset.name}\n", encoding="utf-8")
    output.write_text(
        json.dumps({"language": language, "package": package, "version": version,
                    "tag": tag, "sourceSha": source_sha, "artifacts": artifact_digests}, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    output.with_name(output.name + ".sha256").write_text(
        f"{hashlib.sha256(output.read_bytes()).hexdigest()}  {output.name}\n", encoding="utf-8"
    )


def verify_language_artifacts(language: str, package: str, version: str, assets: list[Path]) -> None:
    """Reject package builds whose archive identity or file set differs from the release plan."""
    if len(assets) != (2 if language == "python" else 1) or len({asset.name for asset in assets}) != len(assets):
        raise ValueError("language release has an unexpected artifact count")
    expected_version = version.removeprefix("v")
    if language == "python":
        expected_version = expected_version.replace("-rc.", "rc")
    suffixes = sorted(
        ".whl" if asset.name.endswith(".whl") else
        ".tar.gz" if asset.name.endswith(".tar.gz") else
        ".tgz" if asset.name.endswith(".tgz") else "invalid"
        for asset in assets
    )
    if suffixes != ([".tar.gz", ".whl"] if language == "python" else [".tgz"]):
        raise ValueError("language release has unexpected artifact types")
    if len({asset.parent.resolve() for asset in assets}) != 1:
        raise ValueError("language release artifacts must share a directory")
    expected_names = {asset.name for asset in assets}
    entries = [path for path in assets[0].parent.iterdir() if path.is_file() or path.is_symlink()]
    if any(path.is_symlink() for path in entries):
        raise ValueError("language release directory contains non-regular files")
    actual_names = {path.name for path in entries}
    release_names = expected_names | {name + ".sha256" for name in expected_names} | {"language-release.json", "language-release.json.sha256"}
    if actual_names != expected_names and actual_names != release_names:
        raise ValueError("language release directory contains unexpected files")
    for asset in assets:
        connector_manifest_bytes: bytes | None = None
        connector_manifest_path = ""
        if language == "typescript":
            actual_name, actual_version = npm_tarball_identity(asset)
            if package != "@superdurable/connectors-sdk" and (actual_name, actual_version) == (package, expected_version):
                with tarfile.open(asset, "r:gz") as archive:
                    connector_manifest_path, connector_manifest_bytes = read_archive_connector_manifest(
                        archive, ["package/dex-connector.json"]
                    )
        elif asset.name.endswith(".whl"):
            expected_distribution = python_distribution_filename(package)
            if not asset.name.startswith(f"{expected_distribution}-{expected_version}-"):
                raise ValueError("wheel filename differs from planned package and version")
            with zipfile.ZipFile(asset) as archive:
                entries = archive.infolist()
                names = [entry.filename for entry in entries]
                if len(set(names)) != len(names) or any(not safe_archive_path(name) for name in names):
                    raise ValueError("wheel has duplicate or unsafe paths")
                if any(stat.S_IFMT(entry.external_attr >> 16) == stat.S_IFLNK for entry in entries):
                    raise ValueError("wheel contains a symbolic link")
                dist_info = {"/".join(parts[:index + 1]) for name in names
                             for parts in [name.split("/")] for index, part in enumerate(parts)
                             if part.endswith(".dist-info")}
                expected_info = f"{expected_distribution}-{expected_version}.dist-info"
                if dist_info != {expected_info}:
                    raise ValueError("wheel dist-info directory differs from planned package and version")
                metadata_path = f"{expected_info}/METADATA"
                if names.count(metadata_path) != 1:
                    raise ValueError("wheel must contain one METADATA file")
                metadata = email.parser.Parser().parsestr(archive.read(metadata_path).decode("utf-8"))
                actual_name, actual_version = metadata.get("Name"), metadata.get("Version")
                if package != "dex-connectors-sdk" and (actual_name, actual_version) == (package, expected_version):
                    connector_manifest_path, connector_manifest_bytes = read_wheel_connector_manifest(archive, names)
        else:
            expected_distribution = python_distribution_filename(package)
            expected_root = f"{expected_distribution}-{expected_version}"
            if asset.name != f"{expected_root}.tar.gz":
                raise ValueError("sdist filename differs from planned package and version")
            with tarfile.open(asset, "r:gz") as archive:
                members = archive.getmembers()
                names = [member.name for member in members]
                if (len(set(names)) != len(names) or
                    any(not safe_archive_path(name) or name.split("/")[0] != expected_root or
                        not (member.isfile() or member.isdir()) for name, member in zip(names, members))):
                    raise ValueError("sdist has duplicate, unsafe, or unexpected paths")
                info = [member for member in members if member.name.endswith("/PKG-INFO")]
                if len(info) != 1 or info[0].name != f"{expected_root}/PKG-INFO" or not info[0].isfile():
                    raise ValueError("sdist must contain one PKG-INFO file")
                member = archive.extractfile(info[0])
                if member is None:
                    raise ValueError("sdist PKG-INFO is unreadable")
                metadata = email.parser.Parser().parsestr(member.read().decode("utf-8"))
                actual_name, actual_version = metadata.get("Name"), metadata.get("Version")
                if package != "dex-connectors-sdk" and (actual_name, actual_version) == (package, expected_version):
                    connector_manifest_path, connector_manifest_bytes = read_archive_connector_manifest(
                        archive, [name for name in names if re.fullmatch(
                            re.escape(expected_root) + r"/src/dex_connectors/[^/]+/dex-connector\.json", name
                        )]
                    )
        if actual_name != package or actual_version != expected_version:
            raise ValueError(f"{asset.name} identity {actual_name}@{actual_version} differs from planned {package}@{expected_version}")
        if connector_manifest_bytes is not None:
            import_package = verify_packaged_connector_manifest(connector_manifest_bytes, language, package, expected_version)
            if language == "python":
                import_root = "" if asset.name.endswith(".whl") else expected_root + "/src/"
                expected_path = f"{import_root}dex_connectors/{import_package}/dex-connector.json"
                if connector_manifest_path != expected_path:
                    raise ValueError(f"{asset.name} dex-connector.json must be at {expected_path}")


def read_wheel_connector_manifest(archive: zipfile.ZipFile, names: list[str]) -> tuple[str, bytes]:
    matches = [name for name in names if re.fullmatch(r"dex_connectors/[^/]+/dex-connector\.json", name)]
    if len(matches) != 1:
        raise ValueError("wheel must contain one dex-connector.json inside its import package")
    return matches[0], archive.read(matches[0])


def read_archive_connector_manifest(archive: tarfile.TarFile, names: list[str]) -> tuple[str, bytes]:
    if len(names) != 1:
        raise ValueError("package archive must contain one dex-connector.json")
    try:
        member = archive.getmember(names[0])
    except KeyError as error:
        raise ValueError("package archive must contain one dex-connector.json") from error
    if not member.isfile():
        raise ValueError("dex-connector.json must be a regular file")
    content = archive.extractfile(member)
    if content is None:
        raise ValueError("dex-connector.json is unreadable")
    return names[0], content.read()


def verify_packaged_connector_manifest(contents: bytes, language: str, package: str, version: str) -> str:
    try:
        document = json.loads(contents)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ValueError("dex-connector.json is invalid JSON") from error
    identity = document.get("package") if isinstance(document, dict) else None
    expected_ecosystem = "pypi" if language == "python" else "npm"
    if not isinstance(identity, dict) or identity.get("ecosystem") != expected_ecosystem or \
            identity.get("name") != package or identity.get("version") != version:
        raise ValueError(f"dex-connector.json package identity must equal {expected_ecosystem}:{package}@{version}")
    release_tag = document.get("releaseTag")
    match = re.fullmatch(r"connectors/([a-z0-9-]+(?:/[a-z0-9-]+)*)/go/v[0-9]+\.[0-9]+\.[0-9]+", release_tag) \
        if isinstance(release_tag, str) else None
    if match is None:
        raise ValueError("dex-connector.json releaseTag must name a stable Go connector release")
    return re.sub(r"[/\-]", "_", match.group(1))


def python_distribution_filename(package: str) -> str:
    return re.sub(r"[-_.]+", "_", package).lower()


def safe_archive_path(name: str) -> bool:
    parts = name.rstrip("/").split("/")
    return bool(name) and not name.startswith("/") and "\\" not in name and all(part not in {"", ".", ".."} for part in parts)


def npm_tarball_identity(tarball: Path) -> tuple[str, str]:
    """Read the sole package manifest only after validating every npm archive entry."""
    with tarfile.open(tarball, "r:gz") as archive:
        members = archive.getmembers()
        names = [member.name for member in members]
        if (len(set(names)) != len(names) or
            any(not safe_archive_path(name) or (name != "package" and not name.startswith("package/")) or
                (name == "package" and not member.isdir()) or
                not (member.isfile() or member.isdir()) for name, member in zip(names, members))):
            raise ValueError("npm tarball has duplicate, unsafe, or unexpected paths")
        package_json = [member for member in members if member.name == "package/package.json" and member.isfile()]
        if len(package_json) != 1:
            raise ValueError("npm tarball must contain one package/package.json")
        manifest = archive.extractfile(package_json[0])
        if manifest is None:
            raise ValueError("npm package manifest is unreadable")
        identity = json.load(manifest)
        if not isinstance(identity, dict) or not isinstance(identity.get("name"), str) or not isinstance(identity.get("version"), str):
            raise ValueError("npm package manifest has no name or version")
        return identity["name"], identity["version"]


class PyPIReleasePending(ValueError):
    """The just-published PyPI release or one of its files is not visible yet."""


def verify_pypi_artifacts(
    package: str, version: str, assets: list[Path], require_all: bool = False, wait_seconds: int = 0
) -> None:
    """Compare local distributions with PyPI, waiting only for post-publish visibility when requested."""
    if wait_seconds < 0 or (wait_seconds and not require_all):
        raise ValueError("PyPI visibility waiting requires --require-all and nonnegative seconds")
    deadline = time.monotonic() + wait_seconds
    while True:
        try:
            request_timeout = min(15, max(0.1, deadline - time.monotonic())) if wait_seconds else 15
            verify_pypi_artifacts_once(package, version, assets, require_all, request_timeout)
            return
        except PyPIReleasePending as error:
            if not wait_seconds:
                raise
            remaining_seconds = deadline - time.monotonic()
            if remaining_seconds <= 0:
                raise ValueError(f"PyPI release did not become visible within {wait_seconds}s: {error}") from error
            time.sleep(min(5, remaining_seconds))


def verify_pypi_artifacts_once(
    package: str, version: str, assets: list[Path], require_all: bool, request_timeout: float
) -> None:
    public_version = version.removeprefix("v").replace("-rc.", "rc")
    url = f"https://pypi.org/pypi/{urllib.parse.quote(package, safe='')}/{urllib.parse.quote(public_version, safe='')}/json"
    try:
        with urllib.request.urlopen(url, timeout=request_timeout) as response:
            release = json.load(response)
    except urllib.error.HTTPError as error:
        if error.code == 404 and not require_all:
            return
        if error.code == 404 or error.code == 429 or 500 <= error.code <= 599:
            raise PyPIReleasePending(f"PyPI release lookup failed: HTTP {error.code}") from error
        raise ValueError(f"PyPI release lookup failed: HTTP {error.code}") from error
    except (urllib.error.URLError, TimeoutError, OSError) as error:
        raise PyPIReleasePending(f"PyPI release lookup failed: {error}") from error
    published = {entry["filename"]: entry["digests"]["sha256"] for entry in release["urls"]}
    for asset in assets:
        digest = published.get(asset.name)
        if digest is None:
            if require_all:
                raise PyPIReleasePending(f"PyPI release is missing {asset.name}")
            continue
        if digest != hashlib.sha256(asset.read_bytes()).hexdigest():
            raise ValueError(f"PyPI artifact {asset.name} has different SHA256 from the local release")


def validate_language_package(
    component_path: str, language: str, lookup: DependencyReleaseLookup, require_go_release: bool = True,
    allow_pending_go_release: bool = False,
) -> str:
    """Require a registry-ready package with an exact, released SDK dependency."""
    path = Path(component_path)
    if language == "python":
        manifest = tomllib.loads((path / "pyproject.toml").read_text(encoding="utf-8"))
        if manifest.get("tool", {}).get("uv", {}).get("sources"):
            raise ValueError("Python package cannot use tool.uv.sources overrides")
        project = manifest.get("project", {})
        dependencies = list(project.get("dependencies", []))
        for optional in project.get("optional-dependencies", {}).values():
            dependencies.extend(optional)
        for group in manifest.get("dependency-groups", {}).values():
            dependencies.extend(group)
        sdk_name = "dex-connectors-sdk"
        sdk_versions = [match.group(1) for dependency in dependencies if (match := re.fullmatch(r"dex-connectors-sdk(?:==|>=)([0-9]+\.[0-9]+\.[0-9]+)", dependency))]
        if any(re.search(r"(?:file:|git\+|git@|https?://|ssh://|\.\./|workspace:|path\s*=)", dependency, re.IGNORECASE) for dependency in dependencies):
            raise ValueError("Python package cannot use path, file, git, or workspace dependencies")
        tag_prefix = "sdkpython/"
    elif language == "typescript":
        package_json = path / "package.json"
        manifest = json.loads(package_json.read_text(encoding="utf-8"))
        validate_npm_publish_manifest(package_json, manifest)
        dependency_groups = [manifest.get(name, {}) for name in ("dependencies", "optionalDependencies", "peerDependencies", "devDependencies", "overrides")]
        if any(re.search(r"^(?:file:|link:|git(?:\+|:)|git@|github:|https?://|ssh://|workspace:|\.\.?/)", value, re.IGNORECASE)
               for group in dependency_groups for value in group.values()):
            raise ValueError("TypeScript package cannot use path, file, git, or workspace dependencies")
        dependencies = {name: value for group in dependency_groups[:3] for name, value in group.items()}
        sdk_name = "@superdurable/connectors-sdk"
        sdk_version = dependencies.get(sdk_name, "")
        match = re.fullmatch(r"(?:>=|\^|~)?([0-9]+\.[0-9]+\.[0-9]+)", sdk_version)
        sdk_versions = [match.group(1)] if match else []
        tag_prefix = "sdkts/"
    else:
        raise ValueError(f"unsupported language: {language}")
    if len(sdk_versions) != 1:
        raise ValueError(f"{component_path} must require one exact {sdk_name} release")
    tag = tag_prefix + "v" + sdk_versions[0]
    if not lookup.has_tag(tag) or not lookup.is_tag_reachable_from_main(tag):
        raise ValueError(f"required SDK release tag is missing or not on main: {tag}")
    if "language-release.json" not in lookup.release_asset_names(tag):
        raise ValueError(f"required SDK GitHub release is missing: {tag}")
    if not lookup.is_language_sdk_published(language, sdk_versions[0]):
        raise ValueError(f"required SDK version is not published in the registry: {tag}")
    validate_language_connector_release(path, language, lookup, require_go_release, allow_pending_go_release)
    return tag


def validate_language_connector_release(
    path: Path, language: str, lookup: DependencyReleaseLookup, require_completed_release: bool = True,
    allow_pending_go_release: bool = False,
) -> None:
    connector_manifest_path = language_connector_manifest_path(path, language)
    if not connector_manifest_path.is_file():
        raise ValueError(f"{path} must contain generated {connector_manifest_path}")
    document = json.loads(connector_manifest_path.read_text(encoding="utf-8"))
    package_identity = document.get("package") if isinstance(document, dict) else None
    expected_ecosystem = "pypi" if language == "python" else "npm"
    if not isinstance(package_identity, dict) or package_identity.get("ecosystem") != expected_ecosystem or \
            package_identity.get("name") != module_path(path) or \
            package_identity.get("version") != declared_package_version(path):
        raise ValueError(f"{connector_manifest_path} package identity differs from {path} package metadata")
    release_tag = document.get("releaseTag") if isinstance(document, dict) else None
    connector_path = "/".join(path.parts[path.parts.index("connectors") + 1:-1])
    if not isinstance(release_tag, str) or not re.fullmatch(
        r"connectors/" + re.escape(connector_path) + r"/go/v[0-9]+\.[0-9]+\.[0-9]+", release_tag
    ):
        raise ValueError(f"{connector_manifest_path} releaseTag must name its Go connector release")
    source_manifest = path.parent / "connector.yaml"
    if not source_manifest.is_file():
        raise ValueError(f"{path} requires {source_manifest}")
    validate_language_capability_coverage(source_manifest, language, document)
    if allow_pending_go_release:
        version_match = re.search(r"(?m)^  version: (v[0-9]+\.[0-9]+\.[0-9]+)$",
                                  source_manifest.read_text(encoding="utf-8"))
        if version_match is None or not VERSION_PATTERN.fullmatch(version_match.group(1)):
            raise ValueError(f"{source_manifest} has no stable metadata.version")
        expected_tag = f"connectors/{connector_path}/go/{version_match.group(1)}"
        if release_tag != expected_tag:
            raise ValueError(f"{connector_manifest_path} releaseTag must equal current Go connector tag {expected_tag}")
    if not require_completed_release:
        return
    if not lookup.has_tag(release_tag):
        if allow_pending_go_release:
            return
        raise ValueError(f"required Go connector release tag is missing: {release_tag}")
    if not lookup.is_tag_reachable_from_main(release_tag):
        raise ValueError(f"required Go connector release is not reachable from main: {release_tag}")
    assets = lookup.release_asset_names(release_tag, allow_missing=True) if allow_pending_go_release else lookup.release_asset_names(release_tag)
    if CONNECTOR_RELEASE_COMPLETE_ASSET not in assets and not allow_pending_go_release:
        raise ValueError(f"required Go connector release is incomplete: {release_tag} has no {CONNECTOR_RELEASE_COMPLETE_ASSET} asset")


def validate_language_capability_coverage(source_manifest: Path, language: str, document: dict) -> None:
    """Check declared Step coverage before a language package is released."""
    operations, triggers = read_declared_language_capabilities(source_manifest, language)
    factories = document.get("factories")
    if not isinstance(factories, list) or any(not isinstance(factory, dict) for factory in factories):
        raise ValueError("dex-connector.json factories must be a list")
    factory_operations = [factory.get("operationId") for factory in factories]
    if any(not isinstance(operation, str) for operation in factory_operations):
        raise ValueError("dex-connector.json factories must name operation IDs")
    if len(set(factory_operations)) != len(factory_operations) or set(factory_operations) != operations:
        raise ValueError(f"{language} declared operations differ from dex-connector.json factories")
    if triggers:
        raise ValueError(f"{language} Trigger factories are not supported by dex-connector.json")


def read_declared_language_capabilities(source_manifest: Path, language: str) -> tuple[set[str], set[str]]:
    repository_root = next((parent for parent in source_manifest.resolve().parents
                            if (parent / "cmd/connectorctl/main.go").is_file()), None)
    if repository_root is None:
        raise ValueError(f"connectorctl is unavailable for {source_manifest}")
    result = subprocess.run(
        ["go", "run", "./cmd/connectorctl", "language-capabilities", str(source_manifest.resolve()), language],
        cwd=repository_root, env={**os.environ, "GOWORK": "off"}, capture_output=True, text=True, check=False,
    )
    if result.returncode != 0:
        raise ValueError(f"connectorctl could not validate language capabilities: {result.stderr.strip()}")
    try:
        declared = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise ValueError("connectorctl returned invalid language capabilities JSON") from error
    if not isinstance(declared, dict) or not all(isinstance(declared.get(field), list) and
            all(isinstance(name, str) for name in declared[field]) for field in ("operations", "triggers")):
        raise ValueError("connectorctl returned invalid language capability lists")
    return set(declared["operations"]), set(declared["triggers"])


def stamp_language_candidate_connector_manifest(path: Path, language: str, candidate_version: str) -> None:
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*", candidate_version):
        raise ValueError(f"invalid language candidate version: {candidate_version}")
    connector_manifest_path = language_connector_manifest_path(path, language)
    if not connector_manifest_path.is_file():
        raise ValueError(f"{path} must contain generated {connector_manifest_path}")
    document = json.loads(connector_manifest_path.read_text(encoding="utf-8"))
    stable_version = candidate_version.removeprefix("v").split("-rc.", 1)[0]
    package_identity = document.get("package") if isinstance(document, dict) else None
    if not isinstance(package_identity, dict) or package_identity.get("version") != stable_version:
        raise ValueError(f"{connector_manifest_path} package.version must equal {stable_version} before candidate build")
    package_identity["version"] = (candidate_version.removeprefix("v").replace("-rc.", "rc")
                                   if language == "python" else candidate_version.removeprefix("v"))
    connector_manifest_path.write_text(json.dumps(document, indent=2) + "\n", encoding="utf-8")


def language_connector_manifest_path(path: Path, language: str) -> Path:
    if "connectors" not in path.parts or path.name != language:
        raise ValueError(f"language connector package is outside connectors/: {path}")
    connector_directory = "/".join(path.parts[path.parts.index("connectors") + 1:-1])
    if not connector_directory:
        raise ValueError(f"language connector package is outside connectors/: {path}")
    if language == "python":
        import_package = re.sub(r"[/\-]", "_", connector_directory)
        return path / "src" / "dex_connectors" / import_package / "dex-connector.json"
    if language == "typescript":
        return path / "dex-connector.json"
    raise ValueError(f"unsupported language: {language}")


def validate_connector(component_path: str, sdk_module: str, lookup: DependencyReleaseLookup) -> tuple[str, ...]:
    """Validates a connector's library dependencies and returns their release tags, SDK first."""
    path = Path(component_path)
    if not (path / "go.mod").is_file():
        raise ValueError(f"connector is not a Go module: {component_path}")
    environment = dict(os.environ)
    environment["GOWORK"] = "off"
    result = subprocess.run(
        ("go", "mod", "edit", "-json"),
        cwd=path,
        env=environment,
        check=False,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if result.returncode != 0:
        # Go rejects branch and commit versions here, so its message names the requirement.
        raise ValueError(f"cannot read {path / 'go.mod'}: {result.stderr.strip()}")
    module = json.loads(result.stdout)
    if module.get("Replace"):
        raise ValueError("connector modules cannot contain replace directives")
    requirements = module.get("Require") or []
    sdk_requirements = [requirement for requirement in requirements if requirement.get("Path") == sdk_module]
    if len(sdk_requirements) != 1:
        raise ValueError(f"connector must require exactly one {sdk_module} version")
    sdk_version = str(sdk_requirements[0].get("Version") or "")
    parse_version(sdk_version)
    sdk_tag = f"sdkgo/{sdk_version}"
    if not lookup.has_tag(sdk_tag):
        raise ValueError(f"connector SDK release tag is missing: {sdk_tag}")
    if not lookup.is_tag_reachable_from_main(sdk_tag):
        raise ValueError(f"connector SDK release is not reachable from main: {sdk_tag}")
    connector_dependencies: list[ConnectorDependency] = []
    for requirement in requirements:
        dependency_module = str(requirement.get("Path") or "")
        if dependency_module == sdk_module:
            continue
        if dependency_module.startswith(CONNECTOR_MODULE_PATH_PREFIX):
            dependency_version = str(requirement.get("Version") or "")
            dependency = ConnectorDependency(
                module_path=dependency_module,
                version=dependency_version,
                tag=connector_dependency_tag(dependency_module, dependency_version),
            )
            validate_connector_dependency_release(lookup, dependency.tag)
            connector_dependencies.append(dependency)
        elif dependency_module == LIBRARY_MODULE_PATH or dependency_module.startswith(f"{LIBRARY_MODULE_PATH}/"):
            raise ValueError(
                "connector modules may require only the SDK and released connector modules "
                f"from this repository: {dependency_module}"
            )
    download_without_changing_go_sum(lookup, path, sdk_module, sdk_version)
    for dependency in connector_dependencies:
        download_without_changing_go_sum(lookup, path, dependency.module_path, dependency.version)
    return (sdk_tag, *(dependency.tag for dependency in connector_dependencies))


def download_without_changing_go_sum(
    lookup: DependencyReleaseLookup, component_path: Path, module: str, version: str
) -> None:
    """Downloads inside the connector so Go checks committed go.sum hashes, and rejects any go.sum edit."""
    go_sum_path = component_path / "go.sum"
    original_go_sum = read_file_bytes_if_present(go_sum_path)
    try:
        lookup.download_module_directly(component_path, module, version)
    finally:
        has_go_sum_changed = read_file_bytes_if_present(go_sum_path) != original_go_sum
        if has_go_sum_changed:
            restore_file_bytes(go_sum_path, original_go_sum)
    if has_go_sum_changed:
        raise ValueError(f"{go_sum_path} is missing entries for {module}@{version}; run go mod tidy")


def read_file_bytes_if_present(path: Path) -> bytes | None:
    return path.read_bytes() if path.is_file() else None


def restore_file_bytes(path: Path, original: bytes | None) -> None:
    if original is None:
        path.unlink(missing_ok=True)
    else:
        path.write_bytes(original)


def connector_dependency_tag(dependency_module: str, version: str) -> str:
    try:
        major = parse_version(version)[0]
    except ValueError as error:
        raise ValueError(f"connector dependency {dependency_module} must pin a released version: {error}") from None
    directory = dependency_module.removeprefix(f"{LIBRARY_MODULE_PATH}/")
    if major >= 2:
        directory = directory.removesuffix(f"/v{major}")
    return f"{directory}/{version}"


def validate_connector_dependency_release(lookup: DependencyReleaseLookup, tag: str) -> None:
    if not lookup.has_tag(tag):
        raise ValueError(f"connector dependency release tag is missing: {tag}; release the dependency first")
    if not lookup.is_tag_reachable_from_main(tag):
        raise ValueError(f"connector dependency release is not reachable from main: {tag}")
    if CONNECTOR_RELEASE_COMPLETE_ASSET not in lookup.release_asset_names(tag):
        raise ValueError(
            f"connector dependency release is incomplete: {tag} has no {CONNECTOR_RELEASE_COMPLETE_ASSET} asset"
        )


def main() -> int:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    plan_parser = subparsers.add_parser("plan")
    plan_parser.add_argument("--component-path", required=True)
    plan_parser.add_argument("--tag-prefix", required=True)
    version_group = plan_parser.add_mutually_exclusive_group(required=True)
    version_group.add_argument("--bump")
    version_group.add_argument("--version")
    plan_parser.add_argument("--ref", default="")
    plan_parser.add_argument("--json-output", type=Path, required=True)
    plan_parser.add_argument("--github-output", type=Path, required=True)
    notes_parser = subparsers.add_parser("notes")
    notes_parser.add_argument("--plan", type=Path, required=True)
    notes_parser.add_argument("--repository", required=True)
    notes_parser.add_argument("--output", type=Path, required=True)
    validate_parser = subparsers.add_parser("validate-connector")
    validate_parser.add_argument("--component-path", required=True)
    validate_parser.add_argument("--sdk-module", required=True)
    validate_parser.add_argument("--main-ref", default="origin/main")
    language_validate_parser = subparsers.add_parser("validate-language-package")
    language_validate_parser.add_argument("--component-path", required=True)
    language_validate_parser.add_argument("--language", choices=("python", "typescript"), required=True)
    language_validate_parser.add_argument("--main-ref", default="origin/main")
    language_validate_mode = language_validate_parser.add_mutually_exclusive_group()
    language_validate_mode.add_argument("--dry-run", action="store_true")
    language_validate_mode.add_argument("--allow-pending-go-release", action="store_true")
    candidate_manifest_parser = subparsers.add_parser("stamp-language-candidate-manifest")
    candidate_manifest_parser.add_argument("--component-path", type=Path, required=True)
    candidate_manifest_parser.add_argument("--language", choices=("python", "typescript"), required=True)
    candidate_manifest_parser.add_argument("--version", required=True)
    manifest_parser = subparsers.add_parser("language-manifest")
    manifest_parser.add_argument("--output", type=Path, required=True)
    manifest_parser.add_argument("--language", choices=("python", "typescript"), required=True)
    manifest_parser.add_argument("--package", required=True)
    manifest_parser.add_argument("--version", required=True)
    manifest_parser.add_argument("--tag", required=True)
    manifest_parser.add_argument("--source-sha", required=True)
    manifest_parser.add_argument("--asset", type=Path, action="append", required=True)
    verify_parser = subparsers.add_parser("verify-language-artifacts")
    verify_parser.add_argument("--language", choices=("python", "typescript"), required=True)
    verify_parser.add_argument("--package", required=True)
    verify_parser.add_argument("--version", required=True)
    verify_parser.add_argument("--asset", type=Path, action="append", required=True)
    registry_parser = subparsers.add_parser("verify-pypi-artifacts")
    registry_parser.add_argument("--package", required=True)
    registry_parser.add_argument("--version", required=True)
    registry_parser.add_argument("--asset", type=Path, action="append", required=True)
    registry_parser.add_argument("--require-all", action="store_true")
    registry_parser.add_argument("--wait-seconds", type=int, default=0)
    arguments = parser.parse_args()
    try:
        if arguments.command == "plan":
            if arguments.ref:
                validate_release_ref(arguments.ref)
            plan = create_plan(
                arguments.component_path,
                arguments.tag_prefix,
                bump=arguments.bump or "",
                target_version=arguments.version or "",
            )
            arguments.json_output.write_text(json.dumps(asdict(plan), indent=2) + "\n", encoding="utf-8")
            write_github_output(arguments.github_output, plan)
        elif arguments.command == "notes":
            raw = json.loads(arguments.plan.read_text(encoding="utf-8"))
            raw["commits"] = tuple(raw["commits"])
            plan = ReleasePlan(**raw)
            notes, has_breaking_changes = release_notes(plan, arguments.repository)
            validate_breaking_bump(plan, has_breaking_changes)
            arguments.output.write_text(notes, encoding="utf-8")
        elif arguments.command == "language-manifest":
            write_language_release_manifest(arguments.output, arguments.language, arguments.package,
                                            arguments.version, arguments.tag, arguments.source_sha, arguments.asset)
        elif arguments.command == "verify-language-artifacts":
            verify_language_artifacts(arguments.language, arguments.package, arguments.version, arguments.asset)
        elif arguments.command == "verify-pypi-artifacts":
            verify_pypi_artifacts(arguments.package, arguments.version, arguments.asset, arguments.require_all,
                                  arguments.wait_seconds)
        elif arguments.command == "validate-language-package":
            print(validate_language_package(arguments.component_path, arguments.language,
                                            DependencyReleaseLookup(arguments.main_ref), not arguments.dry_run,
                                            arguments.allow_pending_go_release))
        elif arguments.command == "stamp-language-candidate-manifest":
            stamp_language_candidate_connector_manifest(arguments.component_path, arguments.language,
                                                        arguments.version)
        else:
            tags = validate_connector(
                arguments.component_path,
                arguments.sdk_module,
                DependencyReleaseLookup(arguments.main_ref),
            )
            print(f"{arguments.component_path}: {', '.join(tags)}")
    except (OSError, ValueError, subprocess.CalledProcessError, json.JSONDecodeError, tarfile.TarError, zipfile.BadZipFile, KeyError) as error:
        print(error, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
