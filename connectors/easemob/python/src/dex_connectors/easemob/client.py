# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Easemob App Token acquisition, cache, refresh, and user detail Query."""

from __future__ import annotations

import asyncio
import hashlib
import json
import time
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlsplit

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
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import FOUND as FOUND
from ._generated.definitions import GET_USER_DETAIL_DEFINITION as GET_USER_DETAIL_DEFINITION
from ._generated.definitions import GET_USER_DETAIL_OPERATION_ID as GET_USER_DETAIL_OPERATION_ID
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from .models import GetUserDetailInput, UserDetail

_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 65_536
_MAX_TOKEN_SECONDS = 365 * 24 * 3600
_INT64_MAX = 2**63 - 1


def _valid_path_segment(value: str) -> bool:
    return bool(value) and len(value.encode()) <= 128 and all(
        character in "abcdefghijklmnopqrstuvwxyz0123456789-_" for character in value
    )


def _app_key_parts(value: str) -> tuple[str, str] | None:
    parts = value.split("#")
    if len(parts) != 2 or not all(_valid_path_segment(part) for part in parts):
        return None
    return parts[0], parts[1]


def _valid_client_id(value: str) -> bool:
    return (
        bool(value)
        and len(value.encode()) <= 256
        and value.strip() == value
        and "\r" not in value
        and "\n" not in value
    )


def _valid_user_id(value: str) -> bool:
    return bool(value) and len(value.encode()) <= 64 and all(
        character in "abcdefghijklmnopqrstuvwxyz0123456789-_." for character in value
    )


def _wire_field(document: dict[str, Any], name: str) -> Any:
    if name in document:
        return document[name]
    return next((value for key, value in document.items() if key.casefold() == name.casefold()), None)


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, GET_USER_DETAIL_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[UserDetail]:
    return QueryAttempt.branch_result(branch, UserDetail(), Receipt(), _failure(kind, message))


@dataclass(frozen=True, slots=True)
class _ProviderReply:
    status: int = 0
    body: bytes = b""
    retry_after: Any = 0
    error: str = ""


class Client:
    """Own provider settings and a synchronized short-lived App Token cache."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate the app's REST host and initialize an empty token cache."""
        if config is None or credentials is None:
            raise ValueError("Easemob settings and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Easemob configuration is invalid")
        base_url = validate_base_url(config.base_url)
        if urlsplit(base_url).path:
            raise ValueError("Easemob REST URL must be a host root without a path")
        self.base_url = base_url
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )
        self._token_lock = asyncio.Lock()
        self._token = ""
        self._token_expires_at = 0.0
        self._token_credential_fingerprint = b""

    def get_user_detail(self) -> GetUserDetailOperation:
        """Bind the read-only user account Query."""
        return GetUserDetailOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()

    async def _send(
        self, method: str, path: str, headers: dict[str, str], body: bytes | None = None
    ) -> _ProviderReply:
        try:
            request = self.http_client.build_request(
                method, self.base_url + path, headers=headers, content=body
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError, TypeError):
            return _ProviderReply(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                content = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _ProviderReply(status=response.status_code, retry_after=retry_after, error="oversize")
            except ProviderTransportError:
                return _ProviderReply(status=response.status_code, retry_after=retry_after, error="response")
            return _ProviderReply(status=response.status_code, body=content, retry_after=retry_after)
        finally:
            await response.aclose()

    async def _app_token(
        self, credentials: Credentials, organization: str, application: str, force_refresh: bool
    ) -> _ProviderReply:
        async with self._token_lock:
            material = (
                credentials.app_key + "\x00" + credentials.client_id + "\x00"
                + credentials.client_secret.reveal()
            )
            fingerprint = hashlib.sha256(material.encode()).digest()
            if (
                not force_refresh
                and self._token
                and fingerprint == self._token_credential_fingerprint
                and time.monotonic() < self._token_expires_at
            ):
                return _ProviderReply(status=200, body=self._token.encode())
            body = json.dumps({
                "grant_type": "client_credentials",
                "client_id": credentials.client_id,
                "client_secret": credentials.client_secret.reveal(),
            }, separators=(",", ":")).encode()
            reply = await self._send(
                "POST", f"/{organization}/{application}/token",
                {"Content-Type": "application/json", "Accept": "application/json"}, body,
            )
            if reply.error or reply.status != 200:
                return reply
            try:
                wire = json.loads(reply.body)
                if not isinstance(wire, dict):
                    raise ValueError("token response is not an object")
                token = _wire_field(wire, "access_token")
                expires_in = _wire_field(wire, "expires_in")
                if (
                    not isinstance(token, str) or not token
                    or any(character in token for character in " \t\r\n")
                    or type(expires_in) is not int or not 0 < expires_in <= _MAX_TOKEN_SECONDS
                ):
                    raise ValueError("token response is invalid")
            except (ValueError, UnicodeDecodeError):
                return _ProviderReply(error="token")
            safety_margin = min(30.0, expires_in / 10)
            self._token = token
            self._token_expires_at = time.monotonic() + expires_in - safety_margin
            self._token_credential_fingerprint = fingerprint
            return _ProviderReply(status=200, body=token.encode())

    async def _request_user(
        self, organization: str, application: str, user_id: str, token: str
    ) -> _ProviderReply:
        return await self._send(
            "GET", f"/{organization}/{application}/users/{user_id}",
            {
                "Accept": "application/json",
                "Content-Type": "application/json",
                "Authorization": "Bearer " + token,
            },
        )


class GetUserDetailOperation:
    """Read selected nonsecret account fields for one IM user."""

    def __init__(self, client: Client) -> None:
        """Bind a validated Easemob client."""
        if client is None:
            raise ValueError("Easemob client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated Query identity and branches."""
        return GET_USER_DETAIL_DEFINITION

    async def invoke(self, call: Call, input: GetUserDetailInput) -> QueryAttempt[UserDetail]:
        """Acquire or reuse an App Token, then GET and validate one user."""
        try:
            credentials = await self.client.credentials.resolve(call)
            parts = _app_key_parts(credentials.app_key)
            client_id = credentials.client_id
            client_secret = credentials.client_secret.reveal()
        except Exception:  # noqa: BLE001
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION,
                "Easemob credentials or user ID are unavailable or invalid",
            )
        if (
            parts is None or not _valid_client_id(client_id) or not client_secret
            or not _valid_user_id(input.user_id)
        ):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION,
                "Easemob credentials or user ID are unavailable or invalid",
            )
        organization, application = parts
        token_reply = await self.client._app_token(credentials, organization, application, False)
        if token_reply.error or token_reply.status != 200:
            classified_token = _classify_reply(token_reply)
            if classified_token is None:
                raise RuntimeError("non-success App Token response was not classified")
            return classified_token
        user_reply = await self.client._request_user(
            organization, application, input.user_id, token_reply.body.decode()
        )
        if user_reply.status == 401:
            token_reply = await self.client._app_token(credentials, organization, application, True)
            if token_reply.error or token_reply.status != 200:
                classified_token = _classify_reply(token_reply)
                if classified_token is None:
                    raise RuntimeError("non-success App Token response was not classified")
                return classified_token
            user_reply = await self.client._request_user(
                organization, application, input.user_id, token_reply.body.decode()
            )
        classified = _classify_reply(user_reply)
        if classified is not None:
            return classified
        try:
            wire = json.loads(user_reply.body)
            if not isinstance(wire, dict):
                raise ValueError("user response is not an object")
        except (ValueError, UnicodeDecodeError):
            return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Easemob returned invalid user JSON")
        entities = _wire_field(wire, "entities")
        count = _wire_field(wire, "count")
        if type(count) is not int or count != 1 or not isinstance(entities, list) or len(entities) != 1:
            return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Easemob returned an invalid user count")
        entity = entities[0]
        if not isinstance(entity, dict):
            return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Easemob returned invalid user JSON")
        user_id = _wire_field(entity, "username")
        user_uuid = _wire_field(entity, "uuid")
        created = _wire_field(entity, "created")
        modified = _wire_field(entity, "modified")
        activated = _wire_field(entity, "activated")
        nickname = _wire_field(entity, "nickname")
        if nickname is None:
            nickname = ""
        if not (
            user_id == input.user_id and isinstance(user_uuid, str) and bool(user_uuid)
            and type(created) is int and 0 < created <= _INT64_MAX
            and type(modified) is int and 0 < modified <= _INT64_MAX
            and type(activated) is bool and isinstance(nickname, str)
        ):
            return _branch(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "Easemob returned incomplete user detail"
            )
        return QueryAttempt.branch_result(
            FOUND, UserDetail(user_id, user_uuid, created, modified, activated, nickname), Receipt()
        )


def _classify_reply(reply: _ProviderReply) -> QueryAttempt[UserDetail] | None:
    if reply.error == "token":
        return _branch(
            INVALID_RESPONSE, FailureKind.PROTOCOL, "Easemob returned an invalid App Token response"
        )
    if reply.error == "request":
        return QueryAttempt.retry(_failure(FailureKind.TRANSPORT, "Easemob request failed"))
    if reply.status == 429:
        return QueryAttempt.retry(
            _failure(FailureKind.RATE_LIMIT, "Easemob rate limited the request"), reply.retry_after
        )
    if reply.status == 408 or reply.status >= 500:
        return QueryAttempt.retry(
            _failure(FailureKind.AVAILABILITY, "Easemob is temporarily unavailable"),
            reply.retry_after,
        )
    if reply.error == "oversize":
        return _branch(
            INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE,
            "Easemob response exceeded the configured limit",
        )
    if reply.error:
        return QueryAttempt.retry(
            _failure(FailureKind.TRANSPORT, "Easemob response was interrupted")
        )
    if reply.status != 200:
        return _branch(
            PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION,
            f"Easemob rejected HTTP {reply.status}",
        )
    return None


Config.__doc__ = "Easemob REST host and response bounds; zero uses manifest defaults."
Credentials.__doc__ = "App Key, Client ID, and private Client Secret for App Token acquisition."
