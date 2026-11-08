# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Wise transfer reads with Go-compatible outcomes."""

from __future__ import annotations

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
from ._generated.definitions import GET_TRANSFER_DEFINITION
from .models import GetTransferInput, TransferStatus


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "wise", "getTransfer", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[TransferStatus]:
    return QueryAttempt.branch_result(branch, TransferStatus(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[TransferStatus]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_transfer_id(transfer_id: object) -> bool:
    return (
        isinstance(transfer_id, str)
        and 1 <= len(transfer_id) <= 19
        and transfer_id[0] != "0"
        and transfer_id.isascii()
        and transfer_id.isdecimal()
        and int(transfer_id) <= 9007199254740991
    )


def _decode_transfer(body: bytes, requested_transfer_id: str) -> TransferStatus:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict):
        raise ValueError("transfer data missing")
    if (
        type(raw.get("id")) is not int
        or raw["id"] != int(requested_transfer_id)
        or not isinstance(raw.get("status"), str)
        or not raw["status"].strip()
        or not isinstance(raw.get("sourceCurrency"), str)
        or len(raw["sourceCurrency"]) != 3
    ):
        raise ValueError("transfer identity, source currency, or status missing")
    return TransferStatus(requested_transfer_id, raw["sourceCurrency"], raw["status"])


class Client:
    """Resolve an API token per call and read selected transfer metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Wise configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Wise request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_transfer(self) -> GetTransferOperation:
        """Bind the getTransfer Query to this client."""
        return GetTransferOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, transfer_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_token = credential.api_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_token):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/transfers/" + quote(transfer_id, safe=""),
                headers={"Accept": "application/json", "Authorization": "Bearer " + api_token},
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


class GetTransferOperation:
    """Read one transfer's ID, status, and source currency."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query transfer."""
        return GET_TRANSFER_DEFINITION

    async def invoke(self, call: Call, input: GetTransferInput) -> QueryAttempt[TransferStatus]:
        """Classify transfer lookup outcomes consistently with Go."""
        if not isinstance(input, GetTransferInput) or not _valid_transfer_id(input.transfer_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "transfer ID must be a positive decimal integer",
            )
        exchange = await self.client._get(call, input.transfer_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Wise credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Wise request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Wise rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Wise is temporarily unavailable", exchange.retry_after)
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Wise did not expose the transfer")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Wise rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Wise response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Wise response was interrupted")
        try:
            transfer_status = _decode_transfer(exchange.body, input.transfer_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Wise returned invalid transfer JSON")
        return QueryAttempt.branch_result("found", transfer_status, Receipt())
