# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Xingyapi provider transport and Go-compatible Query decisions."""

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
from ._generated.definitions import GET_INSTANCE_ONLINE_STATUS_DEFINITION
from .models import GetInstanceOnlineStatusInput, GetInstanceOnlineStatusOutput


def _opaque(value: str, limit: int = 1024) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= limit and all(0x20 < ord(char) < 0x7F for char in value)
    )


class Client:
    """Resolve credentials for each getInstanceOnlineStatus request."""

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
            raise ValueError("Xingyapi configuration and credentials are required")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def get_instance_online_status(self) -> "GetInstanceOnlineStatusOperation":
        """Bind the getInstanceOnlineStatus Query."""
        return GetInstanceOnlineStatusOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class GetInstanceOnlineStatusOperation:
    """Query Xingyapi and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return GET_INSTANCE_ONLINE_STATUS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[GetInstanceOnlineStatusOutput]:
        return QueryAttempt.branch_result(
            branch,
            GetInstanceOnlineStatusOutput(),
            Receipt(),
            Failure(kind, "xingyapi", "getInstanceOnlineStatus", "Xingyapi request failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[GetInstanceOnlineStatusOutput]:
        return QueryAttempt.retry(
            Failure(kind, "xingyapi", "getInstanceOnlineStatus", "Xingyapi request failed"), delay
        )

    def _request(
        self, input: GetInstanceOnlineStatusInput, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[GetInstanceOnlineStatusOutput]:
        import re

        if re.fullmatch(r"inst_[A-Za-z0-9_-]{1,128}", input.instance_guid) is None:
            return self._branch("defect", FailureKind.VALIDATION)
        token = credentials.api_key.reveal()
        if not _opaque(token):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        return self.client.http_client.build_request(
            "POST",
            "https://api.xingyapi.com/api/personal/getInfo",
            content=json.dumps({"instance_guid": input.instance_guid}, separators=(",", ":")),
            headers={"Accept": "application/json", "Content-Type": "application/json", "X-Nebula-Key": token},
        )

    def _decode(
        self, wire: object, input: GetInstanceOnlineStatusInput
    ) -> GetInstanceOnlineStatusOutput | QueryAttempt[GetInstanceOnlineStatusOutput] | None:
        import re

        if (
            not isinstance(wire, dict)
            or type(wire.get("ok")) is not bool
            or type(wire.get("code")) is not int
        ):
            return None
        code = wire["code"]
        if (
            code == 50001
            and wire["ok"] is False
            and wire.get("message") == "no account is currently logged in on this device"
        ):
            return GetInstanceOnlineStatusOutput(is_online=False)
        if code == 50000:
            return self._retry(FailureKind.AVAILABILITY)
        if code != 0:
            kind = (
                FailureKind.AUTHENTICATION
                if code == 401
                else FailureKind.AUTHORIZATION
                if code == 40301
                else FailureKind.PROVIDER_REJECTION
            )
            return self._branch("providerRejected", kind)
        data = wire.get("data")
        if wire["ok"] is not True or not isinstance(data, dict):
            return None
        account_id = data.get("uin")
        if type(account_id) is int:
            account_id = str(account_id)
        if not isinstance(account_id, str) or re.fullmatch(r"[0-9]{1,20}", account_id) is None:
            return None
        return GetInstanceOnlineStatusOutput(
            is_online=account_id != "0", account_id="" if account_id == "0" else account_id
        )

    async def invoke(
        self, call: Call, input: GetInstanceOnlineStatusInput
    ) -> QueryAttempt[GetInstanceOnlineStatusOutput]:
        """Read one bounded provider response without persisting credentials."""
        if not isinstance(input, GetInstanceOnlineStatusInput):
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
        return QueryAttempt.branch_result("checked", result, Receipt())
