# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Get Note task status with Go-compatible provider outcome classification."""

import json
import re
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
    GET_NOTE_TASK_STATUS_DEFINITION,
    GET_NOTE_TASK_STATUS_OPERATION_ID,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
)
from .models import GetNoteTaskStatusInput, GetNoteTaskStatusOutput

_ENDPOINT = "https://openapi.biji.com/open/api/v1/resource/note/task/progress"
_TASK_ID_PATTERN = re.compile(r"[A-Za-z0-9_-]{1,128}\Z")


class Client:
    """Resolve paired OpenAPI credentials per attempt and bound provider responses."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate settings and own the transport unless one was injected."""
        if config is None or credentials is None:
            raise ValueError("Dedao configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Dedao configuration is invalid")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.owns_http_client = http_client is None

    def get_note_task_status(self) -> "GetNoteTaskStatusOperation":
        """Bind the task progress Query."""
        return GetNoteTaskStatusOperation(self)

    async def aclose(self) -> None:
        """Close the connector-owned transport after the Worker stops."""
        if self.owns_http_client:
            await self.http_client.aclose()


class GetNoteTaskStatusOperation:
    """Read one asynchronous note task status without exposing note content."""

    def __init__(self, client: Client) -> None:
        """Bind a validated Dedao client."""
        if client is None:
            raise ValueError("Dedao client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation identity and branches."""
        return GET_NOTE_TASK_STATUS_DEFINITION

    async def invoke(
        self, call: Call, input: GetNoteTaskStatusInput
    ) -> QueryAttempt[GetNoteTaskStatusOutput]:
        """Dispatch one task query and classify definite and retryable outcomes."""
        if not isinstance(input, GetNoteTaskStatusInput) or not _is_valid_task_id(input.task_id):
            return _branch(DEFECT, FailureKind.VALIDATION, "Get Note task ID is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            client_id = credentials.client_id.reveal()
        except Exception:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Get Note credentials are unavailable or invalid"
            )
        if not _is_valid_credential(api_key) or not _is_valid_credential(client_id):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Get Note credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "POST",
                _ENDPOINT,
                headers={
                    "Authorization": api_key,
                    "X-Client-ID": client_id,
                    "Content-Type": "application/json",
                    "Accept": "application/json",
                },
                json={"task_id": input.task_id},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "Get Note task request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Get Note rate limited task request", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Get Note API is temporarily unavailable", retry_after
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch(PROVIDER_REJECTED, kind, f"Get Note rejected task request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Get Note task response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Get Note task response was interrupted")
            wire = _decode_wire(body)
            if wire is None:
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Get Note returned invalid task data")
            if not wire["success"]:
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, "Get Note rejected the task request"
                )
            data = wire.get("data")
            if not isinstance(data, dict):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Get Note omitted task data")
            status_value = data.get("status", "")
            if status_value not in ("pending", "processing", "success", "failed"):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Get Note returned unknown task status"
                )
            note_id = data.get("note_id", "")
            if not isinstance(note_id, str) or len(note_id.encode("utf-8")) > 128:
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Get Note returned invalid note ID")
            return QueryAttempt.branch_result(
                FOUND, GetNoteTaskStatusOutput(input.task_id, status_value, note_id), Receipt()
            )
        finally:
            await response.aclose()


def _is_valid_task_id(value: object) -> bool:
    return isinstance(value, str) and _TASK_ID_PATTERN.fullmatch(value) is not None


def _is_valid_credential(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 1024 and all(0x21 <= ord(c) <= 0x7E for c in value)


def _decode_wire(body: bytes) -> dict[str, Any] | None:
    try:
        wire = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return None
    if not isinstance(wire, dict) or type(wire.get("success")) is not bool:
        return None
    return wire


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetNoteTaskStatusOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetNoteTaskStatusOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, GET_NOTE_TASK_STATUS_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[GetNoteTaskStatusOutput]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, GET_NOTE_TASK_STATUS_OPERATION_ID, message), after)
