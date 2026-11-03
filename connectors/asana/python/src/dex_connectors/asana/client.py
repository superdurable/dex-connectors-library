# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated, bounded Asana HTTP client and secret-safe classifications."""

import json
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        AddCommentOperation,
        CreateTaskOperation,
        GetTaskOperation,
        ListTasksOperation,
        UpdateTaskOperation,
    )


@dataclass(frozen=True, slots=True)
class Exchange:
    """One provider attempt with dispatch evidence and a bounded response."""

    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    error: str = ""
    is_body_too_large: bool = False
    has_body_read_failed: bool = False
    reflects_secret: bool = False


class Client:
    """Resolve a personal token per call and send one request per exchange.

    A supplied HTTP client remains caller-owned; close a connector-owned client
    with ``aclose`` after the Worker stops.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Asana configuration and credential source are required")
        self.endpoint = validate_base_url(config.endpoint or Config().endpoint)
        self.max_response_bytes = config.max_response_bytes or Config().max_response_bytes
        if isinstance(self.max_response_bytes, bool) or self.max_response_bytes < 1:
            raise ValueError("Asana maxResponseBytes must be positive")
        self._credentials = credentials
        self._http_client = http_client or new_provider_http_client(20)
        self._owns_http_client = http_client is None
        self._now = now or (lambda: datetime.now(UTC))

    def list_tasks(self) -> "ListTasksOperation":
        """Bind the task page Query to this connection."""
        from .operations import ListTasksOperation

        return ListTasksOperation(self)

    def get_task(self) -> "GetTaskOperation":
        """Bind the task detail Query to this connection."""
        from .operations import GetTaskOperation

        return GetTaskOperation(self)

    def create_task(self) -> "CreateTaskOperation":
        """Bind the single-dispatch task creation Mutation."""
        from .operations import CreateTaskOperation

        return CreateTaskOperation(self)

    def update_task(self) -> "UpdateTaskOperation":
        """Bind the repeatable absolute task update Mutation."""
        from .operations import UpdateTaskOperation

        return UpdateTaskOperation(self)

    def add_comment(self) -> "AddCommentOperation":
        """Bind the single-dispatch comment Mutation."""
        from .operations import AddCommentOperation

        return AddCommentOperation(self)

    async def aclose(self) -> None:
        """Close only the provider client created by this connector."""
        if self._owns_http_client:
            await self._http_client.aclose()

    async def token(self, call: Call) -> str | None:
        """Resolve and validate a secret without storing it in Flow state."""
        try:
            credentials = await self._credentials.resolve(call)
            if not isinstance(credentials, Credentials):
                return None
            token = credentials.access_token.reveal()
            return token if is_header_safe_credential(token) else None
        except Exception:
            return None

    async def exchange(
        self,
        call: Call,
        token: str,
        method: str,
        path: str,
        query: Mapping[str, str] | None = None,
        payload: object | None = None,
    ) -> Exchange:
        """Send one JSON request without redirects and bound the complete body."""
        target = self.endpoint + path
        if query:
            target += "?" + str(httpx.QueryParams(sorted(query.items())))
        headers = {"Authorization": f"Bearer {token}", "Accept": "application/json"}
        body = None
        if payload is not None:
            headers["Content-Type"] = "application/json"
            try:
                body = json.dumps(payload, separators=(",", ":"), allow_nan=False).encode()
            except (TypeError, ValueError):
                return Exchange(error="notBuilt")
        try:
            request = self._http_client.build_request(method, target, headers=headers, content=body)
        except (httpx.HTTPError, ValueError):
            return Exchange(error="notBuilt")
        try:
            response, _ = await send_traced(self._http_client, request)
        except ProviderTransportError as error:
            return Exchange(error="beforeSend" if error.provably_not_sent else "afterSend")
        limit = self.max_response_bytes if 200 <= response.status_code < 300 else MAX_ERROR_BODY_BYTES
        try:
            contents = await read_bounded_body(response, limit)
        except BodyTooLargeError:
            return Exchange(status=response.status_code, headers=response.headers, is_body_too_large=True)
        except ProviderTransportError:
            return Exchange(status=response.status_code, headers=response.headers, has_body_read_failed=True)
        return Exchange(
            status=response.status_code,
            headers=response.headers,
            body=b"" if token.encode() in contents else contents,
            reflects_secret=token.encode() in contents,
        )

    def receipt(self, call: Call, object_id: str = "") -> Receipt:
        """Record only a stable Call identity and optional Asana object ID."""
        return Receipt(call.id, "asana", self._now().astimezone(UTC), call.idempotency_key, object_id)

    def retry_after(self, exchange: Exchange) -> float:
        """Read a bounded provider Retry-After hint."""
        return parse_retry_after(exchange.headers.get("Retry-After", ""), self._now())


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build a persistable failure without provider response text."""
    return Failure(kind, "asana", operation, message)


def rejection(operation: str, subject: str, status: int) -> Failure:
    """Classify a conclusive rejection by HTTP status alone."""
    kind = {
        400: FailureKind.VALIDATION,
        401: FailureKind.AUTHENTICATION,
        402: FailureKind.AUTHORIZATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
    }.get(status, FailureKind.PROVIDER_REJECTION)
    return failure(operation, kind, f"Asana rejected the {subject} with HTTP {status}")


def data_object(exchange: Exchange) -> dict[str, Any]:
    """Decode Asana's required object envelope from a successful response."""
    try:
        parsed = json.loads(exchange.body)
    except (ValueError, UnicodeDecodeError) as error:
        raise ValueError("response has no data object") from error
    if not isinstance(parsed, dict) or not isinstance(parsed.get("data"), dict):
        raise ValueError("response has no data object")
    data: dict[str, Any] = parsed["data"]
    return data
