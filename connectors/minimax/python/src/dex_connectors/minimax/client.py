# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider Query implementation with Go-compatible classifications."""

import json
import math
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    DEFECT,
    FETCHED,
    GET_TOKEN_PLAN_REMAINS_DEFINITION,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
)
from .models import GetTokenPlanRemainsInput, GetTokenPlanRemainsOutput, TokenPlanModelGroup


class Client:
    """Own a bounded transport and resolve private credentials per call."""

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
            raise ValueError("Minimax configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.base_url = validate_base_url(config.base_url)

    def get_token_plan_remains(self) -> "GetTokenPlanRemainsOperation":
        """Bind the read-only provider Query."""
        return GetTokenPlanRemainsOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class GetTokenPlanRemainsOperation:
    """Read provider data with typed Go-compatible outcomes."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Minimax client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable Query and branch contract."""
        return GET_TOKEN_PLAN_REMAINS_DEFINITION

    async def invoke(
        self, call: Call, input: GetTokenPlanRemainsInput
    ) -> QueryAttempt[GetTokenPlanRemainsOutput]:
        """Classify one bounded provider request and discard secret response fields."""

        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Minimax credentials are unavailable or invalid"
            )
        if not isinstance(credentials, Credentials) or not _valid_secret(credentials.api_key.reveal()):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Minimax credentials are unavailable or invalid"
            )
        endpoint = self.client.base_url + "/v1/token_plan/remains"
        headers = {"Accept": "application/json", "Authorization": "Bearer " + credentials.api_key.reveal()}
        try:
            request = self.client.http_client.build_request("GET", endpoint, headers=headers)
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Minimax request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Minimax rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Minimax service is temporarily unavailable", retry_after
                )

            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"Minimax rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Minimax response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Minimax response was interrupted")
            try:
                data: Any = json.loads(
                    body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid JSON number"))
                )
                result = _decode(data, input)
            except (ValueError, TypeError, KeyError, OverflowError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Minimax returned invalid data")
            if isinstance(result, QueryAttempt):
                return result
            return QueryAttempt.branch_result(FETCHED, result, Receipt())
        finally:
            await response.aclose()


def _valid_secret(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 1024 and all(32 < ord(c) < 127 for c in value)


def _decode(
    data: Any, input: GetTokenPlanRemainsInput
) -> GetTokenPlanRemainsOutput | QueryAttempt[GetTokenPlanRemainsOutput]:
    if (
        not isinstance(data, dict)
        or not isinstance(data.get("base_resp"), dict)
        or type(data["base_resp"].get("status_code")) is not int
    ):
        raise ValueError("invalid envelope")
    code = data["base_resp"]["status_code"]
    if code in (1002, 1039):
        return _retry(FailureKind.RATE_LIMIT, "MiniMax rate limited the Token Plan request")
    if code != 0:
        kind = (
            FailureKind.AUTHENTICATION
            if code == 1004
            else FailureKind.QUOTA_EXHAUSTED
            if code == 1008
            else FailureKind.PROVIDER_REJECTION
        )
        return _branch(PROVIDER_REJECTED, kind, f"MiniMax rejected Token Plan usage with code {code}")
    rows = data.get("model_remains")
    if not isinstance(rows, list) or len(rows) > 1000:
        raise ValueError("invalid groups")
    groups = []
    seen: set[str] = set()
    for row in rows:
        if not isinstance(row, dict):
            raise ValueError("invalid group")
        name, interval_end, weekly_end = (
            row.get("model_name"),
            row.get("end_time", 0),
            row.get("weekly_end_time", 0),
        )
        if (
            not isinstance(name, str)
            or not name
            or len(name.encode()) > 256
            or name in seen
            or type(interval_end) is not int
            or interval_end < 0
            or type(weekly_end) is not int
            or weekly_end < 0
        ):
            raise ValueError("invalid group")
        seen.add(name)
        interval = row.get("current_interval_remaining_percent")
        weekly = row.get("current_weekly_remaining_percent")
        if any(
            value is not None
            and (type(value) not in (int, float) or not math.isfinite(value) or not 0 <= value <= 1000)
            for value in (interval, weekly)
        ):
            raise ValueError("invalid percentage")
        interval_status = row.get("current_interval_status")
        weekly_status = row.get("current_weekly_status")
        if any(value is not None and type(value) is not int for value in (interval_status, weekly_status)):
            raise ValueError("invalid status")
        groups.append(
            TokenPlanModelGroup(
                name,
                float(interval) if interval is not None else None,
                float(weekly) if weekly is not None else None,
                interval_end,
                weekly_end,
                interval_status,
                weekly_status,
            )
        )
    return GetTokenPlanRemainsOutput(tuple(groups))


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetTokenPlanRemainsOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetTokenPlanRemainsOutput(),
        Receipt(),
        Failure(kind, "minimax", "getTokenPlanRemains", message),
    )


def _retry(
    kind: FailureKind, message: str, retry_after: float = 0
) -> QueryAttempt[GetTokenPlanRemainsOutput]:
    return QueryAttempt.retry(Failure(kind, "minimax", "getTokenPlanRemains", message), retry_after)
