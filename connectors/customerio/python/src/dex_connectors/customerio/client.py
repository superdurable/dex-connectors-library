# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Customer.io customer lookup with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from email.utils import parseaddr
from typing import Any
from urllib.parse import urlencode

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
from ._generated.definitions import GET_CUSTOMERS_BY_EMAIL_DEFINITION
from .models import CustomerLookupPage, GetCustomersByEmailInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "customerio", "getCustomersByEmail", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[CustomerLookupPage]:
    return QueryAttempt.branch_result(branch, CustomerLookupPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[CustomerLookupPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_email(email: object) -> bool:
    if not isinstance(email, str) or not email or email.strip() != email:
        return False
    try:
        byte_count = len(email.encode("utf-8"))
    except UnicodeEncodeError:
        return False
    return (
        byte_count <= 320
        and parseaddr(email)[1] == email
        and "@" in email
        and not any(ord(character) < 0x20 or ord(character) == 0x7F for character in email)
    )


def _decode_customers(body: bytes, email: str) -> CustomerLookupPage:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("results"), list):
        raise ValueError("customer results are missing")
    customer_ids: list[str] = []
    for result in raw["results"]:
        if not isinstance(result, dict) or not isinstance(result.get("cio_id"), str) or not result["cio_id"]:
            raise ValueError("customer ID is missing")
        customer_ids.append(result["cio_id"])
    return CustomerLookupPage(email, tuple(customer_ids))


class Client:
    """Resolve a private key per call and look up customers."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Customer.io configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Customer.io request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_customers_by_email(self) -> GetCustomersByEmailOperation:
        """Bind the getCustomersByEmail Query to this client."""
        return GetCustomersByEmailOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, email: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            private_key = credential.private_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(private_key):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/v1/customers?" + urlencode({"email": email}),
                headers={"Accept": "application/json", "Authorization": "Bearer " + private_key},
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


class GetCustomersByEmailOperation:
    """Look up Customer.io profile IDs by exact email address."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query worker."""
        return GET_CUSTOMERS_BY_EMAIL_DEFINITION

    async def invoke(self, call: Call, input: GetCustomersByEmailInput) -> QueryAttempt[CustomerLookupPage]:
        """Classify customer lookup outcomes consistently with Go."""
        if not isinstance(input, GetCustomersByEmailInput) or not _valid_email(input.email):
            return _branch(
                "defect", FailureKind.VALIDATION, "email must be one valid address of at most 320 bytes"
            )
        exchange = await self.client._get(call, input.email)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Customer.io credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Customer.io request failed")
        if exchange.status == 429:
            return _retry(
                FailureKind.RATE_LIMIT, "Customer.io rate limited the request", exchange.retry_after
            )
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Customer.io is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Customer.io rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Customer.io response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Customer.io response was interrupted")
        try:
            page = _decode_customers(exchange.body, input.email)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Customer.io returned invalid customer-lookup JSON"
            )
        return QueryAttempt.branch_result("found", page, Receipt())
