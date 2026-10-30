# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Renames connector GitHub releases to `<displayName> <Language> <version>`.

Prints a dry-run table by default; `--apply` edits only releases whose title differs.
A connector whose manifest is missing from main keeps the display name of its current
`<displayName> <version>` title. Any other Git or manifest failure stops before an edit.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
import time
from dataclasses import dataclass

from component_release import LANGUAGE_DISPLAY_NAMES, connector_release_title

CONNECTOR_RELEASE_TAG_PATTERN = re.compile(
    r"^(?P<component>connectors/[^\s]+)/(?P<version>v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    r"(?:-rc\.[1-9][0-9]*)?)$"
)
MANIFEST_DISPLAY_NAME_PATTERN = re.compile(r"^  displayName:\s*(?P<value>.+?)\s*$")
DISPLAY_NAME_SOURCE_MANIFEST = "manifest"
DISPLAY_NAME_SOURCE_CURRENT_TITLE = "current title"


@dataclass(frozen=True)
class ConnectorReleaseTag:
    tag: str
    directory: str
    language: str
    version: str


@dataclass(frozen=True)
class ReleaseRetitle:
    tag: str
    current_title: str
    new_title: str
    skip_reason: str
    display_name_source: str = DISPLAY_NAME_SOURCE_MANIFEST

    @property
    def is_change_required(self) -> bool:
        return not self.skip_reason and self.current_title != self.new_title


def parse_connector_release_tag(tag: str) -> ConnectorReleaseTag | None:
    """Returns the connector directory, language, and version of a tag; legacy root tags are Go."""
    match = CONNECTOR_RELEASE_TAG_PATTERN.fullmatch(tag)
    if match is None:
        return None
    component = match.group("component")
    directory, _, last_segment = component.rpartition("/")
    if last_segment in LANGUAGE_DISPLAY_NAMES and directory != "connectors":
        return ConnectorReleaseTag(tag, directory, last_segment, match.group("version"))
    return ConnectorReleaseTag(tag, component, "go", match.group("version"))


def read_manifest_display_name(manifest: str) -> str:
    is_inside_metadata = False
    for line in manifest.splitlines():
        if line and not line[0].isspace():
            is_inside_metadata = line.rstrip() == "metadata:"
            continue
        match = MANIFEST_DISPLAY_NAME_PATTERN.fullmatch(line) if is_inside_metadata else None
        if match is not None:
            value = match.group("value")
            if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
                value = value[1:-1]
            return value
    raise ValueError("manifest has no metadata.displayName")


def plan_release_retitles(releases: list[dict[str, str]], display_names: dict[str, str | None]) -> list[ReleaseRetitle]:
    """Plans one retitle per connector release.

    `display_names` maps a directory to its main manifest display name, or None when main has no manifest.
    """
    retitles: list[ReleaseRetitle] = []
    for release in releases:
        parsed = parse_connector_release_tag(release["tag_name"])
        if parsed is None:
            continue
        current_title = release.get("name") or ""
        display_name = display_names[parsed.directory]
        display_name_source = DISPLAY_NAME_SOURCE_MANIFEST
        if display_name is None:
            display_name = read_legacy_title_display_name(current_title, parsed.version)
            display_name_source = DISPLAY_NAME_SOURCE_CURRENT_TITLE
        if display_name is None:
            retitles.append(ReleaseRetitle(parsed.tag, current_title, "", f"no manifest at {parsed.directory}",
                                           display_name_source))
            continue
        new_title = connector_release_title(display_name, parsed.language, parsed.version)
        retitles.append(ReleaseRetitle(parsed.tag, current_title, new_title, "", display_name_source))
    return sorted(retitles, key=lambda retitle: retitle.tag)


def read_legacy_title_display_name(title: str, version: str) -> str | None:
    suffix = f" {version}"
    if not title.endswith(suffix):
        return None
    display_name = title[: -len(suffix)].strip()
    for language_display_name in LANGUAGE_DISPLAY_NAMES.values():
        if display_name.lower().endswith(f" {language_display_name.lower()}"):
            display_name = display_name[: -len(language_display_name) - 1].strip()
            break
    return display_name or None


def list_releases(repository: str) -> list[dict[str, str]]:
    output = subprocess.run(
        ["gh", "api", "--paginate", f"repos/{repository}/releases?per_page=100",
         "--jq", ".[] | {tag_name, name}"],
        check=True, capture_output=True, text=True,
    ).stdout
    return [json.loads(line) for line in output.splitlines() if line.strip()]


def verify_main_ref(main_ref: str) -> None:
    run_git(["rev-parse", "--verify", "--end-of-options", f"{main_ref}^{{commit}}"],
            f"main ref {main_ref!r} does not resolve to a commit")


def read_main_display_name(main_ref: str, directory: str) -> str | None:
    """Returns the display name of the manifest on `main_ref`, or None only when that path is missing."""
    manifest_path = f"{directory}/connector.yaml"
    listing = run_git(["ls-tree", "--name-only", main_ref, "--", manifest_path],
                      f"cannot list {manifest_path} on {main_ref}")
    if not listing.strip():
        return None
    manifest = run_git(["show", f"{main_ref}:{manifest_path}"], f"cannot read {manifest_path} on {main_ref}")
    try:
        return read_manifest_display_name(manifest)
    except ValueError as error:
        raise ValueError(f"invalid {manifest_path} on {main_ref}: {error}") from error


def run_git(arguments: list[str], failure_message: str) -> str:
    result = subprocess.run(["git", *arguments], capture_output=True, text=True)
    if result.returncode != 0:
        raise ValueError(f"{failure_message}: {result.stderr.strip()}")
    return result.stdout


def print_retitle_table(retitles: list[ReleaseRetitle], main_ref: str) -> None:
    from_manifest = [retitle for retitle in retitles if retitle.display_name_source == DISPLAY_NAME_SOURCE_MANIFEST]
    from_current_title = [retitle for retitle in retitles if retitle.display_name_source != DISPLAY_NAME_SOURCE_MANIFEST]
    print_retitle_rows(from_manifest)
    if from_current_title:
        print(f"\n# No connector.yaml on {main_ref}; display name taken from the current title")
        print_retitle_rows(from_current_title)


def print_retitle_rows(retitles: list[ReleaseRetitle]) -> None:
    for retitle in retitles:
        if retitle.skip_reason:
            status = f"skip ({retitle.skip_reason})"
        elif retitle.is_change_required:
            status = "change"
        else:
            status = "unchanged"
        print(f"{status}\t{retitle.tag}\t{retitle.current_title}\t{retitle.new_title}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", default="superdurable/dex-connectors-library")
    parser.add_argument("--main-ref", default="origin/main")
    parser.add_argument("--apply", action="store_true", help="edit release titles instead of printing a dry run")
    parser.add_argument("--delay-seconds", type=float, default=1.0, help="pause between release edits")
    arguments = parser.parse_args()
    try:
        verify_main_ref(arguments.main_ref)
        releases = list_releases(arguments.repository)
        directories = {parsed.directory for release in releases
                       if (parsed := parse_connector_release_tag(release["tag_name"])) is not None}
        display_names = {directory: read_main_display_name(arguments.main_ref, directory)
                         for directory in sorted(directories)}
        retitles = plan_release_retitles(releases, display_names)
    except (OSError, ValueError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        print(error, file=sys.stderr)
        return 1
    print_retitle_table(retitles, arguments.main_ref)
    pending = [retitle for retitle in retitles if retitle.is_change_required]
    print(f"{len(pending)} of {len(retitles)} connector releases need a new title", file=sys.stderr)
    if not arguments.apply:
        return 0
    for index, retitle in enumerate(pending):
        if index:
            time.sleep(arguments.delay_seconds)
        result = subprocess.run(
            ["gh", "release", "edit", retitle.tag, "--repo", arguments.repository, "--title", retitle.new_title],
            capture_output=True, text=True,
        )
        if result.returncode != 0:
            print(f"stopped at {retitle.tag}: {result.stderr.strip()}", file=sys.stderr)
            return 1
        print(f"retitled {retitle.tag}: {retitle.new_title}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
