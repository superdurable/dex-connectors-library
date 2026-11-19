# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""threatbook provider transport and Go-compatible Query decisions."""

import json
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
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import CHECK_IP_REPUTATION_DEFINITION
from .models import CheckIPReputationInput, CheckIPReputationOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[CheckIPReputationOutput]:
    return QueryAttempt.branch_result(
        branch,
        CheckIPReputationOutput(),
        Receipt(),
        Failure(kind, "threatbook", "checkIPReputation", message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[CheckIPReputationOutput]:
    return QueryAttempt.retry(Failure(kind, "threatbook", "checkIPReputation", message), delay)


class Client:
    """Resolve credentials per checkIPReputation request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("threatbook configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("threatbook timeout or response limit is invalid")
        self.config = config
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def check_ip_reputation(self) -> "CheckIPReputationOperation":
        """Bind the checkIPReputation Query to this client."""
        return CheckIPReputationOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class CheckIPReputationOperation:
    """Perform the checkIPReputation read and return bounded provider data."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("threatbook client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return CHECK_IP_REPUTATION_DEFINITION

    async def invoke(
        self, call: Call, input: CheckIPReputationInput
    ) -> QueryAttempt[CheckIPReputationOutput]:
        """Classify transport and provider outcomes without exposing secrets."""
        try:
            credentials = await self.client.credentials.resolve(call)
            request = self._build_request(input, credentials)
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials or request are invalid"
            )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "threatbook request failed")
        return await self._process_response(response, input, credentials)

    async def _process_response(
        self, response: httpx.Response, input: CheckIPReputationInput, credentials: Credentials
    ) -> QueryAttempt[CheckIPReputationOutput]:
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "threatbook rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "threatbook is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "threatbook response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "threatbook response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, f"threatbook rejected request with HTTP {status}")
        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "threatbook returned invalid JSON")
        return self._decode(input, document)

    def _build_request(self, input: CheckIPReputationInput, credentials: Credentials) -> httpx.Request:
        import ipaddress

        address = str(ipaddress.ip_address(input.ip_address))
        key = credentials.api_key.reveal()
        if not key:
            raise ValueError("missing API key")
        return self.client.http_client.build_request(
            "GET",
            "https://api.threatbook.cn/v3/scene/ip_reputation",
            params={"apikey": key, "resource": address, "lang": "en"},
            headers={"Accept": "application/json"},
        )

    def _decode(self, input: CheckIPReputationInput, document: Any) -> QueryAttempt[CheckIPReputationOutput]:
        if not isinstance(document, dict) or type(document.get("response_code")) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid reputation response")
        code = document["response_code"]
        if code == 2:
            return _branch("noData", FailureKind.NOT_FOUND, "no intelligence for IP")
        if code == -4 and document.get("verbose_msg") == "Frequent Limitation":
            return _retry(FailureKind.RATE_LIMIT, "frequency limit reached")
        if code in (-5, -11):
            return _retry(FailureKind.AVAILABILITY, "reputation service unavailable")
        if code != 0:
            return _branch("providerRejected", FailureKind.PROVIDER_REJECTION, "reputation request rejected")
        data = document.get("data")
        info = data.get(input.ip_address) if isinstance(data, dict) and len(data) == 1 else None
        if (
            not isinstance(info, dict)
            or type(info.get("is_malicious")) is not bool
            or not isinstance(info.get("judgments"), list)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid reputation data")
        labels = info["judgments"]
        if len(labels) > 20 or any(
            not isinstance(item, str) or not item or len(item) > 80 for item in labels
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid threat labels")
        return QueryAttempt.branch_result(
            "found",
            CheckIPReputationOutput(
                is_malicious=info["is_malicious"],
                confidence_level=info.get("confidence_level", ""),
                severity=info.get("severity", ""),
                judgments=tuple(labels),
            ),
            Receipt(),
        )
