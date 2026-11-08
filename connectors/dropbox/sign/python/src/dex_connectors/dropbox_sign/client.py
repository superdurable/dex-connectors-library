# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Dropbox Sign signature-request reads with Go-compatible outcomes."""

from __future__ import annotations

import base64
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote

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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_SIGNATURE_REQUEST_DEFINITION
from .models import GetSignatureRequestInput, SignatureRequestStatus


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "dropbox-sign", "getSignatureRequest", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SignatureRequestStatus]:
    return QueryAttempt.branch_result(branch, SignatureRequestStatus(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[SignatureRequestStatus]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_signature_request_id(value: object) -> bool:
    return (
        isinstance(value, str)
        and bool(value)
        and len(value.encode("utf-8")) <= 256
        and not any(ord(char) < 32 or 127 <= ord(char) <= 159 or char in "/?#" for char in value)
    )


def _decode_signature_request(body: bytes, requested_id: str) -> SignatureRequestStatus:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("signature_request"), dict):
        raise ValueError("signature request object missing")
    request = raw["signature_request"]
    if (
        request.get("signature_request_id") != requested_id
        or not isinstance(request.get("title"), str)
        or not request["title"].strip()
        or type(request.get("is_complete")) is not bool
        or type(request.get("is_declined")) is not bool
        or type(request.get("has_error")) is not bool
    ):
        raise ValueError("signature request identity or status missing")
    return SignatureRequestStatus(
        id=request["signature_request_id"],
        title=request["title"],
        is_complete=request["is_complete"],
        is_declined=request["is_declined"],
        has_error=request["has_error"],
    )


class Client:
    """Resolve an API key per call and read selected signature-request status."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Dropbox Sign configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Dropbox Sign request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_signature_request(self) -> GetSignatureRequestOperation:
        """Bind the getSignatureRequest Query to this client."""
        return GetSignatureRequestOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, request_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_key) or ":" in api_key:
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/signature_request/" + quote(request_id, safe=""),
                headers={
                    "Accept": "application/json",
                    "Authorization": "Basic " + base64.b64encode((api_key + ":").encode()).decode(),
                },
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            return _Exchange(status=response.status_code, body=body, retry_after=retry_after)
        finally:
            await response.aclose()


class GetSignatureRequestOperation:
    """Read one signature request's title and completion status."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_SIGNATURE_REQUEST_DEFINITION

    async def invoke(
        self, call: Call, input: GetSignatureRequestInput
    ) -> QueryAttempt[SignatureRequestStatus]:
        """Classify lookup outcomes consistently with Go."""
        if not isinstance(input, GetSignatureRequestInput) or not _valid_signature_request_id(
            input.signature_request_id
        ):
            return _branch(
                "defect", FailureKind.VALIDATION, "signature request ID must be 1–256 printable characters"
            )
        exchange = await self.client._get(call, input.signature_request_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Dropbox Sign credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Dropbox Sign request failed")
        if exchange.status == 429:
            return _retry(
                FailureKind.RATE_LIMIT, "Dropbox Sign rate limited the request", exchange.retry_after
            )
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Dropbox Sign is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch(
                "notFound", FailureKind.NOT_FOUND, "Dropbox Sign did not expose the signature request"
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Dropbox Sign rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Dropbox Sign response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Dropbox Sign response was interrupted")
        try:
            status = _decode_signature_request(exchange.body, input.signature_request_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Dropbox Sign returned invalid signature request JSON",
            )
        return QueryAttempt.branch_result("found", status, Receipt())
