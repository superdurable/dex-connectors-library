# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Square provider reads and Go-compatible branch classification."""

import json
from collections.abc import Callable
from dataclasses import dataclass as dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any, TypeVar
from urllib.parse import quote, urlencode

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk import OperationRef as OperationRef
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk import StepDefaults as StepDefaults
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
from dex_connectors.sdk.retry_policy import (
    default_connector_step_execute_retry_policy as default_connector_step_execute_retry_policy,
)

from ._generated.config import (
    Config as Config,
)
from ._generated.config import (
    Credentials as Credentials,
)
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    FOUND as FOUND,
)
from ._generated.definitions import (
    GET_PAYMENT_DEFINITION as GET_PAYMENT_DEFINITION,
)
from ._generated.definitions import (
    GET_PAYMENT_OPERATION_ID as GET_PAYMENT_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_PAYMENTS_DEFINITION as LIST_PAYMENTS_DEFINITION,
)
from ._generated.definitions import (
    LIST_PAYMENTS_OPERATION_ID as LIST_PAYMENTS_OPERATION_ID,
)
from ._generated.definitions import (
    LISTED as LISTED,
)
from ._generated.definitions import (
    NOT_FOUND as NOT_FOUND,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import GetPaymentInput, ListPaymentsInput, Money, Payment, PaymentPage

SQUARE_VERSION = "2026-09-16"
_DEFAULT_BASE_URL = "https://connect.squareup.com/v2"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152

Output = TypeVar("Output")


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Square configuration is required")
        if credentials is None:
            raise ValueError("Square credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_payments(self) -> "ListPaymentsOperation":
        """Bind the payment page Query to this client."""
        return ListPaymentsOperation(self)

    def get_payment(self) -> "GetPaymentOperation":
        """Bind the payment lookup Query to this client."""
        return GetPaymentOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def authorize(self, call: Call) -> dict[str, str] | None:
        """Return the authentication headers, or None when credentials are unusable."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return None
        token = credentials.access_token.reveal()
        if not token or not is_header_safe_credential(token):
            return None
        return {"Authorization": "Bearer " + token, "Square-Version": SQUARE_VERSION}

    async def execute_read(
        self,
        call: Call,
        operation: str,
        path: str,
        query: dict[str, str],
        empty: Output,
        success: str,
        decode: Callable[[Any], Output],
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go executeRead helper."""
        endpoint = self.base_url + path
        if query:
            endpoint += "?" + urlencode(sorted(query.items()))
        headers = await self.authorize(call)
        if headers is None:
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "Square credentials are unavailable"
            )
        try:
            request = self.http_client.build_request(
                "GET", endpoint, headers={"Accept": "application/json", **headers}
            )
        except (httpx.HTTPError, ValueError):
            return _failed(
                operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "provider request could not be built"
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _retry(operation, FailureKind.AVAILABILITY, "provider read failed", 0)
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(
                    operation, FailureKind.RATE_LIMIT, "provider rate limited the read", retry_after
                )
            if status == 408 or status >= 500:
                return _retry(
                    operation,
                    FailureKind.AVAILABILITY,
                    "provider read is temporarily unavailable",
                    retry_after,
                )
            if status == 404:
                return _failed(
                    operation, NOT_FOUND, empty, FailureKind.PROVIDER_REJECTION, "resource was not found"
                )
            if status == 401:
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.AUTHENTICATION,
                    "provider rejected credentials",
                )
            if status == 403:
                return _failed(
                    operation, PROVIDER_REJECTED, empty, FailureKind.AUTHORIZATION, "provider denied access"
                )
            if status != 200:
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected read with HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "provider response was interrupted", 0)
            try:
                value = decode(json.loads(body))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _failed(
                    operation, INVALID_RESPONSE, empty, FailureKind.PROTOCOL, "provider returned invalid JSON"
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class ListPaymentsOperation:
    """Read one page of Square payments."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Square client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_PAYMENTS_DEFINITION

    async def invoke(self, call: Call, input: ListPaymentsInput) -> QueryAttempt[PaymentPage]:
        """Read once, then select a branch or safe retry."""
        operation = LIST_PAYMENTS_OPERATION_ID
        if not isinstance(input, ListPaymentsInput) or not isinstance(input.cursor, str):
            return _failed(operation, DEFECT, PaymentPage(), FailureKind.VALIDATION, "cursor must be text")
        query = {"cursor": input.cursor} if input.cursor else {}
        return await self.client.execute_read(
            call, operation, "/payments", query, PaymentPage(), LISTED, _decode_payment_page
        )


class GetPaymentOperation:
    """Read one Square payment by ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Square client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_PAYMENT_DEFINITION

    async def invoke(self, call: Call, input: GetPaymentInput) -> QueryAttempt[Payment]:
        """Validate the payment ID, read once, then select a branch or safe retry."""
        operation = GET_PAYMENT_OPERATION_ID
        if not isinstance(input, GetPaymentInput) or not is_valid_path_segment(input.payment_id):
            return _failed(
                operation, DEFECT, Payment(), FailureKind.VALIDATION, "paymentId must be a path segment"
            )
        path = "/payments/" + quote(input.payment_id, safe="")
        return await self.client.execute_read(
            call, operation, path, {}, Payment(), FOUND, _decode_payment_envelope
        )


def is_valid_path_segment(value: object) -> bool:
    """Accept one non-dot path segment without separators or control characters."""
    return (
        isinstance(value, str)
        and value not in ("", ".", "..")
        and not any(character in "/?#" for character in value)
        and all(ord(character) >= 32 for character in value)
    )


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _decode_payment_page(document: Any) -> PaymentPage:
    document = _object(document)
    payments = document.get("payments")
    if payments is None:
        payments = []
    if not isinstance(payments, list):
        raise ValueError
    return PaymentPage(tuple(_decode_payment(payment) for payment in payments), _string(document, "cursor"))


def _decode_payment_envelope(document: Any) -> Payment:
    payment = _decode_payment(_object(document).get("payment") or {})
    if not payment.id:
        raise ValueError
    return payment


def _decode_payment(document: Any) -> Payment:
    document = _object(document)
    money = document.get("amount_money")
    money = _object({} if money is None else money)
    amount = money.get("amount")
    if amount is None:
        amount = 0
    if type(amount) is not int:
        raise ValueError
    return Payment(
        _string(document, "id"), _string(document, "status"), Money(amount, _string(money, "currency"))
    )


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


Config.__doc__ = "Square HTTP settings; zero or blank values use manifest defaults."
Credentials.__doc__ = "Square access token, resolved anew for every provider attempt."
