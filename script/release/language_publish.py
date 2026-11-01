# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Select first registry publishes and defer PyPI project-creation throttling."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

PYPI_FIRST_PUBLISH_LIMIT = 3
PYPI_RETRY_DELAYS = (20, 60, 120)
LOOKUP_RETRY_DELAYS = (1, 3, 7)
MAXIMUM_GITHUB_MATRIX_JOBS = 256


def has_registry_project(language: str, package: str) -> bool:
    if language == "python":
        url = f"https://pypi.org/pypi/{urllib.parse.quote(package)}/json"
    elif language == "typescript":
        url = f"https://registry.npmjs.org/{urllib.parse.quote(package, safe='')}"
    else:
        raise ValueError(f"unsupported language: {language}")
    return has_registry_url(url, f"{language} project {package}")


def has_registry_url(url: str, description: str, *, sleep=time.sleep) -> bool:
    request = urllib.request.Request(url, headers={"User-Agent": "dex-connector-release/1"})
    for attempt in range(len(LOOKUP_RETRY_DELAYS) + 1):
        try:
            with urllib.request.urlopen(request, timeout=20):
                return True
        except urllib.error.HTTPError as error:
            if error.code == 404:
                return False
            if error.code != 429 and error.code < 500:
                raise RuntimeError(f"registry lookup failed for {description}: HTTP {error.code}") from error
            reason = f"HTTP {error.code}"
        except (urllib.error.URLError, TimeoutError) as error:
            reason = str(error)
        if attempt == len(LOOKUP_RETRY_DELAYS):
            raise RuntimeError(f"registry lookup failed for {description} after retries: {reason}")
        sleep(LOOKUP_RETRY_DELAYS[attempt])
    raise AssertionError("unreachable")


def list_completed_language_tags(*, run=subprocess.run, sleep=time.sleep) -> set[str]:
    command = ("gh", "api", "--paginate", "repos/{owner}/{repo}/releases?per_page=100", "--jq",
               '.[] | select(any(.assets[]; .name == "language-release.complete")) | .tag_name')
    for attempt in range(len(LOOKUP_RETRY_DELAYS) + 1):
        result = run(command, capture_output=True, text=True, check=False)
        if result.returncode == 0:
            return set(result.stdout.splitlines())
        if attempt == len(LOOKUP_RETRY_DELAYS):
            raise RuntimeError(f"GitHub release inventory failed after retries: {result.stderr.strip()}")
        sleep(LOOKUP_RETRY_DELAYS[attempt])
    raise AssertionError("unreachable")


def has_run_project_limit_artifact(repository: str, run_id: str, *, run=subprocess.run, sleep=time.sleep) -> bool:
    artifact_name = f"pypi-project-limit-{run_id}"
    command = ("gh", "api", "--paginate", f"repos/{repository}/actions/runs/{run_id}/artifacts?per_page=100",
               "--jq", ".artifacts[].name")
    for attempt in range(len(LOOKUP_RETRY_DELAYS) + 1):
        result = run(command, capture_output=True, text=True, check=False)
        if result.returncode == 0:
            return artifact_name in result.stdout.splitlines()
        if attempt == len(LOOKUP_RETRY_DELAYS):
            raise RuntimeError(f"PyPI project-limit artifact lookup failed after retries: {result.stderr.strip()}")
        sleep(LOOKUP_RETRY_DELAYS[attempt])
    raise AssertionError("unreachable")


def is_release_complete(item: dict[str, str], completed_tags: set[str]) -> bool:
    return item["tag_prefix"] + item["version"] in completed_tags


def select_release_matrices(items: list[dict[str, str]], first_publish_limit: int,
                            *, completed_tags: set[str] | None = None,
                            project_exists=has_registry_project) -> dict[str, object]:
    if first_publish_limit < 1:
        raise ValueError("PyPI first publish limit must be positive")
    updates: list[dict[str, str]] = []
    first: list[dict[str, str]] = []
    deferred: list[dict[str, str]] = []
    python_first_count = 0
    for item in items:
        if is_release_complete(item, completed_tags or set()):
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
        elif item["language"] == "python" and python_first_count >= first_publish_limit:
            item["defer_reason"] = "first_publish_cap"
            deferred.append(item)
        else:
            item["first_publish"] = True
            first.append(item)
            if item["language"] == "python":
                python_first_count += 1
    if len(updates) + len(first) > MAXIMUM_GITHUB_MATRIX_JOBS:
        available_first = max(0, MAXIMUM_GITHUB_MATRIX_JOBS - len(updates))
        if len(updates) > MAXIMUM_GITHUB_MATRIX_JOBS:
            for item in updates[MAXIMUM_GITHUB_MATRIX_JOBS:] + first:
                item["defer_reason"] = "matrix_limit"
            deferred.extend(updates[MAXIMUM_GITHUB_MATRIX_JOBS:])
            updates = updates[:MAXIMUM_GITHUB_MATRIX_JOBS]
            deferred.extend(first)
            first = []
        else:
            for item in first[available_first:]:
                item["defer_reason"] = "matrix_limit"
            deferred.extend(first[available_first:])
            first = first[:available_first]
    for index, item in enumerate(updates + first):
        item["artifact_index"] = index
    return {"updates": updates, "first": first, "deferred": deferred,
            "pending_count": len(updates) + len(first) + len(deferred)}


def write_selection_outputs(output_path: Path, selection: dict[str, object], go_matrix: str, go_count: str) -> None:
    with output_path.open("a", encoding="utf-8") as output:
        output.write(f"matrix={go_matrix}\ncount={go_count}\n")
        for name in ("updates", "first", "deferred"):
            items = selection[name]
            output.write(f"{name}_matrix={json.dumps({'include': items}, separators=(',', ':'))}\n")
            output.write(f"{name}_count={len(items)}\n")
        output.write(f"language_count={len(selection['updates']) + len(selection['first'])}\n")
        output.write(f"language_matrix={json.dumps({'include': selection['updates'] + selection['first']}, separators=(',', ':'))}\n")
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


def summarize(expected: list[dict[str, str]], results_directory: Path, deferred: list[dict[str, str]]) -> dict[str, object]:
    results: dict[str, dict[str, str]] = {}
    for result_file in results_directory.rglob("release-result.json"):
        result = json.loads(result_file.read_text(encoding="utf-8"))
        tag = result["tag"]
        if tag in results:
            raise ValueError(f"duplicate release result: {tag}")
        results[tag] = result
    published: list[str] = []
    deferred_tags = [item["tag_prefix"] + item["version"] for item in deferred]
    deferred_reasons = {item["tag_prefix"] + item["version"]: item.get("defer_reason", "unspecified") for item in deferred}
    failed: list[str] = []
    for item in expected:
        tag = item["tag_prefix"] + item["version"]
        result = results.get(tag, {"status": "failed"})
        status = result["status"]
        if status == "published":
            published.append(tag)
        elif status == "deferred":
            deferred_tags.append(tag)
            deferred_reasons[tag] = result.get("reason", "unspecified")
        else:
            failed.append(tag)
    return {"published": published, "deferred": deferred_tags, "deferred_reasons": deferred_reasons, "failed": failed,
            "pending_count": len(deferred_tags) + len(failed)}


def main() -> int:
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    select_parser = commands.add_parser("select")
    select_parser.add_argument("--matrix-output", type=Path, required=True)
    select_parser.add_argument("--github-output", type=Path, required=True)
    select_parser.add_argument("--first-publish-limit", type=int, default=PYPI_FIRST_PUBLISH_LIMIT)
    completed_parser = commands.add_parser("completed-language-tags")
    completed_parser.add_argument("--output", type=Path, required=True)
    limit_parser = commands.add_parser("check-project-limit")
    limit_parser.add_argument("--repository", required=True)
    limit_parser.add_argument("--run-id", required=True)
    limit_parser.add_argument("--github-output", type=Path, required=True)
    publish_parser = commands.add_parser("publish-pypi")
    publish_parser.add_argument("--github-output", type=Path, required=True)
    publish_parser.add_argument("assets", nargs="+")
    summary_parser = commands.add_parser("summarize")
    summary_parser.add_argument("--selection", type=Path, required=True)
    summary_parser.add_argument("--results-directory", type=Path, required=True)
    summary_parser.add_argument("--github-output", type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.command == "select":
            entries = dict(line.split("=", 1) for line in args.matrix_output.read_text(encoding="utf-8").splitlines())
            items = json.loads(entries["language_matrix"])["include"]
            try:
                completed_tags = list_completed_language_tags()
            except RuntimeError as error:
                print(f"::warning::{error}; deferring language selection without blocking Go releases", file=sys.stderr)
                for item in items:
                    item["defer_reason"] = "release_inventory_unavailable"
                selection = {"updates": [], "first": [], "deferred": items, "pending_count": len(items)}
            else:
                selection = select_release_matrices(items, args.first_publish_limit, completed_tags=completed_tags)
            write_selection_outputs(args.github_output, selection, entries["matrix"], entries["count"])
            Path(os.environ.get("LANGUAGE_SELECTION_PATH", "/tmp/language-selection.json")).write_text(
                json.dumps(selection) + "\n", encoding="utf-8")
            print(f"Language release selection: {len(selection['updates'])} updates, "
                  f"{len(selection['first'])} first publishes, {len(selection['deferred'])} deferred")
        elif args.command == "completed-language-tags":
            completed_tags = list_completed_language_tags()
            args.output.write_text("\n".join(sorted(completed_tags)) + "\n", encoding="utf-8")
        elif args.command == "check-project-limit":
            if has_run_project_limit_artifact(args.repository, args.run_id):
                with args.github_output.open("a", encoding="utf-8") as output:
                    output.write("status=deferred\nreason=project_creation_limit\n")
                print("::notice::Deferred: this run has already hit the PyPI project-creation limit.")
        elif args.command == "publish-pypi":
            status = publish_pypi(args.assets)
            with args.github_output.open("a", encoding="utf-8") as output:
                output.write(f"status={status}\n")
        else:
            selection = json.loads(args.selection.read_text(encoding="utf-8"))
            expected = selection["updates"] + selection["first"]
            summary = summarize(expected, args.results_directory, selection["deferred"])
            with args.github_output.open("a", encoding="utf-8") as output:
                output.write(f"result={json.dumps(summary, separators=(',', ':'))}\n")
                output.write(f"pending_count={summary['pending_count']}\n")
                output.write(f"published_count={len(summary['published'])}\n")
            with Path(os.environ["GITHUB_STEP_SUMMARY"]).open("a", encoding="utf-8") as output:
                output.write("## Language releases\n\n")
                for status in ("published", "deferred", "failed"):
                    if status == "deferred":
                        labels = [f"{tag} ({summary['deferred_reasons'][tag]})" for tag in summary[status]]
                    else:
                        labels = summary[status]
                    output.write(f"- {status}: {len(summary[status])} — {', '.join(labels) or 'none'}\n")
                output.write(f"- pending: {summary['pending_count']}\n")
            print(json.dumps(summary))
    except (OSError, ValueError, RuntimeError, KeyError, json.JSONDecodeError) as error:
        print(error, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
