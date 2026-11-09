# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Chargebee subscription reads with Go-compatible outcomes."""

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
from ._generated.definitions import GET_SUBSCRIPTION_DEFINITION
from .models import GetSubscriptionInput, SubscriptionStatus


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "chargebee", "getSubscription", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SubscriptionStatus]:
    return QueryAttempt.branch_result(branch, SubscriptionStatus(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[SubscriptionStatus]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_subscription_id(subscription_id: object) -> bool:
    return (
        isinstance(subscription_id, str)
        and 1 <= len(subscription_id) <= 50
        and all(
            character in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
            for character in subscription_id
        )
    )


def _decode_subscription(body: bytes, requested_subscription_id: str) -> SubscriptionStatus:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("subscription"), dict):
        raise ValueError("subscription data missing")
    data = raw["subscription"]
    if (
        data.get("id") != requested_subscription_id
        or not isinstance(data.get("status"), str)
        or not data["status"].strip()
        or not isinstance(data.get("currency_code"), str)
        or len(data["currency_code"]) != 3
    ):
        raise ValueError("subscription identity, currency, or status missing")
    return SubscriptionStatus(data["id"], data["currency_code"], data["status"])


class Client:
    """Resolve an API key per call and read selected subscription metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Chargebee configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Chargebee request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_subscription(self) -> GetSubscriptionOperation:
        """Bind the getSubscription Query to this client."""
        return GetSubscriptionOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, subscription_id: str) -> _Exchange:
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
                self.base_url + "/subscriptions/" + quote(subscription_id, safe=""),
                headers={
                    "Accept": "application/json",
                    "Authorization": "Basic " + base64.b64encode((api_key + ":").encode()).decode("ascii"),
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


class GetSubscriptionOperation:
    """Read one subscription's ID, status, and currency code."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query subscription."""
        return GET_SUBSCRIPTION_DEFINITION

    async def invoke(self, call: Call, input: GetSubscriptionInput) -> QueryAttempt[SubscriptionStatus]:
        """Classify subscription lookup outcomes consistently with Go."""
        if not isinstance(input, GetSubscriptionInput) or not _valid_subscription_id(input.subscription_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "subscription ID must be 1–50 URL-safe characters",
            )
        exchange = await self.client._get(call, input.subscription_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Chargebee credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Chargebee request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Chargebee rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Chargebee is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Chargebee did not expose the subscription")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Chargebee rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Chargebee response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Chargebee response was interrupted")
        try:
            subscription_status = _decode_subscription(exchange.body, input.subscription_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Chargebee returned invalid subscription JSON"
            )
        return QueryAttempt.branch_result("found", subscription_status, Receipt())
