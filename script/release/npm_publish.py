# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Publish npm packages and check the scoped CI token without exposing credentials."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timedelta, timezone
from pathlib import Path

ISSUE_TITLE = "npm: NPM_TOKEN needs rotation"
ROTATION_URL = "https://github.com/superdurable/dex-connectors-library/blob/main/docs/versioning-and-releases.md#npm-token-rotation"
AUTH_ERRORS = ("E401", "ENEEDAUTH", "oidc", "trusted publisher", "invalid token", "token expired", "authentication failed")


def npm(*arguments: str, token: str = "") -> subprocess.CompletedProcess[str]:
    environment = dict(os.environ)
    if not token:
        environment.pop("NODE_AUTH_TOKEN", None)
        return subprocess.run(("npm", *arguments), env=environment, capture_output=True, text=True, check=False)
    with tempfile.TemporaryDirectory() as temporary:
        config = Path(temporary) / "npmrc"
        config.write_text("//registry.npmjs.org/:_authToken=${NODE_AUTH_TOKEN}\n", encoding="utf-8")
        environment["NODE_AUTH_TOKEN"] = token
        environment["NPM_CONFIG_USERCONFIG"] = str(config)
        return subprocess.run(("npm", *arguments), env=environment, capture_output=True, text=True, check=False)


def is_auth_error(result: subprocess.CompletedProcess[str]) -> bool:
    if "cannot publish over" in result.stderr.lower() or "previously published" in result.stderr.lower():
        return False
    return any(marker.lower() in result.stderr.lower() for marker in AUTH_ERRORS)


def print_npm_failure(result: subprocess.CompletedProcess[str]) -> None:
    token = os.environ.get("NPM_TOKEN", "")
    message = result.stderr.replace(token, "[redacted]") if token else result.stderr
    if message:
        print(message, file=sys.stderr)


def report_token_issue(reason: str, expiry: str = "") -> None:
    if not os.environ.get("GH_TOKEN"):
        return
    body = f"@longquanzheng NPM_TOKEN needs rotation: {reason}.\n\nExpiry: {expiry or 'unknown'}.\n\nSteps: {ROTATION_URL}\n"
    label = subprocess.run(("gh", "label", "create", "area:release", "--color", "0e8a16", "--force"),
                           capture_output=True, text=True, check=False)
    if label.returncode != 0:
        raise RuntimeError("cannot ensure npm token rotation issue label")
    existing = subprocess.run(("gh", "issue", "list", "--state", "open", "--search", f"{ISSUE_TITLE} in:title",
                               "--json", "number,title", "--limit", "100"),
                              capture_output=True, text=True, check=False)
    if existing.returncode != 0:
        raise RuntimeError("cannot inspect open issues for npm token rotation")
    for issue in json.loads(existing.stdout):
        if issue["title"] == ISSUE_TITLE:
            label_result = subprocess.run(("gh", "issue", "edit", str(issue["number"]), "--add-label", "area:release"),
                                          capture_output=True, text=True, check=False)
            if label_result.returncode != 0:
                raise RuntimeError("cannot label npm token rotation issue")
            result = subprocess.run(("gh", "issue", "comment", str(issue["number"]), "--body", body),
                                    capture_output=True, text=True, check=False)
            if result.returncode != 0:
                raise RuntimeError("cannot comment on npm token rotation issue")
            return
    result = subprocess.run(("gh", "issue", "create", "--title", ISSUE_TITLE, "--label", "area:release", "--body", body),
                            capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise RuntimeError("cannot create npm token rotation issue")


def publish(package: str, tarball: str, dist_tag: str = "latest") -> None:
    tarball_integrity = "sha512-" + base64.b64encode(hashlib.sha512(Path(tarball).read_bytes()).digest()).decode("ascii")
    tarball_name, package_version = package_identity_from_tarball(Path(tarball))
    if tarball_name != package:
        raise RuntimeError(f"npm tarball package {tarball_name} differs from planned {package}")
    existing_version = npm("view", f"{package}@{package_version}", "dist.integrity", "--json")
    if existing_version.returncode == 0:
        try:
            published_integrity = json.loads(existing_version.stdout)
        except json.JSONDecodeError:
            published_integrity = existing_version.stdout.strip()
        if published_integrity == tarball_integrity:
            print(f"{package}@{package_version} already published with identical integrity")
            return
        raise RuntimeError(f"{package}@{package_version} already exists with different integrity")
    if "E404" not in existing_version.stderr:
        print_npm_failure(existing_version)
        raise RuntimeError("npm version lookup failed before publication")
    found = npm("view", package, "version", "--json")
    if found.returncode == 0 and dist_tag == "latest":
        published = npm("publish", tarball, "--provenance", "--access", "public", "--tag", dist_tag)
        if published.returncode == 0:
            return
        print_npm_failure(published)
        if not is_auth_error(published):
            raise RuntimeError("npm publish failed with a non-authentication error; inspect npm logs")
        print("::warning::Trusted publisher is unavailable; configure npm trust for this workflow.", file=sys.stderr)
    elif found.returncode != 0 and "E404" not in found.stderr:
        print_npm_failure(found)
        raise RuntimeError("npm package lookup failed before publication")
    elif found.returncode != 0:
        if dist_tag != "latest":
            raise RuntimeError("npm pre-release requires an existing package; publish the placeholder or a stable release first")
        print("Creating the first npm package release with NPM_TOKEN.")
    token = os.environ.get("NPM_TOKEN", "")
    if not token:
        report_token_issue("the CI token is missing")
        raise RuntimeError("NPM_TOKEN is required for the first publish or trusted publisher fallback")
    publish_arguments = ("publish", tarball, "--provenance", "--access", "public", "--tag", dist_tag) if dist_tag == "latest" else ("publish", tarball, "--access", "public", "--tag", dist_tag)
    published = npm(*publish_arguments, token=token)
    if published.returncode != 0:
        print_npm_failure(published)
        if is_auth_error(published):
            report_token_issue("npm rejected the token during a release publish")
            raise RuntimeError("npm rejected NPM_TOKEN; rotate it using the linked procedure")
        raise RuntimeError("npm publish failed with a non-authentication error; inspect npm logs")


def package_identity_from_tarball(tarball: Path) -> tuple[str, str]:
    import tarfile

    with tarfile.open(tarball, "r:gz") as archive:
        member = archive.extractfile("package/package.json")
        if member is None:
            raise RuntimeError("npm tarball has no package/package.json")
        identity = json.load(member)
        return str(identity["name"]), str(identity["version"])


def check_token() -> None:
    token = os.environ.get("NPM_TOKEN", "")
    if not token:
        report_token_issue("the CI token is missing")
        raise RuntimeError("NPM_TOKEN is missing")
    identity = npm("whoami", "--scope", "@superdurable", token=token)
    if identity.returncode != 0:
        report_token_issue("npm whoami rejected the CI token")
        raise RuntimeError("NPM_TOKEN is invalid or expired; rotate it")
    expiry = os.environ.get("NPM_TOKEN_EXPIRES_AT", "")
    if expiry:
        check_token_expiry(expiry)
        return
    tokens = npm("token", "list", "--json", token=token)
    if tokens.returncode != 0:
        unknown_token_expiry()
        return
    try:
        entries = json.loads(tokens.stdout)
        if isinstance(entries, dict):
            entries = entries.get("tokens", [])
        if not isinstance(entries, list):
            unknown_token_expiry()
            return
        matches = [entry for entry in entries if token_entry_matches(entry, token)]
        expiry = (matches[0].get("expires") or matches[0].get("expiration")) if len(matches) == 1 else ""
        if not expiry:
            unknown_token_expiry()
            return
        check_token_expiry(expiry)
    except (ValueError, TypeError, AttributeError, KeyError):
        unknown_token_expiry()


def check_token_expiry(expiry: str) -> None:
    try:
        expiry_time = datetime.fromisoformat(expiry.replace("Z", "+00:00"))
        if expiry_time.tzinfo is None:
            expiry_time = expiry_time.replace(tzinfo=timezone.utc)
    except ValueError:
        unknown_token_expiry()
        return
    if expiry_time <= datetime.now(timezone.utc) + timedelta(days=14):
        report_token_issue("the token expires within 14 days", expiry)
        raise RuntimeError(f"NPM_TOKEN expires within 14 days ({expiry}); rotate it")


def token_entry_matches(entry: dict[str, object], token: str) -> bool:
    for key in ("key", "token"):
        prefix = str(entry.get(key) or "").removesuffix("...").removesuffix("…")
        if prefix and token.startswith(prefix):
            return True
    return False


def unknown_token_expiry() -> None:
    print("::warning::Cannot determine NPM_TOKEN expiry; set repository variable NPM_TOKEN_EXPIRES_AT.", file=sys.stderr)
    report_token_issue("expiry is unknown; set repository variable NPM_TOKEN_EXPIRES_AT")


def main() -> int:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    publish_parser = subparsers.add_parser("publish")
    publish_parser.add_argument("--package", required=True)
    publish_parser.add_argument("--tarball", required=True)
    publish_parser.add_argument("--dist-tag", default="latest")
    subparsers.add_parser("check-token")
    arguments = parser.parse_args()
    try:
        if arguments.command == "publish":
            publish(arguments.package, arguments.tarball, arguments.dist_tag)
        else:
            check_token()
    except RuntimeError as error:
        print(error, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
