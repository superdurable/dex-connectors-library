# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Yuandian provider transport and Go-compatible Query decisions."""

import json
from datetime import UTC, datetime

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


def _opaque(value: str, limit: int = 1024) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= limit and all(0x20 < ord(char) < 0x7F for char in value)
    )


class Client:
    """Resolve credentials for each getCompanyProfile request."""

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
            raise ValueError("Yuandian configuration and credentials are required")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def get_company_profile(self) -> "GetCompanyProfileOperation":
        """Bind the getCompanyProfile Query."""
        return GetCompanyProfileOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class GetCompanyProfileOperation:
    """Query Yuandian and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return GET_COMPANY_PROFILE_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[GetCompanyProfileOutput]:
        return QueryAttempt.branch_result(
            branch,
            GetCompanyProfileOutput(),
            Receipt(),
            Failure(kind, "yuandian", "getCompanyProfile", "Yuandian request failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[GetCompanyProfileOutput]:
        return QueryAttempt.retry(
            Failure(kind, "yuandian", "getCompanyProfile", "Yuandian request failed"), delay
        )

    def _request(
        self, input: GetCompanyProfileInput, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[GetCompanyProfileOutput]:
        if len(input.credit_code) != 18 or any(
            ch not in "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ" for ch in input.credit_code
        ):
            return self._branch("defect", FailureKind.VALIDATION)
        token = credentials.api_key.reveal()
        if not _opaque(token):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        return self.client.http_client.build_request(
            "GET",
            "https://open.chineselaw.com/open/rh_company_detail",
            params={"tyshxydm": input.credit_code},
            headers={"Accept": "application/json", "X-API-Key": token},
        )

    def _decode(
        self, wire: object, input: GetCompanyProfileInput
    ) -> GetCompanyProfileOutput | QueryAttempt[GetCompanyProfileOutput] | None:
        if not isinstance(wire, dict) or type(wire.get("code")) is not int:
            return None
        code = wire["code"]
        if code == 429:
            return self._retry(FailureKind.RATE_LIMIT)
        if code >= 500:
            return self._retry(FailureKind.AVAILABILITY)
        if code != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if code == 401
                else FailureKind.AUTHORIZATION
                if code == 403
                else FailureKind.PROVIDER_REJECTION
            )
            return self._branch("providerRejected", kind)
        data = wire.get("data")
        if data is None and isinstance(wire.get("message"), str) and "未查询到" in wire["message"]:
            return self._branch("noData", FailureKind.NOT_FOUND)
        if not isinstance(data, dict):
            return None
        fields = {
            "company_id": "企业ID",
            "name": "企业名称",
            "credit_code": "统一社会信用代码",
            "registration_status": "经营状态",
            "industry": "行业",
            "company_type": "企业类型",
        }
        if any(
            not isinstance(data.get(key, ""), str) or len(data.get(key, "")) > limit
            for key, limit in (
                ("企业ID", 255),
                ("企业名称", 255),
                ("经营状态", 100),
                ("行业", 255),
                ("企业类型", 255),
            )
        ):
            return None
        if (
            not data.get("企业ID")
            or not data.get("企业名称")
            or data.get("统一社会信用代码") != input.credit_code
        ):
            return None
        return GetCompanyProfileOutput(**{field: data.get(key, "") for field, key in fields.items()})

    async def invoke(
        self, call: Call, input: GetCompanyProfileInput
    ) -> QueryAttempt[GetCompanyProfileOutput]:
        """Read one bounded provider response without persisting credentials."""
        if not isinstance(input, GetCompanyProfileInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        request = self._request(input, credentials)
        if isinstance(request, QueryAttempt):
            return request
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        result = self._decode(wire, input)
        if isinstance(result, QueryAttempt):
            return result
        if result is None:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("found", result, Receipt())
