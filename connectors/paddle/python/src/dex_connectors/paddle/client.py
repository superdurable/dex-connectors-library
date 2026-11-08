# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Paddle transaction reads with Go-compatible outcomes."""

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
from ._generated.definitions import GET_TRANSACTION_DEFINITION
from .models import GetTransactionInput, TransactionStatus


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "paddle", "getTransaction", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[TransactionStatus]:
    return QueryAttempt.branch_result(branch, TransactionStatus(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[TransactionStatus]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_transaction_id(transaction_id: object) -> bool:
    return (
        isinstance(transaction_id, str)
        and len(transaction_id) == 30
        and transaction_id.startswith("txn_")
        and all(character in "abcdefghijklmnopqrstuvwxyz0123456789" for character in transaction_id[4:])
    )


def _decode_transaction(body: bytes, requested_transaction_id: str) -> TransactionStatus:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("data"), dict):
        raise ValueError("transaction data missing")
    data = raw["data"]
    if (
        data.get("id") != requested_transaction_id
        or not isinstance(data.get("status"), str)
        or not data["status"].strip()
        or not isinstance(data.get("currency_code"), str)
        or len(data["currency_code"]) != 3
    ):
        raise ValueError("transaction identity, status, or currency missing")
    return TransactionStatus(data["id"], data["status"], data["currency_code"])


class Client:
    """Resolve an API key per call and read selected transaction metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Paddle configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Paddle request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_transaction(self) -> GetTransactionOperation:
        """Bind the getTransaction Query to this client."""
        return GetTransactionOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, transaction_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_key):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/transactions/" + quote(transaction_id, safe=""),
                headers={"Accept": "application/json", "Authorization": "Bearer " + api_key},
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


class GetTransactionOperation:
    """Read one transaction's ID, name, and status."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_TRANSACTION_DEFINITION

    async def invoke(self, call: Call, input: GetTransactionInput) -> QueryAttempt[TransactionStatus]:
        """Classify transaction lookup outcomes consistently with Go."""
        if not isinstance(input, GetTransactionInput) or not _valid_transaction_id(input.transaction_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "transaction ID must start with txn_ and contain 26 lowercase letters or digits",
            )
        exchange = await self.client._get(call, input.transaction_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Paddle credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Paddle request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Paddle rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Paddle is temporarily unavailable", exchange.retry_after)
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Paddle did not expose the transaction")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Paddle rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Paddle response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Paddle response was interrupted")
        try:
            transaction_status = _decode_transaction(exchange.body, input.transaction_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Paddle returned invalid transaction JSON"
            )
        return QueryAttempt.branch_result("found", transaction_status, Receipt())
