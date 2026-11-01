# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""PayPal provider reads behind a cached client-credentials token, with Go-compatible classification."""

import base64
import hashlib
import json
import time
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import quote, urlencode

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    SecretString,
    StepDefaults,
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
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from .models import (
    GetOrderInput,
    ListTransactionsInput,
    Order,
    TransactionAmount,
    TransactionDetail,
    TransactionInfo,
    TransactionPage,
)

CONNECTOR_ID = "paypal"
GET_ORDER_OPERATION_ID = "getOrder"
LIST_TRANSACTIONS_OPERATION_ID = "listTransactions"
FOUND = "found"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
_DEFAULT_BASE_URL = "https://api-m.paypal.com"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_MAX_TOKEN_RESPONSE_BYTES = 65_536
_TOKEN_EXPIRY_MARGIN_SECONDS = 30
_MAX_TOKEN_LIFETIME_SECONDS = 24 * 60 * 60
_MAX_TRANSACTION_WINDOW = timedelta(days=31)
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_SHARED_BRANCHES = (
    BranchDefinition(NOT_FOUND, "The requested resource was not found.", True),
    BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
    BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
    BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
)
GET_ORDER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ORDER_OPERATION_ID),
    (BranchDefinition(FOUND, "Read one PayPal checkout order."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)
LIST_TRANSACTIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TRANSACTIONS_OPERATION_ID),
    (BranchDefinition(LISTED, "List one page of PayPal transactions in a date range."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """PayPal HTTP settings; zero or blank values use manifest defaults."""

    base_url: str = _DEFAULT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """PayPal REST app client ID and secret, resolved anew for every provider attempt."""

    client_id: str
    client_secret: SecretString


@dataclass(frozen=True, slots=True)
class _AccessToken:
    """A bearer token cached for the credential pair that obtained it."""

    credential_hash: bytes
    value: str
    expires_at: float


@dataclass(frozen=True, slots=True)
class _AuthorizationFailure:
    """A failed token exchange: a retry, or a terminal branch with its Failure."""

    failure: Failure
    branch: str = ""
    is_retry: bool = False
    retry_after_seconds: float = 0

    def as_query(self, empty: Output) -> QueryAttempt[Output]:
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


class Client:
    """One redirect-safe HTTP client, call-time credential source, and cached bearer token."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("PayPal configuration is required")
        if credentials is None:
            raise ValueError("PayPal credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )
        self._cached_token: _AccessToken | None = None

    def get_order(self) -> "GetOrderOperation":
        """Bind the order lookup Query to this client."""
        return GetOrderOperation(self)

    def list_transactions(self) -> "ListTransactionsOperation":
        """Bind the transaction page Query to this client."""
        return ListTransactionsOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def authorize(self, call: Call, operation: str) -> str | _AuthorizationFailure:
        """Return a bearer token, exchanging the client credentials when no cached token fits."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            credentials = None
        client_id = "" if credentials is None else credentials.client_id
        client_secret = "" if credentials is None else credentials.client_secret.reveal()
        if (
            not isinstance(client_id, str)
            or not client_id
            or not client_secret
            or not is_header_safe_credential(client_id)
            or not is_header_safe_credential(client_secret)
        ):
            return _authorization_failure(
                operation, FailureKind.AUTHENTICATION, "PayPal credentials are unavailable", DEFECT
            )
        credential_hash = hashlib.sha256(f"{client_id}\x00{client_secret}".encode()).digest()
        cached = self._cached_token
        if (
            cached is not None
            and cached.credential_hash == credential_hash
            and time.monotonic() < cached.expires_at
        ):
            return cached.value
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + "/v1/oauth2/token",
                content=b"grant_type=client_credentials",
                headers={
                    "Authorization": "Basic "
                    + base64.b64encode(f"{client_id}:{client_secret}".encode()).decode(),
                    "Content-Type": "application/x-www-form-urlencoded",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _authorization_failure(
                operation, FailureKind.LOCAL_DEFECT, "PayPal token request could not be built", DEFECT
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _authorization_failure(
                operation, FailureKind.AVAILABILITY, "PayPal token exchange failed", is_retry=True
            )
        try:
            status = response.status_code
            if status in (408, 429) or status >= 500:
                return _authorization_failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "PayPal token exchange unavailable",
                    is_retry=True,
                    retry_after_seconds=parse_retry_after(
                        response.headers.get("Retry-After", ""), datetime.now(UTC)
                    ),
                )
            if status != 200:
                return _authorization_failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "PayPal rejected client credentials",
                    PROVIDER_REJECTED,
                )
            try:
                body = await read_bounded_body(response, _MAX_TOKEN_RESPONSE_BYTES)
            except BodyTooLargeError:
                return _authorization_failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "PayPal token response exceeded the limit",
                    INVALID_RESPONSE,
                )
            except ProviderTransportError:
                return _authorization_failure(
                    operation, FailureKind.AVAILABILITY, "PayPal token response unavailable", is_retry=True
                )
        finally:
            await response.aclose()
        token = _decode_access_token(body)
        if token is None:
            return _authorization_failure(
                operation, FailureKind.PROTOCOL, "PayPal returned an invalid token response", INVALID_RESPONSE
            )
        access_token, expires_in = token
        if expires_in > _TOKEN_EXPIRY_MARGIN_SECONDS:
            lifetime = min(expires_in - _TOKEN_EXPIRY_MARGIN_SECONDS, _MAX_TOKEN_LIFETIME_SECONDS)
            self._cached_token = _AccessToken(credential_hash, access_token, time.monotonic() + lifetime)
        return access_token

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
        token = await self.authorize(call, operation)
        if isinstance(token, _AuthorizationFailure):
            return token.as_query(empty)
        try:
            request = self.http_client.build_request(
                "GET", endpoint, headers={"Accept": "application/json", "Authorization": "Bearer " + token}
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
                self._cached_token = None
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


class GetOrderOperation:
    """Read one PayPal checkout order."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("PayPal client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_ORDER_DEFINITION

    async def invoke(self, call: Call, input: GetOrderInput) -> QueryAttempt[Order]:
        """Validate the order ID, read once, then select a branch or safe retry."""
        operation = GET_ORDER_OPERATION_ID
        if not isinstance(input, GetOrderInput) or not is_valid_path_segment(input.order_id):
            return _failed(
                operation, DEFECT, Order(), FailureKind.VALIDATION, "orderId must be a path segment"
            )
        path = "/v2/checkout/orders/" + quote(input.order_id, safe="$&+:=@")
        return await self.client.execute_read(call, operation, path, {}, Order(), FOUND, _decode_order)


class ListTransactionsOperation:
    """List one page of PayPal transactions in a date range."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("PayPal client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_TRANSACTIONS_DEFINITION

    async def invoke(self, call: Call, input: ListTransactionsInput) -> QueryAttempt[TransactionPage]:
        """Validate the window, read once, then select a branch or safe retry."""
        operation = LIST_TRANSACTIONS_OPERATION_ID
        if not isinstance(input, ListTransactionsInput) or not _is_valid_transaction_window(input):
            return _failed(
                operation,
                DEFECT,
                TransactionPage(),
                FailureKind.VALIDATION,
                "startDate and endDate must be RFC 3339, ordered within 31 days, and page nonnegative",
            )
        query = {"start_date": input.start_date, "end_date": input.end_date, "fields": "transaction_info"}
        if input.page > 0:
            query["page"] = str(input.page)
        return await self.client.execute_read(
            call,
            operation,
            "/v1/reporting/transactions",
            query,
            TransactionPage(),
            LISTED,
            _decode_transaction_page,
        )


def is_valid_path_segment(value: object) -> bool:
    """Accept one non-dot path segment without separators or control characters."""
    return (
        isinstance(value, str)
        and value not in ("", ".", "..")
        and not any(character in "/?#" for character in value)
        and all(ord(character) >= 32 for character in value)
    )


def _is_valid_transaction_window(input: ListTransactionsInput) -> bool:
    start = _parse_rfc3339(input.start_date)
    end = _parse_rfc3339(input.end_date)
    if start is None or end is None or type(input.page) is not int or input.page < 0:
        return False
    return start <= end and end - start <= _MAX_TRANSACTION_WINDOW


def _parse_rfc3339(value: object) -> datetime | None:
    """Parse Go's time.RFC3339 layout: a T separator and a Z or numeric zone are required."""
    if not isinstance(value, str) or len(value) < 20 or value[10] != "T":
        return None
    if not (value.endswith("Z") or value[-6] in "+-"):
        return None
    try:
        parsed = datetime.fromisoformat(value)
    except ValueError:
        return None
    return parsed if parsed.tzinfo is not None else None


def _decode_access_token(body: bytes) -> tuple[str, int] | None:
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    if not isinstance(document, dict):
        return None
    access_token = _optional(document, "access_token", "")
    token_type = _optional(document, "token_type", "")
    expires_in = _optional(document, "expires_in", 0)
    if (
        not isinstance(access_token, str)
        or not isinstance(token_type, str)
        or type(expires_in) is not int
        or not access_token
        or not is_header_safe_credential(access_token)
        or token_type.lower() != "bearer"
    ):
        return None
    return access_token, expires_in


def _decode_order(document: Any) -> Order:
    document = _object(document)
    order_id = _string(document, "id")
    if not order_id:
        raise ValueError
    return Order(order_id, _string(document, "status"))


def _decode_transaction_page(document: Any) -> TransactionPage:
    document = _object(document or {})
    details = _optional(document, "transaction_details", [])
    page = _optional(document, "page", 0)
    total_pages = _optional(document, "total_pages", 0)
    if not isinstance(details, list) or type(page) is not int or type(total_pages) is not int:
        raise ValueError
    return TransactionPage(tuple(_decode_transaction_detail(detail) for detail in details), page, total_pages)


def _decode_transaction_detail(document: Any) -> TransactionDetail:
    info = _object(_optional(_object(document or {}), "transaction_info", {}))
    amount = _object(_optional(info, "transaction_amount", {}))
    return TransactionDetail(
        TransactionInfo(
            _string(info, "transaction_id"),
            _string(info, "transaction_status"),
            TransactionAmount(_string(amount, "currency_code"), _string(amount, "value")),
        )
    )


def _authorization_failure(
    operation: str,
    kind: FailureKind,
    message: str,
    branch: str = "",
    is_retry: bool = False,
    retry_after_seconds: float = 0,
) -> _AuthorizationFailure:
    return _AuthorizationFailure(
        Failure(kind, CONNECTOR_ID, operation, message), branch, is_retry, retry_after_seconds
    )


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


def _optional(document: dict[str, Any], name: str, default: Any) -> Any:
    """Return a JSON field, treating absent or null like Go's zero value."""
    value = document.get(name)
    return default if value is None else value


def _string(document: dict[str, Any], name: str) -> str:
    value = _optional(document, name, "")
    if not isinstance(value, str):
        raise ValueError
    return value
