# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""An in-process fake of the Drive changes and files APIs for the file Trigger tests.

Like Drive, and like the Go connector's drivechangetest, it reports only each file's latest change after
a page token.
"""

import json
import threading
from dataclasses import dataclass, replace
from datetime import UTC, datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any
from urllib.parse import parse_qs, urlsplit

_ZERO_TIME = datetime.min.replace(tzinfo=UTC)


def drive_now() -> datetime:
    """Return the current time at Drive's millisecond precision."""
    now = datetime.now(UTC)
    return now.replace(microsecond=now.microsecond // 1000 * 1000)


def drive_time_text(value: datetime) -> str:
    """Format a time as Drive does: UTC with milliseconds."""
    return value.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S.") + f"{value.microsecond // 1000:03d}Z"


@dataclass(frozen=True, slots=True)
class DriveFile:
    """The fake's state of one Drive file."""

    id: str
    name: str
    mime_type: str
    parent: str
    created_time: datetime
    modified_time: datetime
    drive_id: str = ""
    """The shared drive ID, or blank for a My Drive file."""
    is_trashed: bool = False
    trashed_time: datetime = _ZERO_TIME


@dataclass(frozen=True, slots=True)
class ScriptedResponse:
    """One failure the fake returns to the next changes.list request."""

    status: int
    retry_after: str = ""


class DriveChangeLog:
    """The fake Drive on a loopback HTTP server; safe for concurrent use. Close it with `close`."""

    def __init__(self, access_token: str) -> None:
        """Start a fake that accepts only access_token."""
        self._access_token = access_token
        self._lock = threading.Lock()
        self._files: dict[str, DriveFile] = {}
        self._changes: list[tuple[str, datetime]] = []
        self._scripted: list[ScriptedResponse] = []
        self._requests: dict[str, int] = {}
        self._list_pages = 0
        change_log = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, format: str, *args: object) -> None:
                pass  # Request lines would only repeat the test's own fixtures.

            def do_GET(self) -> None:
                status, headers, body = change_log._serve(self.path, self.headers.get("Authorization", ""))
                self.send_response(status)
                for name, value in headers.items():
                    self.send_header(name, value)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        self._server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()
        self.url = f"http://127.0.0.1:{self._server.server_address[1]}"

    def close(self) -> None:
        """Stop the server."""
        self._server.shutdown()
        self._server.server_close()

    def create_file(self, file_id: str, name: str, mime_type: str, parent: str) -> None:
        """Add a file and record its change."""
        now = drive_now()
        self.put(DriveFile(file_id, name, mime_type, parent, now, now))

    def put(self, file: DriveFile) -> None:
        """Store file as is, such as one with an older creation time, and record its change."""
        with self._lock:
            self._files[file.id] = file
            self._changes.append((file.id, drive_now()))

    def modify_file(self, file_id: str, modified_at: datetime) -> None:
        """Set a file's modified time and record its change."""
        self._edit(file_id, lambda file: replace(file, modified_time=modified_at))

    def trash_file(self, file_id: str) -> None:
        """Move a file to the trash; like Drive, set trashedTime only for a shared drive file."""
        self._edit(
            file_id,
            lambda file: replace(
                file, is_trashed=True, trashed_time=drive_now() if file.drive_id else file.trashed_time
            ),
        )

    def touch_file(self, file_id: str) -> None:
        """Record a change that leaves the file's times unchanged, such as a permission change."""
        self._edit(file_id, lambda file: file)

    def _edit(self, file_id: str, change: Any) -> None:
        with self._lock:
            self._files[file_id] = change(self._files[file_id])
            self._changes.append((file_id, drive_now()))

    def script(self, *responses: ScriptedResponse) -> None:
        """Queue failures for the next changes.list requests."""
        with self._lock:
            self._scripted.extend(responses)

    def requests(self, path: str) -> int:
        """Count the requests made to a path, such as /drive/v3/changes."""
        with self._lock:
            return self._requests.get(path, 0)

    def list_pages(self) -> int:
        """Count the successful changes.list pages served."""
        with self._lock:
            return self._list_pages

    def _serve(self, target: str, authorization: str) -> tuple[int, dict[str, str], bytes]:
        url = urlsplit(target)
        query = {name: values[0] for name, values in parse_qs(url.query).items()}
        with self._lock:
            self._requests[url.path] = self._requests.get(url.path, 0) + 1
            if authorization != f"Bearer {self._access_token}":
                return 401, {}, b""
            if url.path == "/drive/v3/changes/startPageToken":
                return _json({"startPageToken": str(len(self._changes) + 1)})
            if url.path == "/drive/v3/changes":
                if self._scripted:
                    scripted = self._scripted.pop(0)
                    headers = {"Retry-After": scripted.retry_after} if scripted.retry_after else {}
                    return scripted.status, headers, b""
                return self._serve_changes(query)
            if url.path.startswith("/drive/v3/files/"):
                file = self._files.get(url.path.removeprefix("/drive/v3/files/"))
                return (404, {}, b"") if file is None else _json(_file_resource(file))
            return 404, {}, b""

    def _serve_changes(self, query: dict[str, str]) -> tuple[int, dict[str, str], bytes]:
        """List each file's latest change at or after pageToken, oldest first."""
        try:
            start, page_size = int(query.get("pageToken", "")), int(query.get("pageSize", ""))
        except ValueError:
            return 400, {}, b""
        if start < 1 or start > len(self._changes) + 1 or page_size < 1:
            return 400, {}, b""
        latest = {file_id: index for index, (file_id, _) in enumerate(self._changes) if index >= start - 1}
        positions = sorted(latest.values())
        response: dict[str, Any] = {}
        if len(positions) > page_size:
            positions = positions[:page_size]
            response["nextPageToken"] = str(positions[-1] + 2)
        else:
            response["newStartPageToken"] = str(len(self._changes) + 1)
        response["changes"] = [
            {
                "changeType": "file",
                "fileId": self._changes[index][0],
                "time": drive_time_text(self._changes[index][1]),
                "file": _file_resource(self._files[self._changes[index][0]]),
            }
            for index in positions
        ]
        self._list_pages += 1
        return _json(response)


def _file_resource(file: DriveFile) -> dict[str, Any]:
    resource: dict[str, Any] = {
        "id": file.id,
        "name": file.name,
        "mimeType": file.mime_type,
        "parents": [file.parent],
        "createdTime": drive_time_text(file.created_time),
        "modifiedTime": drive_time_text(file.modified_time),
        "trashed": file.is_trashed,
        "size": "42",
    }
    if file.drive_id:
        resource["driveId"] = file.drive_id
        if file.trashed_time != _ZERO_TIME:
            resource["trashedTime"] = drive_time_text(file.trashed_time)
    return resource


def _json(value: object) -> tuple[int, dict[str, str], bytes]:
    return 200, {"Content-Type": "application/json"}, json.dumps(value).encode()
