# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Aqara signed device-list Query with Go-compatible provider decisions."""

import hashlib
import json
import secrets
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    DEFECT,
    INVALID_RESPONSE,
    LIST_DEVICES_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
)
from .models import Device, DevicePage, ListDevicesInput


class Client:
    """Own a redirect-safe transport and resolve Aqara credentials per call."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] = lambda: datetime.now(UTC),
        nonce_source: Callable[[int], bytes] = secrets.token_bytes,
    ) -> None:
        if config is None or credentials is None or clock is None or nonce_source is None:
            raise ValueError("Aqara configuration, credentials, clock, and nonce source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Aqara timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.clock = clock
        self.nonce_source = nonce_source

    def list_devices(self) -> "ListDevicesOperation":
        """Bind the one-page device Query to this client."""
        return ListDevicesOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class ListDevicesOperation:
    """Read one bounded page of devices authorized to the Aqara connection."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Aqara client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable list-devices operation and branch contract."""
        return LIST_DEVICES_DEFINITION

    async def invoke(self, call: Call, input: ListDevicesInput) -> QueryAttempt[DevicePage]:
        """Sign and send one request, then classify the bounded response."""
        if (
            not isinstance(input, ListDevicesInput)
            or type(input.page_num) is not int
            or type(input.page_size) is not int
        ):
            return _branch(
                DEFECT, FailureKind.VALIDATION, "pageNum or pageSize is outside the supported range"
            )
        page_num = input.page_num or 1
        page_size = input.page_size or 50
        if not 1 <= page_num <= 1_000_000 or not 1 <= page_size <= 100:
            return _branch(
                DEFECT, FailureKind.VALIDATION, "pageNum or pageSize is outside the supported range"
            )
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Aqara connection credentials are unavailable or invalid"
            )
        if not isinstance(credentials, Credentials) or not all(
            _valid_credential(value)
            for value in (
                credentials.app_id,
                credentials.key_id,
                credentials.app_key.reveal(),
                credentials.access_token.reveal(),
            )
        ):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Aqara connection credentials are unavailable or invalid"
            )
        try:
            nonce_bytes = self.client.nonce_source(16)
            if len(nonce_bytes) != 16:
                raise ValueError("wrong nonce length")
            nonce = nonce_bytes.hex()
        except (ValueError, OSError, TypeError):
            return _retry(FailureKind.AVAILABILITY, "secure nonce generation failed")
        timestamp = str(int(self.client.clock().timestamp() * 1000))
        request_body = json.dumps(
            {"intent": "query.device.info", "data": {"pageNum": page_num, "pageSize": page_size}},
            separators=(",", ":"),
            ensure_ascii=False,
        ).encode("utf-8")
        signature = _sign_request(
            credentials.access_token.reveal(),
            credentials.app_id,
            credentials.key_id,
            nonce,
            timestamp,
            credentials.app_key.reveal(),
        )
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/v3.0/open/api",
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "Accesstoken": credentials.access_token.reveal(),
                    "Appid": credentials.app_id,
                    "Keyid": credentials.key_id,
                    "Nonce": nonce,
                    "Time": timestamp,
                    "Sign": signature,
                },
                content=request_body,
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Aqara request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Aqara rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Aqara is temporarily unavailable", retry_after)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Aqara response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Aqara response was interrupted")
            if status != 200:
                return _branch(
                    PROVIDER_REJECTED, _classify_http(status), f"Aqara rejected the query with HTTP {status}"
                )
            try:
                page, provider_code = _decode_page(body, page_num, page_size)
            except (UnicodeDecodeError, ValueError, TypeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Aqara returned an invalid device response"
                )
            if provider_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "Aqara rate limited the request", retry_after)
            if provider_code in (100, 104, 500, 501):
                return _retry(FailureKind.AVAILABILITY, "Aqara is temporarily unavailable", retry_after)
            if provider_code != 0:
                return _branch(
                    PROVIDER_REJECTED,
                    _classify_code(provider_code),
                    f"Aqara rejected the query with code {provider_code}",
                )
            return QueryAttempt.branch_result(LISTED, page, Receipt())
        finally:
            await response.aclose()


def _sign_request(
    access_token: str, app_id: str, key_id: str, nonce: str, timestamp: str, app_key: str
) -> str:
    canonical = (
        f"Accesstoken={access_token}&Appid={app_id}&Keyid={key_id}"
        f"&Nonce={nonce}&Time={timestamp}{app_key}"
    ).lower()
    return hashlib.md5(canonical.encode("utf-8")).hexdigest()  # noqa: S324 - Aqara's documented wire signature


def _valid_credential(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 512 and all(32 < ord(char) < 127 for char in value)


def _decode_page(body: bytes, page_num: int, page_size: int) -> tuple[DevicePage, int]:
    document: Any = json.loads(
        body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid number"))
    )
    if not isinstance(document, dict) or type(document.get("code")) is not int:
        raise ValueError("invalid Aqara JSON")
    code = document["code"]
    if code != 0:
        return DevicePage(), code
    result = document.get("result")
    if not isinstance(result, dict) or not isinstance(result.get("data"), list):
        raise ValueError("invalid device count")
    items = result["data"]
    total_count = result.get("totalCount", 0)
    if type(total_count) is not int or total_count < 0 or len(items) > page_size:
        raise ValueError("invalid device count")
    devices: list[Device] = []
    for item in items:
        if not isinstance(item, dict):
            raise ValueError("invalid device")
        device_id = item.get("did", "")
        model = item.get("model", "")
        state = item.get("state")
        if (
            not isinstance(device_id, str)
            or not device_id
            or not isinstance(model, str)
            or not model
            or type(state) is not int
            or state not in (0, 1)
        ):
            raise ValueError("invalid device identity or state")
        fields = ("deviceName", "firmwareVersion", "positionId", "parentDid", "timeZone")
        if (
            any(not isinstance(item.get(field, ""), str) for field in fields)
            or type(item.get("modelType", 0)) is not int
        ):
            raise ValueError("invalid device metadata")
        devices.append(
            Device(
                device_id,
                item.get("deviceName", ""),
                model,
                item.get("modelType", 0),
                state == 1,
                item.get("firmwareVersion", ""),
                item.get("positionId", ""),
                item.get("parentDid", ""),
                item.get("timeZone", ""),
            )
        )
    return DevicePage(total_count, page_num, page_size, tuple(devices)), 0


def _classify_code(code: int) -> FailureKind:
    if code in (106, 107, 108, 109, 804, 2002, 2004, 2005):
        return FailureKind.AUTHENTICATION
    if code in (403, 603, 706, 717, 755, 756, 803, 2008, 2010, 2013, 2014):
        return FailureKind.AUTHORIZATION
    if code in (101, 102, 103, 105, 302, 303, 304, 305, 306, 604):
        return FailureKind.VALIDATION
    return FailureKind.PROVIDER_REJECTION


def _classify_http(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    if status in (400, 404):
        return FailureKind.VALIDATION
    return FailureKind.PROVIDER_REJECTION


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[DevicePage]:
    return QueryAttempt.branch_result(
        branch, DevicePage(), Receipt(), Failure(kind, "aqara", "listDevices", message)
    )


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[DevicePage]:
    return QueryAttempt.retry(Failure(kind, "aqara", "listDevices", message), retry_after)
