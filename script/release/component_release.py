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


@dataclass(frozen=True)
class ReleasePlan:
    component_path: str
    module_path: str
    baseline_tag: str
    baseline_version: str
    bump: str
    version: str
    tag: str
    commits: tuple[str, ...]


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


def validate_release_ref(ref: str) -> None:
    if ref != "refs/heads/main":
        raise ValueError("component releases can run only from main")


def module_path(component_path: Path) -> str:
    go_mod = component_path / "go.mod"
    for line in go_mod.read_text(encoding="utf-8").splitlines():
        if line.startswith("module "):
            return line.removeprefix("module ").strip()
    raise ValueError(f"{go_mod} does not declare a module")


def latest_reachable_tag(tag_prefix: str) -> tuple[str, str]:
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
    if not candidates:
        return "", ""
    _, tag, version = max(candidates)
    return tag, version


def component_commits(component_path: str, baseline_tag: str) -> tuple[str, ...]:
    revision = f"{baseline_tag}..HEAD" if baseline_tag else "HEAD"
    result = git(
        "log",
        "--first-parent",
        "--format=%H",
        revision,
        "--",
        component_path,
    )
    return tuple(line for line in result.stdout.splitlines() if line)


def create_plan(component_path: str, tag_prefix: str, bump: str) -> ReleasePlan:
    path = Path(component_path)
    if not path.is_dir() or not (path / "go.mod").is_file():
        raise ValueError(f"component is not a Go module: {component_path}")
    baseline_tag, baseline_version = latest_reachable_tag(tag_prefix)
    version = next_version(baseline_version, bump)
    commits = component_commits(component_path, baseline_tag)
    if not commits:
        raise ValueError(f"{component_path} has no changes since {baseline_tag or 'repository creation'}")
    tag = tag_prefix + version
    if git("show-ref", "--verify", "--quiet", f"refs/tags/{tag}", check=False).returncode == 0:
        raise ValueError(f"release tag already exists: {tag}")
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
            )
        )
        + "\n",
        encoding="utf-8",
    )


def validate_connector(component_path: str, sdk_module: str) -> str:
    path = Path(component_path)
    if not (path / "go.mod").is_file():
        raise ValueError(f"connector is not a Go module: {component_path}")
    environment = dict(os.environ)
    environment["GOWORK"] = "off"
    result = subprocess.run(
        ("go", "mod", "edit", "-json"),
        cwd=path,
        env=environment,
        check=True,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    module = json.loads(result.stdout)
    if module.get("Replace"):
        raise ValueError("connector modules cannot contain replace directives")
    sdk_requirements = [
        requirement
        for requirement in module.get("Require") or []
        if requirement.get("Path") == sdk_module
    ]
    if len(sdk_requirements) != 1:
        raise ValueError(f"connector must require exactly one {sdk_module} version")
    version = str(sdk_requirements[0].get("Version") or "")
    parse_version(version)
    sdk_tag = f"sdk/go/{version}"
    if git("show-ref", "--verify", "--quiet", f"refs/tags/{sdk_tag}", check=False).returncode != 0:
        raise ValueError(f"connector SDK release tag is missing: {sdk_tag}")
    if git("merge-base", "--is-ancestor", sdk_tag, "HEAD", check=False).returncode != 0:
        raise ValueError(f"connector SDK release is not reachable from HEAD: {sdk_tag}")
    subprocess.run(
        ("go", "mod", "download", f"{sdk_module}@{version}"),
        cwd=path,
        env=environment,
        check=True,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return version


def main() -> int:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    plan_parser = subparsers.add_parser("plan")
    plan_parser.add_argument("--component-path", required=True)
    plan_parser.add_argument("--tag-prefix", required=True)
    plan_parser.add_argument("--bump", required=True)
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
    arguments = parser.parse_args()
    try:
        if arguments.command == "plan":
            if arguments.ref:
                validate_release_ref(arguments.ref)
            plan = create_plan(arguments.component_path, arguments.tag_prefix, arguments.bump)
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
            validate_connector(arguments.component_path, arguments.sdk_module)
    except (OSError, ValueError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        print(error, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
