# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OneNET signed device-detail Query and provider decisions."""

from __future__ import annotations

import base64
import binascii
import hashlib
import hmac
import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from typing import Any
from urllib.parse import quote

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
from ._generated.definitions import QUERY_DEVICE_DETAIL_DEFINITION
from .models import DeviceDetail, QueryDeviceDetailInput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[DeviceDetail]:
    return QueryAttempt.branch_result(
        branch, DeviceDetail(), Receipt(), Failure(kind, "onenet", "queryDeviceDetail", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[DeviceDetail]:
    return QueryAttempt.retry(Failure(kind, "onenet", "queryDeviceDetail", message), delay)


def _valid_resource(value: str) -> bool:
    return (
        bool(value) and len(value) <= 128 and all(c.isascii() and (c.isalnum() or c in "-_") for c in value)
    )


class Client:
    """Own OneNET settings and resolve credentials per Query attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("valid OneNET configuration and credentials are required")
        self.base_url = validate_base_url(config.base_url)
        if httpx.URL(self.base_url).path not in ("", "/"):
            raise ValueError("OneNET API URL must be a host root")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def query_device_detail(self) -> QueryDeviceDetailOperation:
        """Bind the device-detail Query to this client."""
        return QueryDeviceDetailOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_client:
            await self.http_client.aclose()


class QueryDeviceDetailOperation:
    """Read one nonsecret device detail."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("OneNET client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return QUERY_DEVICE_DETAIL_DEFINITION

    async def invoke(self, call: Call, input: QueryDeviceDetailInput) -> QueryAttempt[DeviceDetail]:
        """Sign one request and classify its observed outcome."""
        try:
            credentials = await self.client.credentials.resolve(call)
            user_id, product_id = credentials.user_id, credentials.product_id
            device_name = input.device_name
            key = base64.b64decode(credentials.access_key.reveal(), validate=True)
            if (
                not user_id.isascii()
                or not user_id.isdecimal()
                or len(user_id) > 32
                or not _valid_resource(product_id)
                or not key
                or not device_name
                or len(device_name) > 256
                or device_name.strip() != device_name
                or any(ord(c) < 32 for c in device_name)
            ):
                raise ValueError("invalid credentials or device")
        except (ValueError, LookupError, OSError, AttributeError, binascii.Error):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "OneNET credentials or device name are invalid"
            )
        expiry = str(int((self.client.clock() + timedelta(hours=1)).timestamp()))
        resource = f"userid/{user_id}"
        signature_text = f"{expiry}\nsha256\n{resource}\n2022-05-01"
        signature = base64.b64encode(hmac.new(key, signature_text.encode(), hashlib.sha256).digest()).decode()
        authorization = (
            f"version=2022-05-01&res={quote(resource, safe='')}&et={expiry}"
            f"&method=sha256&sign={quote(signature, safe='')}"
        )
        try:
            request = self.client.http_client.build_request(
                "GET",
                f"{self.client.base_url}/device/detail",
                params={"product_id": product_id, "device_name": device_name},
                headers={"Accept": "application/json", "Authorization": authorization},
            )
        except (httpx.HTTPError, ValueError):
            return _retry(FailureKind.TRANSPORT, "OneNET request failed")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.TRANSPORT, "OneNET request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "OneNET rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "OneNET is unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "OneNET response exceeded the limit"
                )
            except ProviderTransportError:
                return _retry(FailureKind.TRANSPORT, "OneNET response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"OneNET rejected HTTP {status}"
            )
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "OneNET returned invalid JSON")
        if not isinstance(wire, dict) or type(wire.get("code")) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "OneNET omitted response code")
        if wire["code"] != 0:
            return _branch("providerRejected", FailureKind.PROVIDER_REJECTION, "OneNET rejected device query")
        data = wire.get("data")
        if (
            not isinstance(data, dict)
            or not isinstance(data.get("pid"), str)
            or not data["pid"]
            or not isinstance(data.get("name"), str)
            or not data["name"]
            or type(data.get("status")) is not int
            or data["status"] not in (0, 1, 2)
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "OneNET returned incomplete device detail"
            )
        return QueryAttempt.branch_result(
            "found",
            DeviceDetail(
                product_id=data["pid"],
                device_name=data["name"],
                status=data["status"],
                description=data.get("desc", ""),
                create_time=data.get("create_time", ""),
                last_online_time=data.get("last_time", ""),
            ),
            Receipt(),
        )
