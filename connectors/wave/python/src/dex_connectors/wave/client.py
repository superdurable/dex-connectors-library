# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Wave GraphQL customer lookup with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

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
from ._generated.definitions import GET_CUSTOMER_DEFINITION
from .models import CustomerProfile, GetCustomerInput

_GET_CUSTOMER_GRAPHQL = (
    "query GetCustomer($businessId: ID!, $customerId: ID!) "
    "{ business(id: $businessId) { customer(id: $customerId) { id name } } }"
)


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "wave", "getCustomer", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[CustomerProfile]:
    return QueryAttempt.branch_result(branch, CustomerProfile(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[CustomerProfile]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_graphql_id(value: object) -> bool:
    if not isinstance(value, str) or not value or value.strip() != value:
        return False
    try:
        return len(value.encode("utf-8")) <= 128 and all(
            ord(char) >= 0x20 and ord(char) != 0x7F for char in value
        )
    except UnicodeEncodeError:
        return False


def _decode_customer(body: bytes, input: GetCustomerInput) -> CustomerProfile | None:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or raw.get("errors") or not isinstance(raw.get("data"), dict):
        raise ValueError("GraphQL data is missing or contains errors")
    business = raw["data"].get("business")
    if business is None:
        return None
    if not isinstance(business, dict):
        raise ValueError("business is invalid")
    customer = business.get("customer")
    if customer is None:
        return None
    if (
        not isinstance(customer, dict)
        or customer.get("id") != input.customer_id
        or not isinstance(customer.get("name"), str)
        or not customer["name"]
    ):
        raise ValueError("customer identity or name is invalid")
    return CustomerProfile(input.business_id, customer["id"], customer["name"])


class Client:
    """Resolve a Wave Full Access Token per call and read customers."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Wave configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Wave request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_customer(self) -> GetCustomerOperation:
        """Bind the getCustomer Query to this client."""
        return GetCustomerOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, input: GetCustomerInput) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            access_token = credential.access_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(access_token):
            return _Exchange(error="credential")
        request_body = {
            "query": _GET_CUSTOMER_GRAPHQL,
            "variables": {"businessId": input.business_id, "customerId": input.customer_id},
        }
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + "/graphql/public",
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "Authorization": "Bearer " + access_token,
                },
                json=request_body,
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


class GetCustomerOperation:
    """Read one customer by GraphQL ID from one Wave business."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query worker."""
        return GET_CUSTOMER_DEFINITION

    async def invoke(self, call: Call, input: GetCustomerInput) -> QueryAttempt[CustomerProfile]:
        """Classify provider outcomes consistently with Go."""
        if (
            not isinstance(input, GetCustomerInput)
            or not _valid_graphql_id(input.business_id)
            or not _valid_graphql_id(input.customer_id)
        ):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "businessId and customerId must each contain 1–128 printable characters",
            )
        exchange = await self.client._get(call, input)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Wave credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Wave request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Wave rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Wave is temporarily unavailable", exchange.retry_after)
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Wave rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Wave response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Wave response was interrupted")
        try:
            profile = _decode_customer(exchange.body, input)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Wave returned invalid customer JSON")
        if profile is None:
            return _branch("notFound", FailureKind.NOT_FOUND, "Wave did not expose the customer")
        return QueryAttempt.branch_result("found", profile, Receipt())
