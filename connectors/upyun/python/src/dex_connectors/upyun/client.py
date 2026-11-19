# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read signed UPYUN service usage with Go-compatible classifications."""

import base64
import hashlib
import hmac
import re
from collections.abc import Callable
from datetime import UTC, datetime
from email.utils import format_datetime

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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_SERVICE_USAGE_DEFINITION
from .models import GetServiceUsageInput, ServiceUsage


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "upyun", "getServiceUsage", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ServiceUsage]:
    return QueryAttempt.branch_result(branch, ServiceUsage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ServiceUsage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Sign each read using current operator credentials and an injectable clock."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("UPYUN configuration and credentials are required")
        if not re.fullmatch(r"[a-z][a-z0-9-]{4,19}", config.bucket):
            raise ValueError("UPYUN bucket is invalid")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.bucket = config.bucket
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def get_service_usage(self) -> "GetServiceUsageOperation":
        """Bind the service usage Query."""
        return GetServiceUsageOperation(self)

    async def aclose(self) -> None:
        """Close a connector-owned transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class GetServiceUsageOperation:
    """Read used bytes for the configured storage service."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query definition."""
        return GET_SERVICE_USAGE_DEFINITION

    async def invoke(self, call: Call, input: GetServiceUsageInput) -> QueryAttempt[ServiceUsage]:
        """Sign the usage request and classify the bounded response."""
        try:
            credentials = await self.client.credentials.resolve(call)
            operator = credentials.operator
            password = credentials.password.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "UPYUN credentials are unavailable")
        if (
            not operator
            or len(operator) > 128
            or operator.strip() != operator
            or any(ord(c) <= 32 or ord(c) >= 127 or c == ":" for c in operator)
            or not password
        ):
            return _branch("defect", FailureKind.AUTHENTICATION, "UPYUN credentials are invalid")
        uri = "/" + self.client.bucket + "/?usage"
        date = format_datetime(self.client.now().astimezone(UTC), usegmt=True)
        key = hashlib.md5(password.encode()).hexdigest().encode()
        signature = base64.b64encode(
            hmac.new(key, ("GET&" + uri + "&" + date).encode(), hashlib.sha1).digest()
        ).decode()
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + uri,
                headers={"Date": date, "Authorization": "UPYUN " + operator + ":" + signature},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.TRANSPORT, "UPYUN request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "UPYUN rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "UPYUN is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "UPYUN usage response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.TRANSPORT, "UPYUN response was interrupted")
            if status != 200:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, f"UPYUN rejected HTTP {status}"
                )
            try:
                amount = int(body.decode("utf-8").strip())
                if amount < 0 or amount > 2**63 - 1:
                    raise ValueError("usage outside int64")
            except (UnicodeDecodeError, ValueError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "UPYUN returned invalid usage bytes")
            return QueryAttempt.branch_result("read", ServiceUsage(bytes=amount), Receipt())
        finally:
            await response.aclose()
