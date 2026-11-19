# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zhihu API quota Query with Go-compatible outcomes."""

import ipaddress
import json
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any, cast
from urllib.parse import urlsplit

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
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_QUOTAS_DEFINITION
from .models import ListQuotasInput, Quota, QuotaPage


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[QuotaPage]:
    return QueryAttempt.branch_result(
        branch, QuotaPage(), Receipt(), Failure(kind, "zhihu", "listQuotas", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[QuotaPage]:
    return QueryAttempt.retry(Failure(kind, "zhihu", "listQuotas", message), delay)


def _validated_base_url(value: str) -> str:
    if not value or any(ord(character) <= 0x20 or ord(character) > 0x7E for character in value):
        raise ValueError("Zhihu base URL must be printable ASCII")
    try:
        parsed = urlsplit(value)
        host = parsed.hostname
    except ValueError as error:
        raise ValueError("Zhihu base URL is invalid") from error
    if not host or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError("Zhihu base URL is invalid")
    if parsed.scheme != "https":
        try:
            is_loopback = host == "localhost" or ipaddress.ip_address(host).is_loopback
        except ValueError:
            is_loopback = host == "localhost"
        if parsed.scheme != "http" or not is_loopback:
            raise ValueError("Zhihu base URL must use HTTPS")
    return value.rstrip("/")


class Client:
    """Own Zhihu settings and resolve the Access Secret for each Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("configuration and credentials are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Zhihu timeout and response limit must be positive")
        self.base_url = _validated_base_url(config.base_url)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def list_quotas(self) -> "ListQuotasOperation":
        """Bind the listQuotas Query."""
        return ListQuotasOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class ListQuotasOperation:
    """Read the account's enabled API quota page."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("provider client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return LIST_QUOTAS_DEFINITION

    async def invoke(self, call: Call, input: ListQuotasInput) -> QueryAttempt[QuotaPage]:
        """Make one read-only provider request."""
        try:
            credentials = await self.client.credentials.resolve(call)
            secret = credentials.access_secret.reveal()
            if (
                not secret
                or len(secret) > 2048
                or secret.strip() != secret
                or secret.lower().startswith("bearer ")
                or any(ord(character) <= 0x20 or ord(character) >= 0x7F for character in secret)
            ):
                raise ValueError("invalid secret")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Zhihu connection secret is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/api/v1/quota",
                headers={
                    "Accept": "application/json",
                    "Authorization": f"Bearer {secret}",
                    "X-Request-Timestamp": str(int(self.client.clock().timestamp())),
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "quota request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Zhihu request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), self.client.clock())
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Zhihu rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Zhihu is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Zhihu response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Zhihu response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.VALIDATION
                if status in (400, 404)
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, f"Zhihu rejected the query with HTTP {status}")
        return _decode_quotas(body, delay)


def _decode_quotas(body: bytes, delay: float) -> QueryAttempt[QuotaPage]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Zhihu returned an invalid quota response")
    if not isinstance(document, dict) or type(document.get("Code")) is not int:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Zhihu returned an invalid quota response")
    code = document["Code"]
    if code == 30001:
        return _retry(FailureKind.RATE_LIMIT, "Zhihu rate limited the request", delay)
    if code == 90001:
        return _retry(FailureKind.AVAILABILITY, "Zhihu quota data is temporarily unavailable", delay)
    if code == 20001:
        return _branch("providerRejected", FailureKind.AUTHENTICATION, "Zhihu rejected the Access Secret")
    if code == 10001:
        return _branch("providerRejected", FailureKind.VALIDATION, "Zhihu rejected the quota request")
    if code != 0:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"Zhihu rejected the query with code {code}"
        )
    entries = document.get("Data")
    if not isinstance(entries, list) or len(entries) > 100:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Zhihu returned an invalid quota response")
    quotas = []
    for entry in entries:
        if not isinstance(entry, dict):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Zhihu returned an invalid quota response"
            )
        api_id, api_name = entry.get("APIID"), entry.get("APIName")
        total_quota = entry.get("TotalQuota")
        total_used = entry.get("TotalUsed")
        remaining_quota = entry.get("RemainingQuota")
        if (
            not isinstance(api_id, str)
            or not api_id
            or not isinstance(api_name, str)
            or not api_name
            or any(
                type(value) is not int or value < 0 for value in (total_quota, total_used, remaining_quota)
            )
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Zhihu returned an invalid quota response"
            )
        quotas.append(
            Quota(api_id, api_name, cast(int, total_quota), cast(int, total_used), cast(int, remaining_quota))
        )
    return QueryAttempt.branch_result("listed", QuotaPage(tuple(quotas)), Receipt())
