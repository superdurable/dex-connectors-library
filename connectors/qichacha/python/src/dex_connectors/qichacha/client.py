# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Qichacha signed enterprise lookup and Go-compatible outcomes."""

import hashlib
import json
from collections.abc import Callable
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
from ._generated.definitions import GET_ENTERPRISE_BASIC_DETAILS_DEFINITION
from .models import GetEnterpriseBasicDetailsInput, GetEnterpriseBasicDetailsOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetEnterpriseBasicDetailsOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetEnterpriseBasicDetailsOutput(),
        Receipt(),
        Failure(kind, "qichacha", "getEnterpriseBasicDetails", message),
    )


def _retry(
    kind: FailureKind, message: str, delay: float = 0
) -> QueryAttempt[GetEnterpriseBasicDetailsOutput]:
    return QueryAttempt.retry(Failure(kind, "qichacha", "getEnterpriseBasicDetails", message), delay)


def _valid_credential(value: str) -> bool:
    return bool(value) and len(value) <= 512 and all(0x20 < ord(character) < 0x7F for character in value)


class Client:
    """Own a bounded transport and resolve rotated credentials for every request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("Qichacha configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.base_url = config.base_url.rstrip("/")
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def get_enterprise_basic_details(self) -> "GetEnterpriseBasicDetailsOperation":
        """Bind the enterprise registration Query."""
        return GetEnterpriseBasicDetailsOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetEnterpriseBasicDetailsOperation:
    """Read mainland enterprise registration data."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Qichacha client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable generated Query definition."""
        return GET_ENTERPRISE_BASIC_DETAILS_DEFINITION

    async def invoke(
        self, call: Call, input: GetEnterpriseBasicDetailsInput
    ) -> QueryAttempt[GetEnterpriseBasicDetailsOutput]:
        """Sign one request and classify its provider response."""
        if (
            not isinstance(input, GetEnterpriseBasicDetailsInput)
            or not input.keyword.strip()
            or input.keyword != input.keyword.strip()
            or len(input.keyword.encode()) > 1000
        ):
            return _branch("defect", FailureKind.VALIDATION, "keyword is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key = credentials.app_key.reveal()
            secret_key = credentials.secret_key.reveal()
            if not _valid_credential(app_key) or not _valid_credential(secret_key):
                raise ValueError("invalid credential")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        timestamp = str(int(self.client.clock().timestamp()))
        signature = hashlib.md5((app_key + timestamp + secret_key).encode()).hexdigest().upper()
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/ECIV4/GetBasicDetailsByName",
                params={"key": app_key, "keyword": input.keyword},
                headers={"Accept": "application/json", "Token": signature, "Timespan": timestamp},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Qichacha request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), self.client.clock())
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "Qichacha rate limited the lookup", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "Qichacha is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Qichacha response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Qichacha response was interrupted")
            if response.status_code != 200:
                return _branch(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Qichacha rejected the request with HTTP {response.status_code}",
                )
            try:
                data: Any = json.loads(
                    body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid JSON number"))
                )
            except (ValueError, UnicodeDecodeError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Qichacha returned invalid JSON")
            if not isinstance(data, dict) or not isinstance(data.get("Status"), str) or not data["Status"]:
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Qichacha response has no status")
            if data["Status"] != "200":
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, "Qichacha rejected the lookup"
                )
            result = data.get("Result")
            if not isinstance(result, dict):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Qichacha response lacks enterprise fields"
                )
            name, status, credit_code, entity_type = (
                result.get("Name"),
                result.get("Status"),
                result.get("CreditCode", ""),
                result.get("EntType", ""),
            )
            if (
                not isinstance(name, str)
                or not name
                or len(name.encode()) > 1000
                or not isinstance(status, str)
                or not status
                or len(status.encode()) > 100
                or not isinstance(credit_code, str)
                or len(credit_code.encode()) > 50
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Qichacha response lacks enterprise fields"
                )
            if entity_type not in {"0", "1", "4", "7", "9", "10", "11"}:
                return _branch(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    "Qichacha returned a non-mainland entity",
                )
            return QueryAttempt.branch_result(
                "fetched", GetEnterpriseBasicDetailsOutput(name, credit_code, status), Receipt()
            )
        finally:
            await response.aclose()
