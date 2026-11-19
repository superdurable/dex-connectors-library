# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider Query implementation with Go-compatible classifications."""

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
from ._generated.definitions import (
    DEFECT,
    INVALID_RESPONSE,
    LIST_WORKSHEETS_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
)
from .models import ListWorksheetsInput, ListWorksheetsOutput, Worksheet


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
            raise ValueError("Mingdao configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def list_worksheets(self) -> "ListWorksheetsOperation":
        """Bind the read-only provider Query."""
        return ListWorksheetsOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class ListWorksheetsOperation:
    """Read provider data with typed Go-compatible outcomes."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Mingdao client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable Query and branch contract."""
        return LIST_WORKSHEETS_DEFINITION

    async def invoke(self, call: Call, input: ListWorksheetsInput) -> QueryAttempt[ListWorksheetsOutput]:
        """Classify one bounded provider request and discard secret response fields."""

        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Mingdao credentials are unavailable or invalid"
            )
        if (
            not isinstance(credentials, Credentials)
            or not _valid_secret(credentials.app_key.reveal())
            or not _valid_secret(credentials.sign.reveal())
        ):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Mingdao credentials are unavailable or invalid"
            )
        endpoint = "https://api.mingdao.com/v3/app/worksheets/list"
        headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
            "HAP-Appkey": credentials.app_key.reveal(),
            "HAP-Sign": credentials.sign.reveal(),
        }
        try:
            request = self.client.http_client.build_request(
                "POST", endpoint, headers=headers, content=b'{"responseFormat":"json"}'
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Mingdao request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Mingdao rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Mingdao service is temporarily unavailable", retry_after
                )

            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"Mingdao rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Mingdao response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Mingdao response was interrupted")
            try:
                data: Any = json.loads(
                    body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid JSON number"))
                )
                result = _decode(data, input)
            except (ValueError, TypeError, KeyError, OverflowError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Mingdao returned invalid data")
            if isinstance(result, QueryAttempt):
                return result
            return QueryAttempt.branch_result(LISTED, result, Receipt())
        finally:
            await response.aclose()


def _valid_secret(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 1024 and all(32 < ord(c) < 127 for c in value)


def _decode(
    data: Any, input: ListWorksheetsInput
) -> ListWorksheetsOutput | QueryAttempt[ListWorksheetsOutput]:
    if not isinstance(data, dict) or type(data.get("success")) is not bool:
        raise ValueError("invalid envelope")
    code = data.get("error_code")
    if not data["success"]:
        if code in (51, 90000):
            return _retry(FailureKind.RATE_LIMIT, "Mingdao rate limited the request")
        if code == 99999:
            return _retry(FailureKind.AVAILABILITY, "Mingdao service is temporarily unavailable")
        return _branch(
            PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, "Mingdao rejected the worksheet-list request"
        )
    rows = data.get("data")
    if (code is not None and (type(code) is not int or code != 1)) or not isinstance(rows, list):
        raise ValueError("invalid worksheet list")
    worksheets = []
    for row in rows:
        if (
            not isinstance(row, dict)
            or not isinstance(row.get("id"), str)
            or not row["id"]
            or not isinstance(row.get("name"), str)
            or not row["name"]
        ):
            raise ValueError("invalid worksheet")
        worksheets.append(Worksheet(row["id"], row["name"]))
    return ListWorksheetsOutput(tuple(worksheets))


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListWorksheetsOutput]:
    return QueryAttempt.branch_result(
        branch, ListWorksheetsOutput(), Receipt(), Failure(kind, "mingdao", "listWorksheets", message)
    )


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[ListWorksheetsOutput]:
    return QueryAttempt.retry(Failure(kind, "mingdao", "listWorksheets", message), retry_after)
