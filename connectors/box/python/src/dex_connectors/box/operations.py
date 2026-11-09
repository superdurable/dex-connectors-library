# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Box file operations with Go-compatible branch and retry decisions."""

from __future__ import annotations

import base64
import binascii
import hashlib
import json
from dataclasses import dataclass
from typing import TYPE_CHECKING, Any

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
from .models import DownloadedFile, DownloadFileInput, UploadedFile, UploadFileInput

if TYPE_CHECKING:
    from .client import Client


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "box", operation, message)


def _query_branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[DownloadedFile]:
    return QueryAttempt.branch_result(
        branch, DownloadedFile(), Receipt(), _failure("downloadFile", kind, message)
    )


def _query_retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[DownloadedFile]:
    return QueryAttempt.retry(_failure("downloadFile", kind, message), delay)


def _mutation_branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[UploadedFile]:
    return MutationAttempt.branch_result(
        branch, UploadedFile(), Receipt(), _failure("uploadFile", kind, message)
    )


def _mutation_retry(kind: FailureKind, message: str, delay: float = 0) -> MutationAttempt[UploadedFile]:
    return MutationAttempt.retry(_failure("uploadFile", kind, message), delay)


def _valid_file_id(value: str) -> bool:
    return bool(value) and len(value) <= 40 and all("0" <= character <= "9" for character in value)


@dataclass(frozen=True, slots=True)
class _FileMetadata:
    file_id: str
    kind: str
    name: str
    sha1: str
    size: int

    def matches(self, file_id: str, digest: str, size: int) -> bool:
        return (
            self.file_id == file_id
            and self.kind == "file"
            and self.name != ""
            and self.sha1.upper() == digest
            and self.size == size
        )


def _decode_metadata(body: bytes, file_id: str) -> _FileMetadata:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict):
        raise ValueError("Box file metadata is not an object")
    identity = raw.get("id")
    kind = raw.get("type")
    name = raw.get("name")
    digest = raw.get("sha1")
    size = raw.get("size")
    if (
        identity != file_id
        or kind != "file"
        or not isinstance(name, str)
        or not name
        or not isinstance(digest, str)
        or len(digest) != 40
        or not all(character in "0123456789abcdefABCDEF" for character in digest)
        or type(size) is not int
        or size < 0
    ):
        raise ValueError("Box file metadata is incomplete")
    return _FileMetadata(identity, kind, name, digest, size)


def _uploaded(metadata: _FileMetadata) -> MutationAttempt[UploadedFile]:
    return MutationAttempt.branch_result(
        defs.UPLOADED, UploadedFile(metadata.file_id, metadata.name, metadata.sha1.upper()), Receipt()
    )


class DownloadFileOperation:
    """Read bounded Box file content by numeric ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated download Query contract."""
        return defs.DOWNLOAD_FILE_DEFINITION

    async def invoke(self, call: Call, input: DownloadFileInput) -> QueryAttempt[DownloadedFile]:
        """Download bytes, classify provider responses, and encode bounded content."""
        if not _valid_file_id(input.file_id):
            return _query_branch(
                defs.DEFECT, FailureKind.VALIDATION, "Box file ID must contain only decimal digits"
            )
        exchange = await self.client.download(call, input.file_id)
        if exchange.error == "credential":
            return _query_branch(
                defs.DEFECT, FailureKind.AUTHENTICATION, "Box credentials are unavailable or invalid"
            )
        if exchange.error == "refresh":
            return _query_retry(FailureKind.AVAILABILITY, "Box credentials could not be refreshed")
        if exchange.error == "request":
            return _query_retry(FailureKind.TRANSPORT, "Box download request failed")
        if exchange.status == 429:
            return _query_retry(FailureKind.RATE_LIMIT, "Box rate limited the download", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _query_retry(
                FailureKind.AVAILABILITY, "Box download is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 202:
            return _query_retry(
                FailureKind.AVAILABILITY, "Box file is not ready for download", exchange.retry_after
            )
        if exchange.error == "invalid_redirect":
            return _query_branch(
                defs.INVALID_RESPONSE, FailureKind.PROTOCOL, "Box returned an unsafe download redirect"
            )
        if exchange.status != 200 and exchange.error == "too_large":
            return _query_branch(
                defs.INVALID_RESPONSE,
                FailureKind.RESPONSE_TOO_LARGE,
                "Box error response exceeds the configured limit",
            )
        if exchange.status == 404:
            return _query_branch(defs.NOT_FOUND, FailureKind.NOT_FOUND, "Box did not find the file")
        if exchange.status != 200:
            return _query_branch(
                defs.PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, f"Box rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _query_branch(
                defs.TOO_LARGE,
                FailureKind.RESPONSE_TOO_LARGE,
                "Box file exceeds the configured download limit",
            )
        if exchange.error:
            return _query_retry(FailureKind.TRANSPORT, "Box download response was interrupted")
        output = DownloadedFile(input.file_id, base64.b64encode(exchange.body).decode(), len(exchange.body))
        return QueryAttempt.branch_result(defs.DOWNLOADED, output, Receipt())


class UploadFileOperation:
    """Replace existing content when its Box digest differs."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated replacement Mutation contract."""
        return defs.UPLOAD_FILE_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: UploadFileInput) -> IdempotencyKey:
        """Record stable call identity; Box receives no request key."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UploadFileInput) -> MutationAttempt[UploadedFile]:
        """Compare current content, replace once, and read back ambiguous outcomes."""
        if not _valid_file_id(input.file_id):
            return _mutation_branch(
                defs.DEFECT, FailureKind.VALIDATION, "Box file ID must contain only decimal digits"
            )
        if ((len(input.content_base64) + 3) // 4) * 3 > self.client.max_upload_bytes:
            return _mutation_branch(
                defs.DEFECT, FailureKind.VALIDATION, "Box upload exceeds the configured byte limit"
            )
        try:
            content = base64.b64decode(input.content_base64, validate=True)
        except (ValueError, binascii.Error):
            return _mutation_branch(
                defs.DEFECT, FailureKind.VALIDATION, "Box upload content must be bounded Base64"
            )
        if len(content) > self.client.max_upload_bytes:
            return _mutation_branch(
                defs.DEFECT, FailureKind.VALIDATION, "Box upload content must be bounded Base64"
            )
        digest = hashlib.sha1(content).hexdigest().upper()
        metadata_exchange = await self.client.get_file_metadata(call, input.file_id)
        metadata, decision = _initial_metadata(metadata_exchange, input.file_id)
        if decision is not None:
            return decision
        assert metadata is not None
        if metadata.matches(input.file_id, digest, len(content)):
            return _uploaded(metadata)
        exchange = await self.client.upload(call, input.file_id, metadata.name, content)
        if exchange.error == "credential" and not exchange.request_sent:
            return _mutation_branch(
                defs.DEFECT, FailureKind.AUTHENTICATION, "Box credentials are unavailable or invalid"
            )
        if exchange.error == "refresh":
            return _mutation_retry(FailureKind.AVAILABILITY, "Box credentials could not be refreshed")
        if exchange.error == "request" and not exchange.request_sent:
            return _mutation_retry(FailureKind.TRANSPORT, "Box upload did not leave the process")
        if exchange.status == 429:
            return _mutation_retry(
                FailureKind.RATE_LIMIT, "Box rate limited the upload", exchange.retry_after
            )
        if exchange.status == 408 or exchange.status >= 500:
            return _mutation_retry(
                FailureKind.AVAILABILITY, "Box upload is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _mutation_branch(defs.NOT_FOUND, FailureKind.NOT_FOUND, "Box did not find the file")
        is_accepted = exchange.status in (200, 201)
        if exchange.status != 0 and not is_accepted:
            return _mutation_branch(
                defs.PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, f"Box rejected HTTP {exchange.status}"
            )
        if is_accepted and not exchange.error:
            try:
                wire: Any = json.loads(exchange.body)
                entries = wire["entries"]
                if isinstance(entries, list) and len(entries) == 1:
                    returned = _decode_metadata(json.dumps(entries[0]).encode(), input.file_id)
                    if returned.matches(input.file_id, digest, len(content)):
                        return _uploaded(returned)
            except (ValueError, TypeError, KeyError, IndexError):
                pass
        readback = await self.client.get_file_metadata(call, input.file_id)
        if readback.status == 200 and not readback.error:
            try:
                confirmed = _decode_metadata(readback.body, input.file_id)
                if confirmed.matches(input.file_id, digest, len(content)):
                    return _uploaded(confirmed)
            except (ValueError, TypeError):
                pass
        # Retry is safe: the next attempt compares SHA-1 and size before replacing the content again.
        return _mutation_retry(
            FailureKind.TRANSPORT, "Box replacement could not be confirmed by read-back", readback.retry_after
        )


def _initial_metadata(
    exchange: Exchange, file_id: str
) -> tuple[_FileMetadata | None, MutationAttempt[UploadedFile] | None]:
    if exchange.error == "credential":
        return None, _mutation_branch(
            defs.DEFECT, FailureKind.AUTHENTICATION, "Box credentials are unavailable or invalid"
        )
    if exchange.error == "refresh":
        return None, _mutation_retry(FailureKind.AVAILABILITY, "Box credentials could not be refreshed")
    if exchange.error == "request":
        return None, _mutation_retry(FailureKind.TRANSPORT, "Box metadata request failed")
    if exchange.status == 429:
        return None, _mutation_retry(
            FailureKind.RATE_LIMIT, "Box rate limited metadata lookup", exchange.retry_after
        )
    if exchange.status == 408 or exchange.status >= 500:
        return None, _mutation_retry(
            FailureKind.AVAILABILITY, "Box metadata is temporarily unavailable", exchange.retry_after
        )
    if exchange.status == 404:
        return None, _mutation_branch(defs.NOT_FOUND, FailureKind.NOT_FOUND, "Box did not find the file")
    if exchange.status != 200:
        return None, _mutation_branch(
            defs.PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, f"Box rejected HTTP {exchange.status}"
        )
    if exchange.error == "too_large":
        return None, _mutation_branch(
            defs.INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE, "Box metadata exceeds the configured limit"
        )
    if exchange.error:
        return None, _mutation_retry(FailureKind.TRANSPORT, "Box metadata response was interrupted")
    try:
        return _decode_metadata(exchange.body, file_id), None
    except (ValueError, TypeError):
        return None, _mutation_branch(
            defs.INVALID_RESPONSE, FailureKind.PROTOCOL, "Box returned invalid file metadata"
        )
