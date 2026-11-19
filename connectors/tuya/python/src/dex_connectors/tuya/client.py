# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Tuya signed device metadata Query with Go-compatible provider outcomes."""

import hashlib
import hmac
import json
import secrets
from collections.abc import Callable
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
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    FOUND as FOUND,
)
from ._generated.definitions import (
    GET_DEVICE_DETAILS_DEFINITION as GET_DEVICE_DETAILS_DEFINITION,
)
from ._generated.definitions import (
    GET_DEVICE_DETAILS_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    NOT_FOUND as NOT_FOUND,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import GetDeviceDetailsInput, GetDeviceDetailsOutput

_EMPTY_BODY_SHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
_ENDPOINTS = frozenset(
    (
        "https://openapi.tuyacn.com",
        "https://openapi.tuyaus.com",
        "https://openapi-ueaz.tuyaus.com",
        "https://openapi.tuyaeu.com",
        "https://openapi-weaz.tuyaeu.com",
        "https://openapi.tuyain.com",
        "https://openapi-sg.iotbing.com",
    )
)


def _valid_credential(value: str) -> bool:
    return (
        isinstance(value, str)
        and bool(value)
        and len(value) <= 512
        and all(33 <= ord(c) <= 126 for c in value)
    )


def _valid_device_id(value: str) -> bool:
    return (
        isinstance(value, str)
        and 1 <= len(value) <= 128
        and all(character.isascii() and (character.isalnum() or character in "-_") for character in value)
    )


def _invalid_constant(value: str) -> None:
    raise ValueError(f"invalid JSON constant {value}")


def _json_object(body: bytes) -> dict[str, object]:
    decoded = json.loads(body.decode("utf-8"), parse_constant=_invalid_constant)
    if not isinstance(decoded, dict):
        raise ValueError("expected object")
    return decoded


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetDeviceDetailsOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetDeviceDetailsOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, GET_DEVICE_DETAILS_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetDeviceDetailsOutput]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, GET_DEVICE_DETAILS_OPERATION_ID, message), delay)


def _classify_status(status: int, phase: str) -> QueryAttempt[GetDeviceDetailsOutput]:
    if status == 404 and phase == "device":
        return _branch(NOT_FOUND, FailureKind.NOT_FOUND, "Tuya did not find the device")
    kind = {401: FailureKind.AUTHENTICATION, 403: FailureKind.AUTHORIZATION}.get(
        status, FailureKind.PROVIDER_REJECTION
    )
    return _branch(PROVIDER_REJECTED, kind, "Tuya rejected the request")


class Client:
    """Bounded Tuya HTTP client with private signing credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] = lambda: datetime.now(UTC),
        nonce_source: Callable[[], bytes] = lambda: secrets.token_bytes(16),
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Tuya configuration and credentials are required")
        if config.endpoint not in _ENDPOINTS:
            raise ValueError("Tuya endpoint must match a supported data center")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Tuya timeout or response limit is invalid")
        self.endpoint = config.endpoint
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.clock = clock
        self.nonce_source = nonce_source

    def get_device_details(self) -> "GetDeviceDetailsOperation":
        """Bind the device metadata Query to this client."""
        return GetDeviceDetailsOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()

    async def _signed_get(
        self,
        client_id: str,
        client_secret: str,
        path: str,
        access_token: str,
    ) -> tuple[bytes, int, QueryAttempt[GetDeviceDetailsOutput] | None]:
        try:
            nonce_bytes = self.nonce_source()
            if len(nonce_bytes) != 16:
                raise ValueError("nonce must be 16 bytes")
            nonce = nonce_bytes.hex()
            timestamp = str(int(self.clock().timestamp() * 1000))
            string_to_sign = "GET\n" + _EMPTY_BODY_SHA256 + "\n\n" + path
            message = client_id + access_token + timestamp + nonce + string_to_sign
            signature = hmac.new(client_secret.encode(), message.encode(), hashlib.sha256).hexdigest().upper()
        except (ValueError, TypeError, OverflowError):
            return b"", 0, _retry(FailureKind.AVAILABILITY, "Tuya nonce or timestamp unavailable")
        headers = {
            "client_id": client_id,
            "t": timestamp,
            "nonce": nonce,
            "sign_method": "HMAC-SHA256",
            "sign": signature,
            "Accept": "application/json",
        }
        if access_token:
            headers["access_token"] = access_token
        request = self.http_client.build_request("GET", self.endpoint + path, headers=headers)
        try:
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return b"", 0, _retry(FailureKind.AVAILABILITY, "Tuya request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), self.clock())
            if response.status_code == 429:
                return b"", 0, _retry(FailureKind.RATE_LIMIT, "Tuya rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return b"", 0, _retry(FailureKind.AVAILABILITY, "Tuya is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return (
                    b"",
                    0,
                    _branch(INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE, "Tuya response exceeded limit"),
                )
            except ProviderTransportError:
                return b"", 0, _retry(FailureKind.AVAILABILITY, "Tuya response was interrupted")
            return body, response.status_code, None
        finally:
            await response.aclose()


class GetDeviceDetailsOperation:
    """Obtain a simple-mode token and read non-secret device metadata."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Tuya client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated device Query contract."""
        return GET_DEVICE_DETAILS_DEFINITION

    async def invoke(self, call: Call, input: GetDeviceDetailsInput) -> QueryAttempt[GetDeviceDetailsOutput]:
        """Sign two requests and classify token and device outcomes."""
        if not _valid_device_id(input.device_id):
            return _branch(DEFECT, FailureKind.VALIDATION, "deviceId is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            client_id = credentials.client_id.reveal()
            client_secret = credentials.client_secret.reveal()
        except Exception:
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Tuya credentials unavailable")
        if not _valid_credential(client_id) or not _valid_credential(client_secret):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Tuya credentials invalid")
        token_body, token_status, failure = await self.client._signed_get(
            client_id, client_secret, "/v1.0/token?grant_type=1", ""
        )
        if failure is not None:
            return failure
        if token_status != 200:
            return _classify_status(token_status, "token")
        try:
            token_document = _json_object(token_body)
            success = token_document.get("success")
            if type(success) is not bool:
                raise ValueError("missing token success")
            if not success:
                code = token_document.get("code")
                if code in (500, 1010):
                    return _retry(FailureKind.AVAILABILITY, "Tuya token service temporarily unavailable")
                return _branch(PROVIDER_REJECTED, FailureKind.AUTHENTICATION, "Tuya rejected token request")
            result = token_document.get("result")
            if not isinstance(result, dict):
                raise ValueError("missing token result")
            access_token = result.get("access_token")
            expire_time = result.get("expire_time")
            if (
                not isinstance(access_token, str)
                or not _valid_credential(access_token)
                or type(expire_time) is not int
                or expire_time <= 0
            ):
                raise ValueError("invalid access token")
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Tuya returned an invalid token")
        device_body, device_status, failure = await self.client._signed_get(
            client_id, client_secret, "/v1.0/devices/" + input.device_id, access_token
        )
        if failure is not None:
            return failure
        if device_status != 200:
            return _classify_status(device_status, "device")
        try:
            device_document = _json_object(device_body)
            success = device_document.get("success")
            if type(success) is not bool:
                raise ValueError("missing device success")
            if not success:
                code = device_document.get("code")
                if code in (500, 1010):
                    return _retry(FailureKind.AVAILABILITY, "Tuya device service temporarily unavailable")
                if code == 1000:
                    return _branch(NOT_FOUND, FailureKind.NOT_FOUND, "Tuya did not find the device")
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, "Tuya rejected device query"
                )
            result = device_document.get("result")
            if not isinstance(result, dict):
                raise ValueError("missing device result")
            device_id = result.get("id")
            name = result.get("name")
            online = result.get("online")
            category = result.get("category", "")
            product_id = result.get("product_id", "")
            created_at = result.get("create_time", 0)
            updated_at = result.get("update_time", 0)
            if (
                device_id != input.device_id
                or not isinstance(name, str)
                or not name
                or type(online) is not bool
                or not isinstance(category, str)
                or not isinstance(product_id, str)
                or type(created_at) is not int
                or type(updated_at) is not int
                or created_at < 0
                or updated_at < 0
            ):
                raise ValueError("invalid device metadata")
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Tuya returned invalid device metadata")
        return QueryAttempt.branch_result(
            FOUND,
            GetDeviceDetailsOutput(
                input.device_id, name, online, category, product_id, created_at, updated_at
            ),
            Receipt(),
        )


Config.__doc__ = "Tuya data-center endpoint, timeout in seconds, and response bound in bytes."
Credentials.__doc__ = "Private Tuya project access ID and secret resolved for each Query."
