# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""LianLian Pay signed payment-status transport and Go-compatible decisions."""

import base64
import hashlib
import json
import re
from collections.abc import Callable
from datetime import UTC, datetime, timedelta, timezone
from typing import Any

import httpx
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa, utils
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
from ._generated.definitions import QUERY_PAYMENT_STATUS_DEFINITION
from .models import PaymentStatusResult, QueryPaymentStatusInput

_PATH = "/query/mch/v1/accp/txn/order-query"
_STATUSES = {"SUCCESS", "FAILURE", "PROCESSING", "CLOSED", "CANCEL"}


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[PaymentStatusResult]:
    return QueryAttempt.branch_result(
        branch, PaymentStatusResult(), Receipt(), Failure(kind, "lianlianpay", "queryPaymentStatus", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[PaymentStatusResult]:
    return QueryAttempt.retry(Failure(kind, "lianlianpay", "queryPaymentStatus", message), delay)


def _valid_order(value: str) -> bool:
    return (
        bool(value)
        and len(value) <= 32
        and value.strip() == value
        and all(0x21 <= ord(character) <= 0x7E for character in value)
    )


def _private_key(value: str) -> rsa.RSAPrivateKey | None:
    if len(value) > 16384 or not value.startswith("-----BEGIN "):
        return None
    if not re.fullmatch(
        r"-----BEGIN (RSA )?PRIVATE KEY-----[\s\S]*-----END (RSA )?PRIVATE KEY-----\s*", value
    ):
        return None
    try:
        parsed = serialization.load_pem_private_key(value.encode(), password=None)
    except (ValueError, TypeError):
        return None
    return parsed if isinstance(parsed, rsa.RSAPrivateKey) else None


class Client:
    """Resolve merchant credentials for each status Query and own an HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("LianLian Pay configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("LianLian Pay timeout or response limit is invalid")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def query_payment_status(self) -> "QueryPaymentStatusOperation":
        """Bind the signed payment-status Query to this client."""
        return QueryPaymentStatusOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class QueryPaymentStatusOperation:
    """Read an authorized merchant payment order's processing status."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("LianLian Pay client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated payment-status Query identity."""
        return QUERY_PAYMENT_STATUS_DEFINITION

    async def invoke(self, call: Call, input: QueryPaymentStatusInput) -> QueryAttempt[PaymentStatusResult]:
        """Sign one order query and classify its provider response."""
        if not _valid_order(input.merchant_order_id):
            return _branch("defect", FailureKind.VALIDATION, "LianLian Pay merchant order number is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            if not re.fullmatch(r"[0-9]{16,18}", credentials.merchant_id):
                raise ValueError("invalid merchant ID")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "LianLian Pay credentials are unavailable or invalid"
            )
        key = _private_key(credentials.private_key.reveal())
        if key is None:
            return _branch("defect", FailureKind.AUTHENTICATION, "LianLian Pay private key is invalid")
        body = json.dumps(
            {"mch_id": credentials.merchant_id, "txn_seqno": input.merchant_order_id}, separators=(",", ":")
        ).encode()
        first_digest = hashlib.md5(body, usedforsecurity=False).hexdigest().encode()
        second_digest = hashlib.md5(first_digest, usedforsecurity=False).digest()
        signature = base64.b64encode(
            key.sign(second_digest, padding.PKCS1v15(), utils.Prehashed(hashes.MD5()))
        )
        timestamp = self.client.now().astimezone(timezone(timedelta(hours=8))).strftime("%Y%m%d%H%M%S")
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + _PATH,
                content=body,
                headers={
                    "Signature-Type": "RSA",
                    "Signature-Data": signature.decode(),
                    "mch_id": credentials.merchant_id,
                    "timestamp": timestamp,
                    "Content-Type": "application/json;charset=utf-8",
                    "Accept": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "LianLian Pay order request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "LianLian Pay order request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "LianLian Pay rate limited order query", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "LianLian Pay order API is temporarily unavailable", delay
                )
            try:
                response_body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "LianLian Pay response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "LianLian Pay order response was interrupted")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.VALIDATION
                    if status in (400, 404, 412)
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(
                    "providerRejected", kind, f"LianLian Pay rejected order query with HTTP {status}"
                )
        finally:
            await response.aclose()
        return _decode_payment(response_body, input)


def _decode_payment(body: bytes, input: QueryPaymentStatusInput) -> QueryAttempt[PaymentStatusResult]:
    try:
        wire: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "LianLian Pay returned an invalid order response"
        )
    if not isinstance(wire, dict) or not isinstance(wire.get("ret_code"), str) or not wire["ret_code"]:
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "LianLian Pay returned an invalid order response"
        )
    if wire["ret_code"] != "0000":
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, "LianLian Pay rejected order query"
        )
    if wire.get("txn_seqno") != input.merchant_order_id or wire.get("txn_status") not in _STATUSES:
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "LianLian Pay returned an invalid order response"
        )
    return QueryAttempt.branch_result(
        "found",
        PaymentStatusResult(merchant_order_id=input.merchant_order_id, status=wire["txn_status"]),
        Receipt(),
    )
