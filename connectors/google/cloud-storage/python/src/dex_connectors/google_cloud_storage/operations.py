# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Cloud Storage decisions matching Go-recorded provider scenarios."""

from __future__ import annotations

import base64
import binascii
import json
import re
from typing import Any, TypeVar
from urllib.parse import quote, urlencode

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

from ._generated import definitions
from .client import Client, Exchange
from .models import (
    DownloadedObject,
    DownloadObjectInput,
    ListObjectsInput,
    Object,
    ObjectsPage,
    UploadObjectInput,
)

Output = TypeVar("Output")
_BUCKET = re.compile(r"[a-z0-9][a-z0-9._-]{1,220}[a-z0-9]\Z")
_GENERATION = re.compile(r"[1-9][0-9]{0,19}\Z")
_MEDIA_TYPE = re.compile(r"[^\s/;]+/[^\s/;]+(?:\s*;.*)?\Z")


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "google-cloud-storage", operation, message)


def _branch(
    operation: str, branch: str, kind: FailureKind, message: str, empty: Output
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(branch, empty, Receipt(), _failure(operation, kind, message))


def _retry(operation: str, kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[Any]:
    return QueryAttempt.retry(_failure(operation, kind, message), delay)


def _read_failure(operation: str, exchange: Exchange, empty: Output) -> QueryAttempt[Output] | None:
    if exchange.error == "credential":
        return _branch(
            operation,
            "defect",
            FailureKind.AUTHENTICATION,
            "connection credentials are unavailable or invalid",
            empty,
        )
    if exchange.error == "redirect":
        return _branch(
            operation,
            "invalidResponse",
            FailureKind.PROTOCOL,
            "Google returned an unsafe download redirect",
            empty,
        )
    if exchange.error in ("request", "refresh", "not_built"):
        return _retry(operation, FailureKind.TRANSPORT, "Google Cloud Storage request failed")
    if exchange.status == 429:
        return _retry(
            operation, FailureKind.RATE_LIMIT, "Google rate limited the request", exchange.retry_after
        )
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(
            operation,
            FailureKind.AVAILABILITY,
            "Google Cloud Storage is temporarily unavailable",
            exchange.retry_after,
        )
    if exchange.status != 200:
        return _branch(
            operation,
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Google rejected HTTP {exchange.status}",
            empty,
        )
    if exchange.error == "too_large":
        return _branch(
            operation,
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Google response exceeded the configured limit",
            empty,
        )
    if exchange.error:
        return _retry(operation, FailureKind.TRANSPORT, "Google response was interrupted")
    return None


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _valid_text(value: object, maximum: int) -> bool:
    return (
        isinstance(value, str) and len(value.encode()) <= maximum and "\r" not in value and "\n" not in value
    )


def _valid_bucket(value: object) -> bool:
    return isinstance(value, str) and _BUCKET.fullmatch(value) is not None


def _valid_name(value: object) -> bool:
    return _valid_text(value, 1024) and value != ""


def _valid_generation(value: object) -> bool:
    return isinstance(value, str) and _GENERATION.fullmatch(value) is not None


def _object(value: object, name: str) -> Object | None:
    if (
        not isinstance(value, dict)
        or value.get("name") != name
        or not _valid_generation(value.get("generation"))
    ):
        return None
    return Object(name, value["generation"])


class ListObjectsOperation:
    """Read one page of object identities."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated object page Query contract."""
        return definitions.LIST_OBJECTS_DEFINITION

    async def invoke(self, call: Call, input: ListObjectsInput) -> QueryAttempt[ObjectsPage]:
        """List one bounded object page."""
        empty = ObjectsPage()
        if (
            not _valid_bucket(input.bucket)
            or not 0 <= input.page_size <= 1000
            or not _valid_text(input.prefix, 1024)
            or not _valid_text(input.page_token, 4096)
        ):
            return _branch(
                "listObjects", "defect", FailureKind.VALIDATION, "object page input is invalid", empty
            )
        page_size = input.page_size or 100
        query = {"maxResults": str(page_size)}
        if input.prefix:
            query["prefix"] = input.prefix
        if input.page_token:
            query["pageToken"] = input.page_token
        path = f"/storage/v1/b/{quote(input.bucket, safe='')}/o?{urlencode(sorted(query.items()))}"
        exchange = await self.client.request(call, path)
        failure = _read_failure("listObjects", exchange, empty)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch(
                "listObjects",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned an invalid object page",
                empty,
            )
        entries = document.get("items", [])
        next_token = document.get("nextPageToken", "")
        if not isinstance(entries, list) or len(entries) > page_size or not _valid_text(next_token, 4096):
            return _branch(
                "listObjects",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned an invalid object page",
                empty,
            )
        objects = []
        for item in entries:
            if (
                not isinstance(item, dict)
                or not _valid_name(item.get("name"))
                or not _valid_generation(item.get("generation"))
            ):
                return _branch(
                    "listObjects",
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Google returned an invalid object page",
                    empty,
                )
            objects.append(Object(item["name"], item["generation"]))
        return QueryAttempt.branch_result("listed", ObjectsPage(tuple(objects), next_token), Receipt())


class DownloadObjectOperation:
    """Read one bounded object body."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated object download Query contract."""
        return definitions.DOWNLOAD_OBJECT_DEFINITION

    async def invoke(self, call: Call, input: DownloadObjectInput) -> QueryAttempt[DownloadedObject]:
        """Download the selected object generation."""
        empty = DownloadedObject()
        if (
            not _valid_bucket(input.bucket)
            or not _valid_name(input.name)
            or (input.generation and not _valid_generation(input.generation))
        ):
            return _branch(
                "downloadObject", "defect", FailureKind.VALIDATION, "object identity is invalid", empty
            )
        query = {"alt": "media"}
        if input.generation:
            query["generation"] = input.generation
        object_path = f"/storage/v1/b/{quote(input.bucket, safe='')}/o/{quote(input.name, safe='')}"
        path = f"{object_path}?{urlencode(sorted(query.items()))}"
        exchange = await self.client.request(call, path, media=True)
        if exchange.status == 404 and not exchange.error:
            return _branch(
                "downloadObject", "notFound", FailureKind.PROVIDER_REJECTION, "object was not found", empty
            )
        failure = _read_failure("downloadObject", exchange, empty)
        if failure is not None:
            return failure
        return QueryAttempt.branch_result(
            "downloaded",
            DownloadedObject(base64.b64encode(exchange.body).decode(), exchange.content_type),
            Receipt(),
        )


class UploadObjectOperation:
    """Create one object only if its name is absent."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated conditional upload Mutation contract."""
        return definitions.UPLOAD_OBJECT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UploadObjectInput) -> IdempotencyKey:
        """Record Dex call identity; the generation precondition prevents duplicate writes."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UploadObjectInput) -> MutationAttempt[Object]:
        """Upload if absent or confirm an identical existing generation."""
        if (
            not _valid_bucket(input.bucket)
            or not _valid_name(input.name)
            or not isinstance(input.content_type, str)
            or not _MEDIA_TYPE.fullmatch(input.content_type)
            or not isinstance(input.content_base64, str)
            or len(input.content_base64) > ((self.client.max_upload_bytes + 2) // 3) * 4 + 4
        ):
            return self._failure("defect", FailureKind.VALIDATION, "object upload input is invalid")
        try:
            content = base64.b64decode(input.content_base64, validate=True)
        except (binascii.Error, ValueError):
            return self._failure("defect", FailureKind.VALIDATION, "object content is invalid or too large")
        if (
            len(content) > self.client.max_upload_bytes
            or base64.b64encode(content).decode() != input.content_base64
        ):
            return self._failure("defect", FailureKind.VALIDATION, "object content is invalid or too large")
        token, error = await self.client.resolve_token(call)
        if error == "credential":
            return self._failure(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        if error:
            return self._retry(FailureKind.TRANSPORT, "connection credentials could not be refreshed")
        query = {"ifGenerationMatch": "0", "name": input.name, "uploadType": "media"}
        path = f"/upload/storage/v1/b/{quote(input.bucket, safe='')}/o?{urlencode(sorted(query.items()))}"
        exchange = await self.client.request(
            call, path, token=token, method="POST", body=content, content_type=input.content_type
        )
        if exchange.status == 412 and not exchange.error:
            return await self._confirm_existing_object(call, input, content)
        if exchange.status == 429:
            return self._retry(FailureKind.RATE_LIMIT, "Google rate limited the upload", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return self._retry(
                FailureKind.AVAILABILITY,
                "Google Cloud Storage upload is temporarily unavailable",
                exchange.retry_after,
            )
        if exchange.error:
            return self._retry(FailureKind.TRANSPORT, "Google Cloud Storage upload failed")
        if exchange.status not in (200, 201):
            return self._failure(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Google rejected HTTP {exchange.status}"
            )
        object_value = _object(_document(exchange.body), input.name)
        if object_value is None:
            return self._retry(
                FailureKind.PROTOCOL, "Google returned invalid upload metadata; checking object on retry"
            )
        return MutationAttempt.branch_result("uploaded", object_value, Receipt())

    async def _confirm_existing_object(
        self, call: Call, input: UploadObjectInput, content: bytes
    ) -> MutationAttempt[Object]:
        path = f"/storage/v1/b/{quote(input.bucket, safe='')}/o/{quote(input.name, safe='')}"
        metadata = await self.client.request(call, path)
        if metadata.error or metadata.status == 429 or metadata.status >= 500:
            return self._retry(
                FailureKind.AVAILABILITY,
                "matching object metadata could not be confirmed",
                metadata.retry_after,
            )
        if metadata.status != 200:
            return self._retry(FailureKind.AVAILABILITY, "matching object changed during confirmation")
        object_value = _object(_document(metadata.body), input.name)
        if object_value is None:
            return self._retry(FailureKind.PROTOCOL, "Google returned invalid object metadata")
        query = urlencode(sorted({"alt": "media", "generation": object_value.generation}.items()))
        media = await self.client.request(call, f"{path}?{query}", media=True)
        if media.status == 404:
            return self._retry(FailureKind.AVAILABILITY, "object changed during upload confirmation")
        if media.error == "too_large":
            return self._failure(
                "objectExists",
                FailureKind.PROVIDER_REJECTION,
                "an object with different content already exists",
            )
        if media.error or media.status == 429 or media.status >= 500:
            return self._retry(
                FailureKind.AVAILABILITY, "existing object could not be confirmed", media.retry_after
            )
        if media.status != 200:
            return self._failure(
                "providerRejected", FailureKind.PROVIDER_REJECTION, "Google denied object confirmation"
            )
        if media.body != content:
            return self._failure(
                "objectExists",
                FailureKind.PROVIDER_REJECTION,
                "an object with different content already exists",
            )
        return MutationAttempt.branch_result("uploaded", object_value, Receipt())

    @staticmethod
    def _failure(branch: str, kind: FailureKind, message: str) -> MutationAttempt[Object]:
        return MutationAttempt.branch_result(
            branch, Object(), Receipt(), _failure("uploadObject", kind, message)
        )

    @staticmethod
    def _retry(kind: FailureKind, message: str, delay: float = 0) -> MutationAttempt[Object]:
        return MutationAttempt.retry(_failure("uploadObject", kind, message), delay)
