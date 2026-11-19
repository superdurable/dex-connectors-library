# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Qiniu Kodo bucket lookup matching the Go provider contract."""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
from collections.abc import Callable
from datetime import UTC, datetime

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    INVALID_RESPONSE,
    LIST_BUCKETS_DEFINITION,
    LIST_BUCKETS_OPERATION_ID,
    LISTED,
    PROVIDER_REJECTED,
)
from .models import ListBucketsInput, ListBucketsOutput


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, LIST_BUCKETS_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListBucketsOutput]:
    return QueryAttempt.branch_result(branch, ListBucketsOutput(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListBucketsOutput]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_credential(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value.encode()) <= 256
        and all("!" <= char <= "~" and char != ":" for char in value)
    )


class Client:
    """Own Qiniu settings and sign each lookup with current credentials."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        """Validate endpoint and bounds; inject a clock only for deterministic tests."""
        if config is None or credentials is None:
            raise ValueError("Qiniu settings and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Qiniu timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.clock = clock or (lambda: datetime.now(UTC))

    def list_buckets(self) -> ListBucketsOperation:
        """Bind the read-only bucket-list Query."""
        return ListBucketsOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class ListBucketsOperation:
    """Read bucket names owned by the signing Qiniu account."""

    def __init__(self, client: Client) -> None:
        """Bind a validated Qiniu client."""
        if client is None:
            raise ValueError("Qiniu client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated bucket-list Query contract."""
        return LIST_BUCKETS_DEFINITION

    async def invoke(self, call: Call, input: ListBucketsInput) -> QueryAttempt[ListBucketsOutput]:
        """Sign one request and classify transport, provider, and response outcomes."""
        try:
            credentials = await self.client.credentials.resolve(call)
            access_key = credentials.access_key.reveal()
            secret_key = credentials.secret_key.reveal()
        except Exception:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        if not _valid_credential(access_key) or not _valid_credential(secret_key):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            timestamp = self.client.clock().astimezone(UTC).strftime("%Y%m%dT%H%M%SZ")
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/buckets",
                headers={"Accept": "application/json", "X-Qiniu-Date": timestamp},
            )
            canonical = (
                "GET "
                + request.url.raw_path.decode("ascii")
                + "\nHost: "
                + request.headers["Host"]
                + "\nX-Qiniu-Date: "
                + timestamp
                + "\n\n"
            )
            signature = base64.urlsafe_b64encode(
                hmac.new(secret_key.encode(), canonical.encode(), hashlib.sha1).digest()
            ).decode()
            request.headers["Authorization"] = "Qiniu " + access_key + ":" + signature
        except (httpx.HTTPError, ValueError, TypeError, OverflowError):
            return _branch(DEFECT, FailureKind.VALIDATION, "bucket-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Qiniu request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Qiniu rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Qiniu is temporarily unavailable", retry_after)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"Qiniu rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Qiniu response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Qiniu response was interrupted")
            try:
                document = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
                if not isinstance(document, list) or any(
                    not isinstance(name, str) or not name or len(name.encode()) > 255 for name in document
                ):
                    raise ValueError
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Qiniu returned an invalid bucket list"
                )
            return QueryAttempt.branch_result(LISTED, ListBucketsOutput(tuple(document)), Receipt())
        finally:
            await response.aclose()


Config.__doc__ = "Qiniu Kodo HTTPS origin, request timeout in seconds, and response bound in bytes."
Credentials.__doc__ = "Secret paired Qiniu AccessKey and SecretKey, resolved per Query."
