# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dropbox operation validation and Go-compatible provider decisions."""

from __future__ import annotations

import base64
import binascii
import json
from collections.abc import Callable
from typing import TYPE_CHECKING, Any, TypeVar

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)

from ._generated import definitions as defs
from .client import Exchange
from .models import (
    ContinueFolderInput,
    DownloadedFile,
    DownloadFileInput,
    EntryMetadata,
    FolderPage,
    GetMetadataInput,
    ListFolderInput,
    UploadedFile,
    UploadFileInput,
)

if TYPE_CHECKING:
    from .client import Client

Output = TypeVar("Output")


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "dropbox", operation, message)


def _query_branch(
    operation: str,
    branch: str,
    empty: Output,
    kind: FailureKind,
    message: str,
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(branch, empty, Receipt(), _failure(operation, kind, message))


def _query_retry(operation: str, kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[Output]:
    return QueryAttempt.retry(_failure(operation, kind, message), delay)


def _mutation_branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[UploadedFile]:
    return MutationAttempt.branch_result(
        branch, UploadedFile(), Receipt(), _failure("uploadFile", kind, message)
    )


def _mutation_retry(kind: FailureKind, message: str, delay: float = 0) -> MutationAttempt[UploadedFile]:
    return MutationAttempt.retry(_failure("uploadFile", kind, message), delay)


def _valid_path(path: str, *, allow_root: bool) -> bool:
    if path == "":
        return allow_root
    try:
        if len(path.encode("utf-8")) > 4096:
            return False
    except UnicodeError:
        return False
    if any(ord(character) < 32 or ord(character) == 127 for character in path):
        return False
    return path.startswith(("/", "id:", "ns:", "rev:"))


def _field(value: dict[str, Any], key: str) -> str:
    result = value.get(key, "")
    if result is None:
        return ""
    if not isinstance(result, str):
        raise ValueError("Dropbox metadata field has an invalid type")
    return result


def _entry(raw: object, *, upload: bool = False) -> EntryMetadata:
    if not isinstance(raw, dict):
        raise ValueError("Dropbox entry is not an object")
    kind = "file" if upload else _field(raw, ".tag")
    if kind not in ("file", "folder"):
        raise ValueError("Dropbox entry kind is invalid")
    result = EntryMetadata(
        id=_field(raw, "id"),
        name=_field(raw, "name"),
        path_display=_field(raw, "path_display"),
        kind=kind,
    )
    if not result.id or not result.name or not result.path_display:
        raise ValueError("Dropbox entry identity is incomplete")
    return result


def _json_body(body: bytes) -> object:
    return json.loads(body)


def _folder_page(body: bytes) -> FolderPage:
    wire = _json_body(body)
    if not isinstance(wire, dict):
        raise ValueError("Dropbox folder page is not an object")
    entries = wire.get("entries")
    has_more = wire.get("has_more")
    cursor = _field(wire, "cursor")
    if not isinstance(entries, list) or not isinstance(has_more, bool) or not cursor:
        raise ValueError("Dropbox folder page is incomplete")
    return FolderPage(tuple(_entry(item) for item in entries), cursor, has_more)


def _entry_body(body: bytes) -> EntryMetadata:
    return _entry(_json_body(body))


def _error_summary(body: bytes) -> str:
    try:
        wire = _json_body(body)
        return _field(wire, "error_summary") if isinstance(wire, dict) else ""
    except (ValueError, UnicodeError):
        return ""


def _query_result(
    operation: str,
    exchange: Exchange,
    empty: Output,
    happy: str,
    decode: Callable[[bytes], Output],
    *,
    not_found: str = "",
    cursor_expired: str = "",
) -> QueryAttempt[Output]:
    if exchange.error == "credential":
        return _query_branch(
            operation,
            defs.DEFECT,
            empty,
            FailureKind.AUTHENTICATION,
            "connection credentials are unavailable or invalid",
        )
    if exchange.error == "refresh":
        return _query_retry(operation, FailureKind.AVAILABILITY, "Dropbox credentials could not be refreshed")
    if exchange.error == "request":
        return _query_retry(operation, FailureKind.TRANSPORT, "Dropbox request failed")
    if exchange.status == 429:
        return _query_retry(
            operation, FailureKind.RATE_LIMIT, "Dropbox rate limited the request", exchange.retry_after
        )
    if exchange.status == 408 or exchange.status >= 500:
        return _query_retry(
            operation, FailureKind.AVAILABILITY, "Dropbox is temporarily unavailable", exchange.retry_after
        )
    if exchange.status == 409:
        summary = _error_summary(exchange.body)
        if not_found and summary.startswith("path/not_found"):
            return _query_branch(
                operation, not_found, empty, FailureKind.NOT_FOUND, "Dropbox did not find the resource"
            )
        if cursor_expired and summary.startswith("reset/"):
            return _query_branch(
                operation,
                cursor_expired,
                empty,
                FailureKind.PROVIDER_REJECTION,
                "Dropbox folder cursor expired",
            )
    if exchange.status != 200:
        return _query_branch(
            operation,
            defs.PROVIDER_REJECTED,
            empty,
            FailureKind.PROVIDER_REJECTION,
            f"Dropbox rejected HTTP {exchange.status}",
        )
    if exchange.error == "too_large":
        return _query_branch(
            operation,
            defs.INVALID_RESPONSE,
            empty,
            FailureKind.RESPONSE_TOO_LARGE,
            "Dropbox response exceeded the configured limit",
        )
    if exchange.error:
        return _query_retry(operation, FailureKind.TRANSPORT, "Dropbox response was interrupted")
    try:
        result = decode(exchange.body)
    except (ValueError, TypeError, UnicodeError):
        return _query_branch(
            operation, defs.INVALID_RESPONSE, empty, FailureKind.PROTOCOL, "Dropbox returned invalid JSON"
        )
    return QueryAttempt.branch_result(happy, result, Receipt())


class ListFolderOperation:
    """List one bounded page of direct child entries."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated folder Query contract."""
        return defs.LIST_FOLDER_DEFINITION

    async def invoke(self, call: Call, input: ListFolderInput) -> QueryAttempt[FolderPage]:
        """Validate a folder path and list one provider page."""
        if not _valid_path(input.path, allow_root=True) or not 1 <= input.limit <= 2000:
            return _query_branch(
                "listFolder",
                defs.DEFECT,
                FolderPage(),
                FailureKind.VALIDATION,
                "folder path or page limit is invalid",
            )
        exchange = await self.client.post_json(
            call, "/2/files/list_folder", {"path": input.path, "limit": input.limit}
        )
        return _query_result(
            "listFolder", exchange, FolderPage(), defs.LISTED, _folder_page, not_found=defs.NOT_FOUND
        )


class GetMetadataOperation:
    """Read one Dropbox file or folder identity."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated metadata Query contract."""
        return defs.GET_METADATA_DEFINITION

    async def invoke(self, call: Call, input: GetMetadataInput) -> QueryAttempt[EntryMetadata]:
        """Validate a path or stable identifier and read its metadata."""
        if not _valid_path(input.path, allow_root=False):
            return _query_branch(
                "getMetadata",
                defs.DEFECT,
                EntryMetadata(),
                FailureKind.VALIDATION,
                "file or folder path is invalid",
            )
        exchange = await self.client.post_json(call, "/2/files/get_metadata", {"path": input.path})
        return _query_result(
            "getMetadata", exchange, EntryMetadata(), defs.FOUND, _entry_body, not_found=defs.NOT_FOUND
        )


class ContinueFolderOperation:
    """Read a folder continuation page or later change set."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated continuation Query contract."""
        return defs.CONTINUE_FOLDER_DEFINITION

    async def invoke(self, call: Call, input: ContinueFolderInput) -> QueryAttempt[FolderPage]:
        """Validate and send a Dropbox folder cursor."""
        try:
            is_valid = bool(input.cursor) and len(input.cursor.encode()) <= 4096
        except UnicodeError:
            is_valid = False
        if not is_valid or any(character in input.cursor for character in "\r\n\x00"):
            return _query_branch(
                "continueFolder",
                defs.DEFECT,
                FolderPage(),
                FailureKind.VALIDATION,
                "folder cursor is invalid",
            )
        exchange = await self.client.post_json(
            call, "/2/files/list_folder/continue", {"cursor": input.cursor}
        )
        return _query_result(
            "continueFolder",
            exchange,
            FolderPage(),
            defs.LISTED,
            _folder_page,
            cursor_expired=defs.CURSOR_EXPIRED,
        )


class DownloadFileOperation:
    """Read bounded file bytes with provider file identity."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated download Query contract."""
        return defs.DOWNLOAD_FILE_DEFINITION

    async def invoke(self, call: Call, input: DownloadFileInput) -> QueryAttempt[DownloadedFile]:
        """Download one file and return Base64 content."""
        empty = DownloadedFile()
        if not _valid_path(input.path, allow_root=False):
            return _query_branch(
                "downloadFile", defs.DEFECT, empty, FailureKind.VALIDATION, "file path is invalid"
            )
        exchange = await self.client.download(call, input.path)
        if exchange.error == "credential":
            return _query_branch(
                "downloadFile",
                defs.DEFECT,
                empty,
                FailureKind.AUTHENTICATION,
                "connection credentials are unavailable or invalid",
            )
        if exchange.error == "refresh":
            return _query_retry(
                "downloadFile", FailureKind.AVAILABILITY, "Dropbox credentials could not be refreshed"
            )
        if exchange.error == "request":
            return _query_retry("downloadFile", FailureKind.TRANSPORT, "Dropbox download request failed")
        if exchange.status == 429:
            return _query_retry(
                "downloadFile",
                FailureKind.RATE_LIMIT,
                "Dropbox rate limited the download",
                exchange.retry_after,
            )
        if exchange.status == 408 or exchange.status >= 500:
            return _query_retry(
                "downloadFile",
                FailureKind.AVAILABILITY,
                "Dropbox download is temporarily unavailable",
                exchange.retry_after,
            )
        if exchange.status == 409 and _error_summary(exchange.body).startswith("path/not_found"):
            return _query_branch(
                "downloadFile", defs.NOT_FOUND, empty, FailureKind.NOT_FOUND, "Dropbox did not find the file"
            )
        if exchange.status != 200:
            return _query_branch(
                "downloadFile",
                defs.PROVIDER_REJECTED,
                empty,
                FailureKind.PROVIDER_REJECTION,
                f"Dropbox rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _query_branch(
                "downloadFile",
                defs.TOO_LARGE,
                empty,
                FailureKind.RESPONSE_TOO_LARGE,
                "Dropbox file exceeds the configured download limit",
            )
        if exchange.error:
            return _query_retry(
                "downloadFile", FailureKind.TRANSPORT, "Dropbox download response was interrupted"
            )
        try:
            file = _entry(json.loads(exchange.metadata))
            if file.kind != "file":
                raise ValueError("Dropbox download metadata is not a file")
        except (ValueError, TypeError, UnicodeError):
            return _query_branch(
                "downloadFile",
                defs.INVALID_RESPONSE,
                empty,
                FailureKind.PROTOCOL,
                "Dropbox returned invalid file metadata",
            )
        output = DownloadedFile(file, base64.b64encode(exchange.body).decode(), len(exchange.body))
        return QueryAttempt.branch_result(defs.DOWNLOADED, output, Receipt())


class UploadFileOperation:
    """Replace a bounded Dropbox file with repeatable content."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest-generated upload Mutation contract."""
        return defs.UPLOAD_FILE_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: UploadFileInput) -> IdempotencyKey:
        """Record stable call identity while Dropbox deduplicates by absolute replacement."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UploadFileInput) -> MutationAttempt[UploadedFile]:
        """Send the same bytes to the same path on every safe retry."""
        if not input.path.startswith("/") or not _valid_path(input.path, allow_root=False):
            return _mutation_branch(defs.DEFECT, FailureKind.VALIDATION, "upload path must be absolute")
        if len(input.content_base64) * 3 // 4 > self.client.max_upload_bytes:
            return _mutation_branch(
                defs.DEFECT, FailureKind.VALIDATION, "upload exceeds the configured byte limit"
            )
        try:
            content = base64.b64decode(input.content_base64, validate=True)
        except (ValueError, binascii.Error):
            return _mutation_branch(
                defs.DEFECT, FailureKind.VALIDATION, "upload content must be bounded Base64"
            )
        if len(content) > self.client.max_upload_bytes:
            return _mutation_branch(
                defs.DEFECT, FailureKind.VALIDATION, "upload content must be bounded Base64"
            )
        exchange = await self.client.upload(call, input.path, content)
        if exchange.error == "credential":
            return _mutation_branch(
                defs.DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        if exchange.error == "refresh":
            return _mutation_retry(FailureKind.AVAILABILITY, "Dropbox credentials could not be refreshed")
        if exchange.error == "request":
            return _mutation_retry(FailureKind.TRANSPORT, "Dropbox upload request failed")
        if exchange.status == 429:
            return _mutation_retry(
                FailureKind.RATE_LIMIT, "Dropbox rate limited the upload", exchange.retry_after
            )
        if exchange.status == 408 or exchange.status >= 500:
            return _mutation_retry(
                FailureKind.AVAILABILITY, "Dropbox upload is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return _mutation_branch(
                defs.PROVIDER_REJECTED,
                FailureKind.PROVIDER_REJECTION,
                f"Dropbox rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _mutation_branch(
                defs.INVALID_RESPONSE,
                FailureKind.RESPONSE_TOO_LARGE,
                "Dropbox upload metadata exceeded the configured limit",
            )
        if exchange.error:
            return _mutation_retry(FailureKind.TRANSPORT, "Dropbox upload response was interrupted")
        try:
            file = _entry(_json_body(exchange.body), upload=True)
        except (ValueError, TypeError, UnicodeError):
            return _mutation_branch(
                defs.INVALID_RESPONSE, FailureKind.PROTOCOL, "Dropbox returned invalid upload metadata"
            )
        return MutationAttempt.branch_result(defs.UPLOADED, UploadedFile(file), Receipt())
