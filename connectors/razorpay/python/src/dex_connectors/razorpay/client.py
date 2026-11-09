# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Razorpay payment reads with Go-compatible outcomes."""

from __future__ import annotations

import base64
import json
import re
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
from ._generated.definitions import GET_PAYMENT_DEFINITION
from .models import GetPaymentInput, PaymentStatus


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "razorpay", "getPayment", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[PaymentStatus]:
    return QueryAttempt.branch_result(branch, PaymentStatus(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[PaymentStatus]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_payment_id(payment_id: object) -> bool:
    return isinstance(payment_id, str) and re.fullmatch(r"pay_[A-Za-z0-9]{6,64}", payment_id) is not None


def _is_payment_missing(body: bytes) -> bool:
    """Report Razorpay's documented HTTP 400 error for an unknown payment ID."""
    try:
        raw: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return False
    error = raw.get("error") if isinstance(raw, dict) else None
    return (
        isinstance(error, dict)
        and error.get("code") == "BAD_REQUEST_ERROR"
        and error.get("description") == "The id provided does not exist"
    )


def _decode_payment(body: bytes, requested_payment_id: str) -> PaymentStatus:
    data: Any = json.loads(body)
    if not isinstance(data, dict) or (
        data.get("id") != requested_payment_id
        or data.get("entity") != "payment"
        or not isinstance(data.get("status"), str)
        or not data["status"].strip()
        or type(data.get("amount")) is not int
        or data["amount"] <= 0
        or not isinstance(data.get("currency"), str)
        or len(data["currency"]) != 3
    ):
        raise ValueError("payment identity, status, amount, or currency missing")
    return PaymentStatus(data["id"], data["status"], data["amount"], data["currency"])


class Client:
    """Resolve an API key per call and read selected payment metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Razorpay configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Razorpay request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_payment(self) -> GetPaymentOperation:
        """Bind the getPayment Query to this client."""
        return GetPaymentOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, payment_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            key_id = credential.key_id.reveal()
            key_secret = credential.key_secret.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if (
            not is_header_safe_credential(key_id)
            or ":" in key_id
            or not is_header_safe_credential(key_secret)
        ):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/payments/" + quote(payment_id, safe=""),
                headers={
                    "Accept": "application/json",
                    "Authorization": "Basic "
                    + base64.b64encode((key_id + ":" + key_secret).encode()).decode(),
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


class GetPaymentOperation:
    """Read one payment's ID, status, amount, and currency."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_PAYMENT_DEFINITION

    async def invoke(self, call: Call, input: GetPaymentInput) -> QueryAttempt[PaymentStatus]:
        """Classify payment lookup outcomes consistently with Go."""
        if not isinstance(input, GetPaymentInput) or not _valid_payment_id(input.payment_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "payment ID must start with pay_ and contain 6–64 letters or digits",
            )
        exchange = await self.client._get(call, input.payment_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Razorpay credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Razorpay request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Razorpay rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Razorpay is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404 or (
            exchange.status == 400 and not exchange.error and _is_payment_missing(exchange.body)
        ):
            return _branch("notFound", FailureKind.NOT_FOUND, "Razorpay did not expose the payment")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Razorpay rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Razorpay response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Razorpay response was interrupted")
        try:
            payment_status = _decode_payment(exchange.body, input.payment_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Razorpay returned invalid payment JSON"
            )
        return QueryAttempt.branch_result("found", payment_status, Receipt())
