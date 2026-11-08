# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Adyen payment link reads with Go-compatible outcomes."""

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
from ._generated.definitions import GET_PAYMENT_LINK_DEFINITION
from .models import GetPaymentLinkInput, PaymentLinkStatus


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "adyen", "getPaymentLink", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[PaymentLinkStatus]:
    return QueryAttempt.branch_result(branch, PaymentLinkStatus(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[PaymentLinkStatus]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_payment_link_id(payment_link_id: object) -> bool:
    return (
        isinstance(payment_link_id, str)
        and 1 <= len(payment_link_id) <= 128
        and payment_link_id.isascii()
        and all(character.isalnum() or character in "-_" for character in payment_link_id)
    )


def _decode_payment_link(body: bytes, requested_payment_link_id: str) -> PaymentLinkStatus:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("amount"), dict):
        raise ValueError("payment link data missing")
    amount = raw["amount"]
    if (
        raw.get("id") != requested_payment_link_id
        or not isinstance(raw.get("status"), str)
        or not raw["status"].strip()
        or not isinstance(amount.get("currency"), str)
        or len(amount["currency"]) != 3
    ):
        raise ValueError("payment link identity, status, or currency missing")
    return PaymentLinkStatus(raw["id"], raw["status"], amount["currency"])


class Client:
    """Resolve an API key per call and read selected payment link metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Adyen configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Adyen request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_payment_link(self) -> GetPaymentLinkOperation:
        """Bind the getPaymentLink Query to this client."""
        return GetPaymentLinkOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, payment_link_id: str) -> _Exchange:
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
                self.base_url + "/paymentLinks/" + quote(payment_link_id, safe=""),
                headers={"Accept": "application/json", "X-API-Key": api_key},
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


class GetPaymentLinkOperation:
    """Read one payment link's ID, status, and amount currency."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_PAYMENT_LINK_DEFINITION

    async def invoke(self, call: Call, input: GetPaymentLinkInput) -> QueryAttempt[PaymentLinkStatus]:
        """Classify payment link lookup outcomes consistently with Go."""
        if not isinstance(input, GetPaymentLinkInput) or not _valid_payment_link_id(input.payment_link_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "payment link ID must contain 1–128 URL-safe characters",
            )
        exchange = await self.client._get(call, input.payment_link_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Adyen credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Adyen request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Adyen rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Adyen is temporarily unavailable", exchange.retry_after)
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Adyen did not expose the payment link")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Adyen rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Adyen response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Adyen response was interrupted")
        try:
            payment_link_status = _decode_payment_link(exchange.body, input.payment_link_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Adyen returned invalid payment link JSON"
            )
        return QueryAttempt.branch_result("found", payment_link_status, Receipt())
