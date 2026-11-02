# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Read GitHub release assets once per workflow stage with bounded rate-limit recovery."""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

REPOSITORY = "superdurable/dex-connectors-library"
MAXIMUM_RATE_LIMIT_WAIT_SECONDS = 1200


class GitHubNotFoundError(RuntimeError):
    """The requested GitHub API resource does not exist."""


def github_json(url: str, *, sleep=time.sleep, clock=time.time) -> tuple[object, str]:
    request = urllib.request.Request(url, headers={
        "Accept": "application/vnd.github+json", "User-Agent": "dex-connector-release-inventory",
    })
    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    if token:
        request.add_header("Authorization", f"Bearer {token}")
    deadline = clock() + MAXIMUM_RATE_LIMIT_WAIT_SECONDS
    attempts = 0
    while True:
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                return json.load(response), response.headers.get("Link", "")
        except urllib.error.HTTPError as error:
            if error.code == 404:
                raise GitHubNotFoundError(f"GitHub API resource not found: {url}") from error
            headers = error.headers or {}
            remaining = (headers.get("X-RateLimit-Remaining") or "").strip()
            reset = (headers.get("X-RateLimit-Reset") or "").strip()
            retry_after = (headers.get("Retry-After") or "").strip()
            body = error.read(65536).decode("utf-8", errors="replace")
            is_limited = error.code == 429 or (error.code == 403 and (
                remaining == "0" or retry_after.isdigit() or "rate limit" in body.lower()
            ))
            if is_limited:
                wait = max(1.0, float(retry_after) if retry_after.isdigit() else
                           float(reset) - clock() + 2 if reset.isdigit() else 60.0)
                if clock() + wait > deadline:
                    raise RuntimeError(f"GitHub API rate limit (HTTP {error.code}); reset in {int(wait)} s "
                                       f"exceeds the {MAXIMUM_RATE_LIMIT_WAIT_SECONDS} s wait budget") from error
                print(f"GitHub API rate limit (HTTP {error.code}); waiting {int(wait)} s for reset",
                      file=sys.stderr, flush=True)
                sleep(wait)
                continue
            if error.code in (500, 502, 503, 504) and attempts < 4:
                attempts += 1
                sleep(2 ** attempts)
                continue
            raise RuntimeError(f"GitHub API request failed: HTTP {error.code}: {body[:200]}") from error


def release_inventory(*, fetch=github_json) -> dict[str, list[str]]:
    url = f"https://api.github.com/repos/{REPOSITORY}/releases?per_page=100"
    inventory: dict[str, list[str]] = {}
    while url:
        response, links = fetch(url)
        if not isinstance(response, list):
            raise RuntimeError("GitHub release inventory returned an invalid page")
        for release in response:
            if not isinstance(release, dict) or not isinstance(release.get("tag_name"), str):
                raise RuntimeError("GitHub release inventory contains an invalid release")
            if release.get("draft") or release.get("prerelease"):
                continue
            inventory[release["tag_name"]] = [asset["name"] for asset in release.get("assets", [])]
        url = next_page_url(links)
    return inventory


def next_page_url(links: str) -> str:
    return next((part[part.index("<") + 1:part.index(">")] for part in links.split(",")
                 if 'rel="next"' in part and "<" in part and ">" in part), "")


def release_assets(tag: str, inventory_path: str | None = None, *, fetch=github_json) -> frozenset[str] | None:
    if inventory_path:
        inventory = json.loads(Path(inventory_path).read_text(encoding="utf-8"))
        if not isinstance(inventory, dict):
            raise RuntimeError("GitHub release inventory must be an object")
        if not inventory.get("_inventory_unavailable"):
            assets = inventory.get(tag)
            return frozenset(assets) if assets is not None else None
    url = f"https://api.github.com/repos/{REPOSITORY}/releases/tags/{urllib.parse.quote(tag, safe='')}"
    try:
        release, _ = fetch(url)
    except GitHubNotFoundError:
        return None
    if not isinstance(release, dict):
        raise RuntimeError(f"GitHub release lookup returned invalid data for {tag}")
    return frozenset(asset["name"] for asset in release.get("assets", []))


def main() -> None:
    parser = argparse.ArgumentParser()
    destination = parser.add_mutually_exclusive_group(required=True)
    destination.add_argument("--output", type=Path)
    destination.add_argument("--tag")
    parser.add_argument("--live", action="store_true", help="ignore a run-level inventory for a fresh release lookup")
    args = parser.parse_args()
    if args.tag:
        assets = release_assets(args.tag, None if args.live else os.environ.get("GH_RELEASE_INVENTORY_PATH"))
        print(json.dumps(sorted(assets) if assets is not None else None))
        return
    inventory = release_inventory()
    args.output.write_text(json.dumps(inventory, sort_keys=True) + "\n", encoding="utf-8")
    print(f"GitHub release inventory: {len(inventory)} releases", flush=True)


if __name__ == "__main__":
    main()
