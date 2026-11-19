# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""tianyancha provider Query and Go-compatible outcome classification."""

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
from ._generated.definitions import GET_COMPANY_PROFILE_DEFINITION
from .models import GetCompanyProfileInput, GetCompanyProfileOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetCompanyProfileOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetCompanyProfileOutput(),
        Receipt(),
        Failure(kind, "tianyancha", "getCompanyProfile", message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetCompanyProfileOutput]:
    return QueryAttempt.retry(Failure(kind, "tianyancha", "getCompanyProfile", message), delay)


class Client:
    """Own a bounded provider transport and resolve rotated credentials per call."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("tianyancha configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.endpoint = "https://open.api.tianyancha.com/services/v4/open/baseinfoV3"
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_company_profile(self) -> "GetCompanyProfileOperation":
        """Bind the read-only provider Query."""
        return GetCompanyProfileOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetCompanyProfileOperation:
    """Read the provider endpoint without changing provider state."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("tianyancha client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable generated Query definition."""
        return GET_COMPANY_PROFILE_DEFINITION

    async def invoke(
        self, call: Call, input: GetCompanyProfileInput
    ) -> QueryAttempt[GetCompanyProfileOutput]:
        """Make one bounded request and classify its provider outcome."""
        if not isinstance(input, GetCompanyProfileInput):
            return _branch("defect", FailureKind.VALIDATION, "provider input is invalid")

        if (
            not input.keyword
            or input.keyword != input.keyword.strip()
            or len(input.keyword) > 255
            or any(c in input.keyword for c in "\r\n\x00")
        ):
            return _branch("defect", FailureKind.VALIDATION, "Tianyancha requires one valid company keyword")
        params = {"keyword": input.keyword}
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if (
                not key
                or len(key) > 2048
                or key != key.strip()
                or any(ord(c) <= 0x20 or ord(c) >= 0x7F for c in key)
            ):
                raise ValueError("invalid credential")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "provider credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.endpoint,
                params=params,
                headers={"Accept": "application/json", "Authorization": "" + key},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "provider request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "provider rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "provider temporarily unavailable", delay)
            if response.status_code != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if response.status_code == 401
                    else FailureKind.AUTHORIZATION
                    if response.status_code == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(
                    "providerRejected",
                    kind,
                    f"provider rejected the request with HTTP {response.status_code}",
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "provider response was interrupted")
            try:
                data: Any = json.loads(
                    body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid JSON number"))
                )
            except (ValueError, UnicodeDecodeError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "provider returned invalid JSON")
            try:
                return _decode(data, delay)
            except (ValueError, TypeError, KeyError, OverflowError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "provider returned invalid data")
        finally:
            await response.aclose()


def _decode(data: Any, delay: float) -> QueryAttempt[GetCompanyProfileOutput]:
    """Validate and reduce the provider response to safe result fields."""
    if not isinstance(data, dict):
        raise ValueError("invalid provider response")

    if type(data.get("error_code")) is not int:
        raise ValueError("invalid error code")
    code = data["error_code"]
    if code == 300000:
        return _branch("noData", FailureKind.NOT_FOUND, "Tianyancha found no matching company")
    if code == 300004:
        return _retry(FailureKind.RATE_LIMIT, "Tianyancha frequency limit reached", delay)
    if code in (300001, 300012):
        return _retry(
            FailureKind.AVAILABILITY, "Tianyancha company service is temporarily unavailable", delay
        )
    if code != 0:
        kind = (
            FailureKind.AUTHENTICATION
            if code in (300002, 300003, 300009)
            else FailureKind.AUTHORIZATION
            if code in (300005, 300011)
            else FailureKind.PROVIDER_REJECTION
        )
        return _branch("providerRejected", kind, f"Tianyancha rejected company request with code {code}")
    result = data.get("result")
    if (
        not isinstance(result, dict)
        or type(result.get("id")) is not int
        or result["id"] <= 0
        or not isinstance(result.get("name"), str)
        or not result["name"]
        or len(result["name"]) > 255
    ):
        raise ValueError("invalid company profile")
    fields = (
        ("regStatus", 100),
        ("creditCode", 100),
        ("industry", 255),
        ("regCapital", 100),
        ("companyOrgType", 255),
    )
    if any(
        not isinstance(result.get(field, ""), str) or len(result.get(field, "")) > limit
        for field, limit in fields
    ):
        raise ValueError("invalid company profile")
    return QueryAttempt.branch_result(
        "found",
        GetCompanyProfileOutput(
            company_id=result["id"],
            name=result["name"],
            registration_status=result.get("regStatus", ""),
            credit_code=result.get("creditCode", ""),
            industry=result.get("industry", ""),
            registered_capital=result.get("regCapital", ""),
            company_type=result.get("companyOrgType", ""),
        ),
        Receipt(),
    )
