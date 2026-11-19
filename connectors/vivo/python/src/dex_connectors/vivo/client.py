# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider transport and Go-compatible Query decisions."""

import hashlib
import hmac
import json
import re
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
from ._generated.definitions import GET_ONLINE_VERSION_DEFINITION
from .models import GetOnlineVersionInput, GetOnlineVersionOutput

_PROVIDER = "vivo"
_OPERATION = "getOnlineVersion"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetOnlineVersionOutput]:
    return QueryAttempt.branch_result(
        branch, GetOnlineVersionOutput(), Receipt(), Failure(kind, _PROVIDER, _OPERATION, message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetOnlineVersionOutput]:
    return QueryAttempt.retry(Failure(kind, _PROVIDER, _OPERATION, message), delay)


class Client:
    """Own the provider HTTP transport and resolve rotating credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("vivo configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("vivo configuration is invalid")

        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

        self.clock = clock or (lambda: datetime.now(UTC))

    def get_online_version(self) -> "GetOnlineVersionOperation":
        """Bind the getOnlineVersion Query."""
        return GetOnlineVersionOperation(self)

    async def aclose(self) -> None:
        """Close this client's own transport."""
        if self._owns_client:
            await self.http_client.aclose()


class GetOnlineVersionOperation:
    """Run the getOnlineVersion provider Query."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("vivo client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity."""
        return GET_ONLINE_VERSION_DEFINITION

    async def invoke(self, call: Call, input: GetOnlineVersionInput) -> QueryAttempt[GetOnlineVersionOutput]:
        """Send one request and classify its provider outcome."""
        prepared = await _prepare(self.client, call, input)
        if isinstance(prepared, QueryAttempt):
            return prepared
        try:
            response, _ = await send_traced(self.client.http_client, prepared)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "vivo version request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "vivo rate limited version request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "vivo API is temporarily unavailable", delay)
            if status != 200 and not False:
                return _http_rejection(status)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "vivo response exceeded configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "vivo response was interrupted")
            if status != 200:
                return _http_rejection(status)
        finally:
            await response.aclose()
        return _decode(body, input, delay)


def _valid_credential(value: str) -> bool:
    return bool(value) and len(value) <= 1024 and all(33 <= ord(c) <= 126 for c in value)


async def _prepare(
    client: Client, call: Call, input: GetOnlineVersionInput
) -> httpx.Request | QueryAttempt[GetOnlineVersionOutput]:
    if len(input.package_name) > 255 or not re.fullmatch(
        r"[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+", input.package_name
    ):
        return _branch("defect", FailureKind.VALIDATION, "vivo package name is invalid")
    try:
        credentials = await client.credentials.resolve(call)
        account = credentials.account.reveal()
        secret = credentials.secret.reveal()
        if not _valid_credential(account) or not _valid_credential(secret):
            raise ValueError("invalid credentials")
    except (ValueError, LookupError, OSError):
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "vivo developer credentials are unavailable or invalid"
        )
    timestamp = int(client.clock().timestamp() * 1000)
    signature_input = (
        f'account={account}&bizParam={{"packageName":"{input.package_name}"}}&timestamp={timestamp}'
    )
    signature = hmac.new(secret.encode(), signature_input.encode(), hashlib.sha256).hexdigest()
    payload = {
        "account": account,
        "timestamp": timestamp,
        "sign": signature,
        "bizParam": {"packageName": input.package_name},
    }
    try:
        return client.http_client.build_request(
            "POST",
            "https://developer-api.vivo.com.cn/gateway/developer/app/publish/v1/get-online-version",
            content=json.dumps(payload, separators=(",", ":")).encode(),
            headers={"Content-Type": "application/json", "Accept": "application/json"},
        )
    except (httpx.HTTPError, ValueError):
        return _branch("defect", FailureKind.VALIDATION, "vivo request is invalid")


def _http_rejection(status: int) -> QueryAttempt[GetOnlineVersionOutput]:
    return _branch(
        "providerRejected",
        FailureKind.PROVIDER_REJECTION,
        f"vivo rejected version request with HTTP {status}",
    )


def _decode(body: bytes, input: GetOnlineVersionInput, delay: float) -> QueryAttempt[GetOnlineVersionOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        document = None
    if not isinstance(document, dict) or type(document.get("code")) is not int:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "vivo returned malformed version data")
    code = document["code"]
    if code in (11001, 11002, 11003, 11004):
        return QueryAttempt.branch_result(
            "noData", GetOnlineVersionOutput(package_name=input.package_name), Receipt()
        )
    if code in (10003, 10006, 500):
        return _retry(FailureKind.AVAILABILITY, "vivo reported temporary query failure")
    if code != 0:
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"vivo rejected version request with code {code}",
        )
    data = document.get("data")
    if (
        not isinstance(data, dict)
        or data.get("packageName") != input.package_name
        or not isinstance(data.get("versionName"), str)
        or not data["versionName"]
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "vivo returned mismatched version data")
    raw_code = data.get("versionCode")
    try:
        if (
            isinstance(raw_code, bool)
            or not isinstance(raw_code, (str, int))
            or not re.fullmatch(r"-?[0-9]+", str(raw_code))
        ):
            raise ValueError("bad code")
        version_code = int(raw_code)
        if not 0 < version_code < 2**63:
            raise ValueError("bad code")
    except ValueError:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "vivo returned invalid version code")
    return QueryAttempt.branch_result(
        "found", GetOnlineVersionOutput(input.package_name, data["versionName"], version_code), Receipt()
    )
