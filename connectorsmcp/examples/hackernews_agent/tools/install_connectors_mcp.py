# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Install a released dex-connectors-mcp from the published release index.

    python3 tools/install_connectors_mcp.py --output .bin/dex-connectors-mcp [--version 0.1.5]

The newest release is the default. The archive's SHA-256 must match the index.
"""

import argparse
import hashlib
import io
import json
import platform
import shutil
import sys
import tarfile
import time
import urllib.error
import urllib.request
from pathlib import Path

RELEASE_INDEX_URL = "https://connectors.superdurable.io/connectors-mcp/index.json"
RELEASE_INDEX_SCHEMA = "dex-connectors-mcp/release-index/v1"
BINARY_NAME = "dex-connectors-mcp"
DOWNLOAD_TIMEOUT_SECONDS = 120
DOWNLOAD_ATTEMPTS = 3
HTTP_TOO_MANY_REQUESTS = 429
MAXIMUM_RETRY_AFTER_SECONDS = 60.0


def main() -> int:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--output", type=Path, required=True, help="where to write the executable")
    parser.add_argument("--version", help="an exact release version; the newest when omitted")
    arguments = parser.parse_args()
    index = json.loads(download(RELEASE_INDEX_URL))
    if index.get("schemaVersion") != RELEASE_INDEX_SCHEMA:
        raise SystemExit(f"unexpected release index schema {index.get('schemaVersion')!r}")
    release = select_release(index["releases"], arguments.version)
    asset = release["assets"].get(current_platform())
    if asset is None:
        raise SystemExit(f"dex-connectors-mcp {release['version']} has no {current_platform()} asset")
    archive = download(asset["url"])
    digest = hashlib.sha256(archive).hexdigest()
    if digest != asset["sha256"]:
        raise SystemExit(f"{asset['url']} has SHA-256 {digest}, but the index lists {asset['sha256']}")
    extract_binary(archive, arguments.output)
    print(f"installed dex-connectors-mcp {release['version']} ({digest}) at {arguments.output}")
    return 0


def select_release(releases: list[dict[str, object]], version: str | None) -> dict:
    if not releases:
        raise SystemExit("the release index lists no release")
    if version is None:
        return max(releases, key=lambda release: version_key(str(release["version"])))
    for release in releases:
        if release["version"] == version:
            return release
    raise SystemExit(f"the release index has no dex-connectors-mcp {version}")


def version_key(version: str) -> tuple[int, ...]:
    return tuple(int(part) for part in version.split("."))


def current_platform() -> str:
    machine = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(
        platform.machine().lower()
    )
    system = {"darwin": "darwin", "linux": "linux"}.get(sys.platform)
    if machine is None or system is None:
        raise SystemExit(f"no dex-connectors-mcp release for {sys.platform}/{platform.machine()}")
    return f"{system}-{machine}"


def download(url: str) -> bytes:
    request = urllib.request.Request(url, headers={"User-Agent": "dex-connectors-mcp-example-installer"})
    for attempt in range(1, DOWNLOAD_ATTEMPTS + 1):
        backoff_seconds = float(2**attempt)
        try:
            with urllib.request.urlopen(request, timeout=DOWNLOAD_TIMEOUT_SECONDS) as response:
                return response.read()
        except urllib.error.HTTPError as error:
            is_retryable = error.code == HTTP_TOO_MANY_REQUESTS or error.code >= 500
            if not is_retryable or attempt == DOWNLOAD_ATTEMPTS:
                raise
            backoff_seconds = retry_after_seconds(error.headers.get("Retry-After"), backoff_seconds)
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            if attempt == DOWNLOAD_ATTEMPTS:
                raise
        # A short backoff rides out a transient network, CDN, or rate-limit failure.
        time.sleep(backoff_seconds)
    raise AssertionError("unreachable")


def retry_after_seconds(retry_after: str | None, default_seconds: float) -> float:
    """Return a delta-seconds Retry-After, capped; an absent or HTTP-date value keeps the default."""
    try:
        seconds = float(retry_after) if retry_after is not None else default_seconds
    except ValueError:
        return default_seconds
    return min(max(seconds, 0.0), MAXIMUM_RETRY_AFTER_SECONDS)


def extract_binary(archive: bytes, output: Path) -> None:
    with tarfile.open(fileobj=io.BytesIO(archive), mode="r:gz") as tar:
        members = [
            member for member in tar.getmembers() if member.isfile() and Path(member.name).name == BINARY_NAME
        ]
        if len(members) != 1:
            raise SystemExit(f"the archive holds {len(members)} {BINARY_NAME} executables, not one")
        source = tar.extractfile(members[0])
        if source is None:
            raise SystemExit(f"cannot read {members[0].name} from the archive")
        output.parent.mkdir(parents=True, exist_ok=True)
        temporary = output.with_name(f".{output.name}.tmp")
        with source, temporary.open("wb") as destination:
            shutil.copyfileobj(source, destination)
        temporary.chmod(0o755)
        temporary.replace(output)


if __name__ == "__main__":
    sys.exit(main())
