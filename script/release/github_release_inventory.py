# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Read GitHub release assets once per workflow stage with bounded rate-limit recovery."""

from __future__ import annotations

import argparse
import http.client
import json
import os
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

REPOSITORY = "superdurable/dex-connectors-library"
GRAPHQL_URL = "https://api.github.com/graphql"
# One page costs one GraphQL point: 100 releases plus up to 100 assets each.
RELEASE_INVENTORY_QUERY = """
query($owner: String!, $name: String!, $after: String) {
  repository(owner: $owner, name: $name) {
    releases(first: 100, after: $after, orderBy: {field: CREATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        tagName
        isDraft
        isPrerelease
        createdAt
        releaseAssets(first: 100) { pageInfo { hasNextPage } nodes { name } }
      }
    }
  }
}
"""
MAXIMUM_RATE_LIMIT_WAIT_SECONDS = 1200
# urlopen raises http.client.RemoteDisconnected unwrapped when a server drops the connection.
TRANSIENT_CONNECTION_ERRORS = (urllib.error.URLError, http.client.HTTPException, ConnectionError, TimeoutError)


def is_certificate_verification_failure(error: BaseException) -> bool:
    """Reports whether a connection error is a TLS certificate rejection, which retrying cannot fix."""
    return isinstance(error, ssl.SSLCertVerificationError) or isinstance(
        getattr(error, "reason", None), ssl.SSLCertVerificationError)


class GitHubNotFoundError(RuntimeError):
    """The requested GitHub API resource does not exist."""


def github_json(url: str, *, body: object | None = None, sleep=time.sleep, clock=time.time) -> tuple[object, str]:
    """GET url, or POST body as JSON when given, and return the decoded response and its Link header."""
    request = urllib.request.Request(url, headers={
        "Accept": "application/vnd.github+json", "User-Agent": "dex-connector-release-inventory",
    }, data=None if body is None else json.dumps(body).encode("utf-8"), method="GET" if body is None else "POST")
    if body is not None:
        request.add_header("Content-Type", "application/json")
    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    if token:
        request.add_header("Authorization", f"Bearer {token}")
    deadline = clock() + MAXIMUM_RATE_LIMIT_WAIT_SECONDS
    attempts = 0
    while True:
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                result, response_headers = json.load(response), response.headers
            if body is None or not is_graphql_rate_limited(result, response_headers):
                return result, response_headers.get("Link", "")
            # GraphQL reports a primary or secondary rate limit as HTTP 200 with errors.
            sleep_until_rate_limit_reset("GraphQL", response_headers, deadline, sleep=sleep, clock=clock)
            continue
        except urllib.error.HTTPError as error:
            if error.code == 404:
                raise GitHubNotFoundError(f"GitHub API resource not found: {url}") from error
            headers = error.headers or {}
            remaining = (headers.get("X-RateLimit-Remaining") or "").strip()
            retry_after = (headers.get("Retry-After") or "").strip()
            error_body = error.read(65536).decode("utf-8", errors="replace")
            is_limited = error.code == 429 or (error.code == 403 and (
                remaining == "0" or retry_after.isdigit() or "rate limit" in error_body.lower()
            ))
            if is_limited:
                sleep_until_rate_limit_reset(f"HTTP {error.code}", headers, deadline, sleep=sleep, clock=clock)
                continue
            if error.code in (500, 502, 503, 504) and attempts < 4:
                attempts += 1
                sleep(2 ** attempts)
                continue
            raise RuntimeError(f"GitHub API request failed: HTTP {error.code}: {error_body[:200]}") from error
        except TRANSIENT_CONNECTION_ERRORS as error:
            if attempts >= 4 or is_certificate_verification_failure(error):
                raise
            attempts += 1
            print(f"GitHub API connection failed ({error}); retrying {url}", file=sys.stderr, flush=True)
            sleep(2 ** attempts)


def is_graphql_rate_limited(result: object, headers) -> bool:
    """Report whether a GraphQL response with errors is a rate limit, which waiting can clear."""
    errors = result.get("errors") if isinstance(result, dict) else None
    if not errors:
        return False
    if any(isinstance(error, dict) and error.get("type") == "RATE_LIMITED" for error in errors):
        return True
    remaining = (headers.get("X-RateLimit-Remaining") or "").strip()
    retry_after = (headers.get("Retry-After") or "").strip()
    return remaining == "0" or retry_after.isdigit()


def sleep_until_rate_limit_reset(description: str, headers, deadline: float, *, sleep, clock) -> None:
    """Wait for the reported reset, or fail when it would exceed the request's wait budget."""
    reset = (headers.get("X-RateLimit-Reset") or "").strip()
    retry_after = (headers.get("Retry-After") or "").strip()
    wait = max(1.0, float(retry_after) if retry_after.isdigit() else
               float(reset) - clock() + 2 if reset.isdigit() else 60.0)
    if clock() + wait > deadline:
        raise RuntimeError(f"GitHub API rate limit ({description}); reset in {int(wait)} s "
                           f"exceeds the {MAXIMUM_RATE_LIMIT_WAIT_SECONDS} s wait budget")
    print(f"GitHub API rate limit ({description}); waiting {int(wait)} s for reset", file=sys.stderr, flush=True)
    sleep(wait)


def release_inventory(*, fetch=github_json) -> dict[str, list[str]]:
    return release_inventory_with_creation_times(fetch=fetch)[0]


def release_inventory_with_creation_times(*, fetch=github_json) -> tuple[dict[str, list[str]], dict[str, str]]:
    """Return published release asset names and GitHub release creation times, both keyed by tag.

    GraphQL cursor pagination reads every release; the REST release list stops after 1,000 results for some
    callers. A release with more assets than one page holds is read again by tag.
    """
    owner, name = REPOSITORY.split("/", 1)
    inventory: dict[str, list[str]] = {}
    created_at: dict[str, str] = {}
    cursor: str | None = None
    while True:
        response, _ = fetch(GRAPHQL_URL, body={"query": RELEASE_INVENTORY_QUERY,
                                               "variables": {"owner": owner, "name": name, "after": cursor}})
        releases = read_release_page(response)
        for release in releases["nodes"]:
            if not isinstance(release, dict):
                raise RuntimeError("GitHub release inventory contains an invalid release")
            if release.get("isDraft") or release.get("isPrerelease"):
                continue
            tag = release.get("tagName")
            assets = release.get("releaseAssets")
            if not isinstance(tag, str) or not tag or not isinstance(assets, dict) or not isinstance(assets.get("nodes"), list):
                raise RuntimeError("GitHub release inventory contains an invalid release")
            if (assets.get("pageInfo") or {}).get("hasNextPage"):
                complete_assets = release_assets(tag, fetch=fetch)
                if complete_assets is None:
                    raise RuntimeError(f"GitHub release {tag} disappeared while the inventory was read")
                inventory[tag] = sorted(complete_assets)
            else:
                inventory[tag] = [asset["name"] for asset in assets["nodes"]]
            if isinstance(release.get("createdAt"), str):
                created_at[tag] = release["createdAt"]
        page_info = releases["pageInfo"]
        if not page_info.get("hasNextPage"):
            return inventory, created_at
        cursor = page_info.get("endCursor")
        if not isinstance(cursor, str) or not cursor:
            raise RuntimeError("GitHub release inventory page has no end cursor")


def read_release_page(response: object) -> dict[str, object]:
    """Return the releases connection of one GraphQL response, failing on any GraphQL error."""
    if not isinstance(response, dict):
        raise RuntimeError("GitHub release inventory returned an invalid page")
    if response.get("errors"):
        messages = "; ".join(str(error.get("message", error)) if isinstance(error, dict) else str(error)
                             for error in response["errors"])
        raise RuntimeError(f"GitHub release inventory query failed: {messages[:300]}")
    repository = (response.get("data") or {}).get("repository")
    releases = repository.get("releases") if isinstance(repository, dict) else None
    if (not isinstance(releases, dict) or not isinstance(releases.get("nodes"), list)
            or not isinstance(releases.get("pageInfo"), dict)):
        raise RuntimeError("GitHub release inventory returned an invalid page")
    return releases


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
    parser.add_argument("--created-at-output", type=Path, help="also write GitHub release creation times by tag")
    args = parser.parse_args()
    if args.tag:
        assets = release_assets(args.tag, None if args.live else os.environ.get("GH_RELEASE_INVENTORY_PATH"))
        print(json.dumps(sorted(assets) if assets is not None else None))
        return
    inventory, created_at = release_inventory_with_creation_times()
    args.output.write_text(json.dumps(inventory, sort_keys=True) + "\n", encoding="utf-8")
    if args.created_at_output:
        args.created_at_output.write_text(json.dumps(created_at, sort_keys=True) + "\n", encoding="utf-8")
    print(f"GitHub release inventory: {len(inventory)} releases", flush=True)


if __name__ == "__main__":
    main()
