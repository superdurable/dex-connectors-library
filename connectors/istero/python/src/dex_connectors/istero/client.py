# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""IsTero account transport and Go-compatible decisions."""

import json
from datetime import UTC, datetime
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
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_ACCOUNT_STATUS_DEFINITION
from .models import GetAccountStatusInput, GetAccountStatusOutput

_ENDPOINT = "https://api.istero.com/resource/v1/isas/mine/information"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetAccountStatusOutput]:
    return QueryAttempt.branch_result(
        branch, GetAccountStatusOutput(), Receipt(), Failure(kind, "istero", "getAccountStatus", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetAccountStatusOutput]:
    return QueryAttempt.retry(Failure(kind, "istero", "getAccountStatus", message), delay)


class Client:
    """Resolve IsTero credentials for each Query and own a redirect-safe HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("IsTero configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("IsTero timeout or response limit is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_account_status(self) -> "GetAccountStatusOperation":
        """Bind the account-status Query to this client."""
        return GetAccountStatusOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetAccountStatusOperation:
    """Read usage and remaining allowance for one ISAS account."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("IsTero client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated IsTero Query identity."""
        return GET_ACCOUNT_STATUS_DEFINITION

    async def invoke(self, call: Call, input: GetAccountStatusInput) -> QueryAttempt[GetAccountStatusOutput]:
        """Read account usage and classify the ISAS provider response."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            isid = credentials.isid
            if not api_key or len(api_key) > 512 or any(not 0x20 < ord(c) < 0x7F for c in api_key):
                raise ValueError("invalid key")
            if not 1 <= len(isid) <= 18 or isid.startswith("0") or not isid.isascii() or not isid.isdigit():
                raise ValueError("invalid ISID")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "ISAS credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                _ENDPOINT + "?" + urlencode({"isid": isid}),
                headers={"Accept": "application/json", "Authorization": "Bearer " + api_key},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "account request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "account request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "provider rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "provider temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "account response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "account response was interrupted")
        finally:
            await response.aclose()
        return _decode_account(body, status, isid, delay)


def _decode_account(
    body: bytes, status: int, isid: str, delay: float
) -> QueryAttempt[GetAccountStatusOutput]:
    try:
        wire: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "provider returned an invalid account response"
        )
    if not isinstance(wire, dict) or type(wire.get("code")) is not int:
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "provider returned an invalid account response"
        )
    code = wire["code"]
    if code == 429:
        return _retry(FailureKind.RATE_LIMIT, "provider rate limited the request", delay)
    if code == 500:
        return _retry(FailureKind.AVAILABILITY, "provider temporarily unavailable", delay)
    if code == 403:
        return _branch("providerRejected", FailureKind.AUTHENTICATION, "provider rejected the key")
    if code == 502:
        return _branch("providerRejected", FailureKind.AUTHORIZATION, "provider denied account access")
    if code != 200:
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"provider rejected the account request with code {code}",
        )
    data = wire.get("data")
    if (
        status != 200
        or not isinstance(data, dict)
        or any(type(data.get(field)) is not int for field in ("isid", "today_used", "today_remaining"))
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "provider returned invalid account fields")
    if str(data["isid"]) != isid or data["today_used"] < 0 or data["today_remaining"] < 0:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "provider returned invalid account fields")
    return QueryAttempt.branch_result(
        "retrieved",
        GetAccountStatusOutput(
            isid=data["isid"],
            today_used=data["today_used"],
            today_remaining=data["today_remaining"],
        ),
        Receipt(),
    )
