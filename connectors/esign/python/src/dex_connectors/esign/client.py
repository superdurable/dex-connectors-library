# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""eSign flow status with HmacSHA256 request authentication."""

import base64
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
from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_SIGN_FLOW_STATUS_DEFINITION,
    GET_SIGN_FLOW_STATUS_OPERATION_ID,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
)
from .models import GetSignFlowStatusInput, GetSignFlowStatusOutput

_ENDPOINT = "https://openapi.esign.cn"
_SIGN_FLOW_ID = re.compile(r"[A-Za-z0-9_-]{1,128}\Z")
_MAX_INT64 = (1 << 63) - 1


class Client:
    """Resolve app signature credentials per Query attempt without persisting secrets."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        current_time: Callable[[], datetime] | None = None,
    ) -> None:
        """Validate settings and own the transport unless one was injected.

        ``current_time`` is injectable for deterministic signed-request tests.
        The AppSecret remains only in the credential source and request signature.
        """
        if config is None or credentials is None:
            raise ValueError("eSign configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("eSign timeout or response limit is invalid")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.owns_http_client = http_client is None
        self.current_time = current_time or (lambda: datetime.now(UTC))

    def get_sign_flow_status(self) -> "GetSignFlowStatusOperation":
        """Bind the read-only signing status Query."""
        return GetSignFlowStatusOperation(self)

    async def aclose(self) -> None:
        """Close the connector-owned transport after the Worker stops."""
        if self.owns_http_client:
            await self.http_client.aclose()


class GetSignFlowStatusOperation:
    """Read one signing status without exposing documents or signers."""

    def __init__(self, client: Client) -> None:
        """Bind a validated eSign client."""
        if client is None:
            raise ValueError("eSign client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation identity and branches."""
        return GET_SIGN_FLOW_STATUS_DEFINITION

    async def invoke(
        self, call: Call, input: GetSignFlowStatusInput
    ) -> QueryAttempt[GetSignFlowStatusOutput]:
        """Sign one GET and classify provider, protocol, and transport outcomes."""
        if not isinstance(input, GetSignFlowStatusInput) or not _valid_sign_flow_id(input.sign_flow_id):
            return _branch(DEFECT, FailureKind.VALIDATION, "eSign signFlowId is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id = credentials.app_id
            app_secret = credentials.app_secret.reveal()
        except Exception:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "eSign app credentials are unavailable or invalid"
            )
        if not _valid_credential(app_id) or not _valid_credential(app_secret):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "eSign app credentials are unavailable or invalid"
            )
        path = f"/v3/sign-flow/{input.sign_flow_id}/detail"
        timestamp = int(self.client.current_time().timestamp() * 1000)
        signature = _sign_read_request(app_secret, path)
        try:
            request = self.client.http_client.build_request(
                "GET",
                _ENDPOINT + path,
                headers={
                    "Accept": "*/*",
                    "Content-Type": "application/json; charset=UTF-8",
                    "X-Tsign-Open-App-Id": app_id,
                    "X-Tsign-Open-Auth-Mode": "Signature",
                    "X-Tsign-Open-Ca-Timestamp": str(timestamp),
                    "X-Tsign-Open-Ca-Signature": signature,
                },
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "eSign status request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "eSign rate limited the status request", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "eSign status is temporarily unavailable", retry_after
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch(
                    PROVIDER_REJECTED, kind, f"eSign rejected the status request with HTTP {status}"
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "eSign response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "eSign status response was interrupted")
            wire = _decode_wire(body)
            if wire is None:
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "eSign returned invalid status data")
            code = wire["code"]
            if code != 0:
                return _branch(
                    PROVIDER_REJECTED,
                    FailureKind.PROVIDER_REJECTION,
                    f"eSign rejected the status request with code {code}",
                )
            data = wire.get("data")
            sign_flow_status = data.get("signFlowStatus") if isinstance(data, dict) else None
            if type(sign_flow_status) is not int or not 0 <= sign_flow_status <= _MAX_INT64:
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "eSign returned incomplete status data"
                )
            return QueryAttempt.branch_result(
                FOUND, GetSignFlowStatusOutput(input.sign_flow_id, sign_flow_status), Receipt()
            )
        finally:
            await response.aclose()


def _valid_sign_flow_id(value: object) -> bool:
    return isinstance(value, str) and _SIGN_FLOW_ID.fullmatch(value) is not None


def _valid_credential(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 1024 and all(0x21 <= ord(c) <= 0x7E for c in value)


def _sign_read_request(secret: str, path: str) -> str:
    canonical = "GET\n*/*\n\napplication/json; charset=UTF-8\n\n" + path
    digest = hmac.new(secret.encode("utf-8"), canonical.encode("utf-8"), hashlib.sha256).digest()
    return base64.b64encode(digest).decode("ascii")


def _decode_wire(body: bytes) -> dict[str, Any] | None:
    try:
        wire: Any = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return None
    if not isinstance(wire, dict) or type(wire.get("code")) is not int:
        return None
    if not -(1 << 63) <= wire["code"] <= _MAX_INT64:
        return None
    data = wire.get("data")
    if data is not None and not isinstance(data, dict):
        return None
    if isinstance(data, dict) and "signFlowStatus" in data and data["signFlowStatus"] is not None:
        if type(data["signFlowStatus"]) is not int:
            return None
    return wire


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetSignFlowStatusOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetSignFlowStatusOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, GET_SIGN_FLOW_STATUS_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[GetSignFlowStatusOutput]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, GET_SIGN_FLOW_STATUS_OPERATION_ID, message), after)
