# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""FXiaoke CRM object metadata transport and Go-compatible decisions."""

import hashlib
import json
import re
import secrets
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
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
from ._generated.definitions import LIST_OBJECT_METADATA_DEFINITION
from .models import ListObjectMetadataInput, ListObjectMetadataOutput, ObjectMetadata

_TRANSIENT = {-1, -2, 14001, 30004, 50009}


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListObjectMetadataOutput]:
    return QueryAttempt.branch_result(
        branch, ListObjectMetadataOutput(), Receipt(), Failure(kind, "fxiaoke", "listObjectMetadata", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListObjectMetadataOutput]:
    return QueryAttempt.retry(Failure(kind, "fxiaoke", "listObjectMetadata", message), delay)


def _valid_identifier(value: str) -> bool:
    return bool(re.fullmatch(r"[A-Za-z0-9_-]{1,128}", value))


def _valid_secret(value: str) -> bool:
    return (
        bool(value)
        and len(value) <= 1024
        and value.strip() == value
        and all(0x20 < ord(character) < 0x7F for character in value)
    )


def _valid_cloud_host(value: str) -> bool:
    if value == "open.fxiaoke.com":
        return True
    return (
        len(value) <= 128
        and value.startswith("open-")
        and value.endswith((".fxiaoke.com", ".sharecrm.com"))
        and ".." not in value
        and "-." not in value
        and ".-" not in value
        and bool(re.fullmatch(r"[a-z0-9.-]+", value))
    )


class Client:
    """Own one CRM cloud context and an in-memory access-token cache."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        nonce_bytes: Callable[[int], bytes] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("FXiaoke configuration and credential source are required")
        if (
            not _valid_cloud_host(config.cloud_host)
            or not _valid_identifier(config.employee_id)
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("FXiaoke cloud host, employee ID, timeout, or response limit is invalid")
        self.base_url = "https://" + config.cloud_host
        self.employee_id = config.employee_id
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.nonce_bytes = nonce_bytes or secrets.token_bytes
        self.token_key = ""
        self.token = ""
        self.enterprise_account = ""
        self.token_valid_until = datetime.fromtimestamp(0, UTC)

    def list_object_metadata(self) -> "ListObjectMetadataOperation":
        """Bind the CRM object-list Query to this client."""
        return ListObjectMetadataOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    def _trace_id(self) -> str:
        value = bytearray(self.nonce_bytes(16))
        if len(value) != 16:
            raise ValueError("invalid nonce length")
        value[6] = value[6] & 0x0F | 0x40
        value[8] = value[8] & 0x3F | 0x80
        digits = value.hex()
        return f"{digits[:8]}-{digits[8:12]}-{digits[12:16]}-{digits[16:20]}-{digits[20:]}"

    async def _request(
        self, request: httpx.Request
    ) -> tuple[bytes | None, int, float, QueryAttempt[ListObjectMetadataOutput] | None]:
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return None, 0, 0, _retry(FailureKind.AVAILABILITY, "FXiaoke request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return None, 0, 0, _retry(FailureKind.RATE_LIMIT, "FXiaoke rate limited the request", delay)
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return (
                    None,
                    0,
                    0,
                    _branch(
                        "invalidResponse",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "FXiaoke response exceeded the configured limit",
                    ),
                )
            except ProviderTransportError:
                return None, 0, 0, _retry(FailureKind.AVAILABILITY, "FXiaoke response was interrupted")
            return body, status, delay, None
        finally:
            await response.aclose()

    async def _access_token(
        self, credentials: Credentials
    ) -> tuple[str, str, QueryAttempt[ListObjectMetadataOutput] | None]:
        key = hashlib.sha256(
            (
                credentials.app_id
                + "\x00"
                + credentials.app_secret.reveal()
                + "\x00"
                + credentials.permanent_code.reveal()
            ).encode()
        ).hexdigest()
        if self.token_key == key and self.token and datetime.now(UTC) < self.token_valid_until:
            return self.token, self.enterprise_account, None
        try:
            trace_id = self._trace_id()
        except (ValueError, OSError):
            return "", "", _retry(FailureKind.AVAILABILITY, "FXiaoke request ID could not be generated")
        body = json.dumps(
            {
                "appId": credentials.app_id,
                "appSecret": credentials.app_secret.reveal(),
                "permanentCode": credentials.permanent_code.reveal(),
                "grantType": "app_secret",
            },
            separators=(",", ":"),
        ).encode()
        request = self.http_client.build_request(
            "POST",
            self.base_url + "/oauth2.0/token?thirdTraceId=" + trace_id,
            content=body,
            headers={"Content-Type": "application/json", "Accept": "application/json"},
        )
        response_body, status, delay, error = await self._request(request)
        if error is not None:
            return "", "", error
        if status != 200:
            return "", "", _classify_http(status, delay)
        assert response_body is not None
        try:
            wire: Any = json.loads(response_body)
        except (ValueError, UnicodeDecodeError):
            return (
                "",
                "",
                _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "FXiaoke returned an invalid token response"
                ),
            )
        if not isinstance(wire, dict) or type(wire.get("errorCode")) is not int:
            return (
                "",
                "",
                _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "FXiaoke returned an invalid token response"
                ),
            )
        code = wire["errorCode"]
        if code in _TRANSIENT:
            return (
                "",
                "",
                _retry(FailureKind.AVAILABILITY, "FXiaoke token service is temporarily unavailable"),
            )
        if code != 0:
            return (
                "",
                "",
                _branch(
                    "providerRejected",
                    FailureKind.AUTHENTICATION,
                    f"FXiaoke rejected app credentials with code {code}",
                ),
            )
        token = wire.get("accessToken")
        account = wire.get("ea")
        expires_in = wire.get("expiresIn")
        if (
            not isinstance(token, str)
            or not _valid_secret(token)
            or not isinstance(account, str)
            or not _valid_identifier(account)
            or type(expires_in) is not int
            or not 0 < expires_in <= 7200
        ):
            return (
                "",
                "",
                _branch("invalidResponse", FailureKind.PROTOCOL, "FXiaoke returned invalid token metadata"),
            )
        self.token_key = key
        self.token = token
        self.enterprise_account = account
        self.token_valid_until = datetime.now(UTC) + timedelta(seconds=max(1, min(expires_in - 60, 6000)))
        return token, account, None


class ListObjectMetadataOperation:
    """Read CRM object names and availability without customer records."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("FXiaoke client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated CRM object-list Query identity."""
        return LIST_OBJECT_METADATA_DEFINITION

    async def invoke(
        self, call: Call, input: ListObjectMetadataInput
    ) -> QueryAttempt[ListObjectMetadataOutput]:
        """Exchange app credentials and list visible CRM object metadata."""
        try:
            credentials = await self.client.credentials.resolve(call)
            if (
                not _valid_identifier(credentials.app_id)
                or not _valid_secret(credentials.app_secret.reveal())
                or not _valid_secret(credentials.permanent_code.reveal())
            ):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "FXiaoke app credentials are unavailable or invalid"
            )
        token, account, error = await self.client._access_token(credentials)
        if error is not None:
            return error
        try:
            trace_id = self.client._trace_id()
        except (ValueError, OSError):
            return _retry(FailureKind.AVAILABILITY, "FXiaoke request ID could not be generated")
        request = self.client.http_client.build_request(
            "POST",
            self.client.base_url + "/cgi/crm/v2/object/list?thirdTraceId=" + trace_id,
            content=b"{}",
            headers={
                "Authorization": "Bearer " + token,
                "x-fs-ea": account,
                "x-fs-userid": self.client.employee_id,
                "Content-Type": "application/json",
                "Accept": "application/json",
            },
        )
        body, status, delay, error = await self.client._request(request)
        if error is not None:
            return error
        if status != 200:
            return _classify_http(status, delay)
        assert body is not None
        return self._decode_objects(body)

    def _decode_objects(self, body: bytes) -> QueryAttempt[ListObjectMetadataOutput]:
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "FXiaoke returned invalid object metadata"
            )
        if not isinstance(wire, dict):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "FXiaoke returned invalid object metadata"
            )
        code = wire.get("errorCode")
        if type(code) is int and code != 0:
            if code == 20016:
                self.client.token = ""
                self.client.enterprise_account = ""
                return _retry(FailureKind.AUTHENTICATION, "FXiaoke token expired")
            if code in _TRANSIENT:
                return _retry(FailureKind.AVAILABILITY, "FXiaoke object list is temporarily unavailable")
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"FXiaoke rejected the object-list request with code {code}",
            )
        data = wire.get("data")
        objects = data.get("objects") if isinstance(data, dict) else None
        if not isinstance(objects, list):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "FXiaoke returned no object list")
        result: list[ObjectMetadata] = []
        for item in objects:
            if (
                not isinstance(item, dict)
                or not isinstance(item.get("describeApiName"), str)
                or not item["describeApiName"]
                or not isinstance(item.get("describeDisplayName"), str)
                or not item["describeDisplayName"]
                or type(item.get("isActive")) is not bool
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "FXiaoke returned invalid object metadata"
                )
            result.append(
                ObjectMetadata(
                    api_name=item["describeApiName"],
                    display_name=item["describeDisplayName"],
                    active=item["isActive"],
                )
            )
        return QueryAttempt.branch_result(
            "listed", ListObjectMetadataOutput(objects=tuple(result)), Receipt()
        )


def _classify_http(status: int, delay: float) -> QueryAttempt[ListObjectMetadataOutput]:
    if status == 429:
        return _retry(FailureKind.RATE_LIMIT, "FXiaoke rate limited the request", delay)
    if status == 408 or status >= 500:
        return _retry(FailureKind.AVAILABILITY, "FXiaoke is temporarily unavailable", delay)
    kind = (
        FailureKind.AUTHENTICATION
        if status == 401
        else FailureKind.AUTHORIZATION
        if status == 403
        else FailureKind.PROVIDER_REJECTION
    )
    return _branch("providerRejected", kind, f"FXiaoke rejected the request with HTTP {status}")
