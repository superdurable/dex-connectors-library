# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from dataclasses import asdict, dataclass
from pathlib import Path

VERSION_PATTERN = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
LIBRARY_MODULE_PATH = "github.com/superdurable/dex-connectors-library"
CONNECTOR_MODULE_PATH_PREFIX = f"{LIBRARY_MODULE_PATH}/connectors/"
CONNECTOR_RELEASE_COMPLETE_ASSET = "connector-release.complete"


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

    def release_asset_names(self, tag: str) -> frozenset[str]:
        gh_binary = os.environ.get("GH_BIN", "gh")
        result = subprocess.run(
            (gh_binary, "release", "view", tag, "--json", "assets", "--jq", ".assets[].name"),
            check=False,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        if result.returncode != 0:
            raise ValueError(f"cannot read the GitHub release for {tag}: {result.stderr.strip()}")
        return frozenset(line for line in result.stdout.splitlines() if line)

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
    go_mod = component_path / "go.mod"
    for line in go_mod.read_text(encoding="utf-8").splitlines():
        if line.startswith("module "):
            return line.removeprefix("module ").strip()
    raise ValueError(f"{go_mod} does not declare a module")


def reachable_tags(tag_prefix: str) -> list[tuple[tuple[int, int, int], str, str]]:
    result = git(
        "for-each-ref",
        "--merged=HEAD",
        "--format=%(refname:short)",
        f"refs/tags/{tag_prefix}*",
    )
    candidates: list[tuple[tuple[int, int, int], str, str]] = []
    for tag in result.stdout.splitlines():
        version = tag.removeprefix(tag_prefix)
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
    result = git(
        "log",
        "--first-parent",
        "--format=%H",
        revision,
        "--",
        component_path,
    )
    return tuple(line for line in result.stdout.splitlines() if line)


def create_plan(
    component_path: str,
    tag_prefix: str,
    bump: str = "",
    target_version: str = "",
) -> ReleasePlan:
    path = Path(component_path)
    if not path.is_dir() or not (path / "go.mod").is_file():
        raise ValueError(f"component is not a Go module: {component_path}")
    if bool(bump) == bool(target_version):
        raise ValueError("exactly one of bump or target version is required")
    candidates = reachable_tags(tag_prefix)
    latest_tag = candidates[-1][1] if candidates else ""
    latest_version = candidates[-1][2] if candidates else ""
    if target_version:
        version = target_version
        if latest_version and parse_version(version) < parse_version(latest_version):
            raise ValueError(f"target version {version} is behind latest release {latest_version}")
        target_is_reachable = latest_version == version
        if target_is_reachable:
            previous = [candidate for candidate in candidates if candidate[0] < parse_version(version)]
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
    major = parse_version(version)[0]
    if major >= 2 and not module.endswith(f"/v{major}"):
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
        (breaking if is_breaking else changes).append(entry)
    sections = ["## Breaking Changes", "", *(breaking or ["None."]), "", "## Changes", "", *(changes or ["None."]), ""]
    return "\n".join(sections), bool(breaking)


def validate_breaking_bump(plan: ReleasePlan, has_breaking_changes: bool) -> None:
    if not has_breaking_changes:
        return
    baseline_major = parse_version(plan.baseline_version)[0] if plan.baseline_version else 0
    if baseline_major == 0 and plan.bump == "patch":
        raise ValueError("a v0 breaking release cannot use a patch bump")
    if baseline_major >= 1 and plan.bump != "major":
        raise ValueError("a v1+ breaking release requires a major bump")


def write_github_output(path: Path, plan: ReleasePlan) -> None:
    path.write_text(
        "\n".join(
            (
                f"version={plan.version}",
                f"tag={plan.tag}",
                f"baseline_tag={plan.baseline_tag}",
                f"module_path={plan.module_path}",
                f"source_sha={plan.source_sha}",
            )
        )
        + "\n",
        encoding="utf-8",
    )


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
        else:
            tags = validate_connector(
                arguments.component_path,
                arguments.sdk_module,
                DependencyReleaseLookup(arguments.main_ref),
            )
            print(f"{arguments.component_path}: {', '.join(tags)}")
    except (OSError, ValueError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        print(error, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
