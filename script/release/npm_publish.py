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
import tarfile
import tempfile
from datetime import datetime, timedelta, timezone
from pathlib import Path

from component_release import npm_tarball_identity

ISSUE_TITLE = "npm: NPM_TOKEN needs rotation"
ROTATION_URL = "https://github.com/superdurable/dex-connectors-library/blob/main/docs/versioning-and-releases.md#npm-token-rotation"
AUTH_ERRORS = ("E401", "ENEEDAUTH", "EOTP", "invalid npm token", "token expired")


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
    labels = subprocess.run(("gh", "label", "list", "--search", "area:release", "--json", "name", "--limit", "100"),
                            capture_output=True, text=True, check=False)
    if labels.returncode != 0:
        raise RuntimeError("cannot inspect npm token rotation issue label")
    if not any(entry.get("name") == "area:release" for entry in json.loads(labels.stdout)):
        label = subprocess.run(("gh", "label", "create", "area:release", "--color", "0e8a16"),
                               capture_output=True, text=True, check=False)
        if label.returncode != 0:
            raise RuntimeError("cannot create npm token rotation issue label")
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


def publish(package: str, tarball: str, version: str, dist_tag: str = "latest") -> None:
    tarball_integrity = "sha512-" + base64.b64encode(hashlib.sha512(Path(tarball).read_bytes()).digest()).decode("ascii")
    tarball_name, package_version = package_identity_from_tarball(Path(tarball))
    expected_version = version.removeprefix("v")
    if tarball_name != package or package_version != expected_version:
        raise RuntimeError(f"npm tarball identity {tarball_name}@{package_version} differs from planned {package}@{expected_version}")
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
        publish_arguments = ("publish", tarball, "--provenance", "--access", "public", "--tag", dist_tag)
        verify_npm_dry_run(publish_arguments, package, expected_version)
        published = npm(*publish_arguments)
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
    verify_npm_dry_run(publish_arguments, package, expected_version)
    published = npm(*publish_arguments, token=token)
    if published.returncode != 0:
        print_npm_failure(published)
        if is_auth_error(published):
            report_token_issue("npm rejected the token during a release publish")
            raise RuntimeError("npm rejected NPM_TOKEN; rotate it using the linked procedure")
        raise RuntimeError("npm publish failed with a non-authentication error; inspect npm logs")


def package_identity_from_tarball(tarball: Path) -> tuple[str, str]:
    try:
        return npm_tarball_identity(tarball)
    except (ValueError, OSError, tarfile.TarError) as error:
        raise RuntimeError(f"invalid npm tarball: {error}") from error


def verify_npm_dry_run(publish_arguments: tuple[str, ...], package: str, version: str) -> None:
    result = npm(*publish_arguments, "--dry-run", "--json")
    if result.returncode != 0:
        print_npm_failure(result)
        raise RuntimeError("npm dry-run failed before publication")
    try:
        reported = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise RuntimeError("npm dry-run returned invalid JSON") from error
    if isinstance(reported, dict) and len(reported) == 1 and package in reported:
        reported = reported[package]
    if not isinstance(reported, dict) or reported.get("name") != package or reported.get("version") != version:
        raise RuntimeError("npm dry-run package identity differs from the release plan")


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
    publish_parser.add_argument("--version", required=True)
    publish_parser.add_argument("--tarball", required=True)
    publish_parser.add_argument("--dist-tag", default="latest")
    subparsers.add_parser("check-token")
    arguments = parser.parse_args()
    try:
        if arguments.command == "publish":
            publish(arguments.package, arguments.tarball, arguments.version, arguments.dist_tag)
        else:
            check_token()
    except RuntimeError as error:
        print(error, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
