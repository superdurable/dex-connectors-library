# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read safe Mureka song task status through its public API."""

import json
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
from ._generated.definitions import GET_TASK_STATUS_DEFINITION
from .models import GetTaskStatusInput, GetTaskStatusOutput

_ENDPOINT = "https://api.mureka.cn/v1/song/query/"
_STATUSES = {
    "preparing",
    "queued",
    "running",
    "streaming",
    "reviewing",
    "succeeded",
    "failed",
    "timeouted",
    "cancelled",
}


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetTaskStatusOutput]:
    return QueryAttempt.branch_result(
        branch, GetTaskStatusOutput(), Receipt(), Failure(kind, "kunlun", "getTaskStatus", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetTaskStatusOutput]:
    return QueryAttempt.retry(Failure(kind, "kunlun", "getTaskStatus", message), delay)


class Client:
    """Resolve current Mureka credentials before each task read."""

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
            raise ValueError("Mureka configuration and credentials are required")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def get_task_status(self) -> "GetTaskStatusOperation":
        """Bind the song task status Query."""
        return GetTaskStatusOperation(self)

    async def aclose(self) -> None:
        """Close a connector-owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class GetTaskStatusOperation:
    """Return task state without lyrics or media URLs."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated song task contract."""
        return GET_TASK_STATUS_DEFINITION

    async def invoke(self, call: Call, input: GetTaskStatusInput) -> QueryAttempt[GetTaskStatusOutput]:
        """Read and classify one provider response."""
        if (
            not isinstance(input, GetTaskStatusInput)
            or not 1 <= len(input.task_id) <= 128
            or any(not (char.isascii() and (char.isalnum() or char in "-_")) for char in input.task_id)
        ):
            return _branch("defect", FailureKind.VALIDATION, "taskId is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "API key is unavailable")
        if (
            not api_key
            or len(api_key) > 1024
            or api_key.strip() != api_key
            or any(ord(char) <= 32 or ord(char) >= 127 for char in api_key)
        ):
            return _branch("defect", FailureKind.AUTHENTICATION, "API key is invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                _ENDPOINT + input.task_id,
                headers={"Authorization": "Bearer " + api_key, "Accept": "application/json"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Mureka request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                try:
                    body = await read_bounded_body(response, self.client.max_response_bytes)
                    error: Any = json.loads(body)
                    if (
                        "exceeded your current quota"
                        in str(error.get("error", {}).get("message", "")).lower()
                    ):
                        return _retry(FailureKind.QUOTA_EXHAUSTED, "Mureka quota is exhausted", delay)
                except (BodyTooLargeError, ProviderTransportError, ValueError, TypeError, AttributeError):
                    pass
                return _retry(FailureKind.RATE_LIMIT, "Mureka rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Mureka is unavailable", delay)
            if status == 404:
                return _branch("notFound", FailureKind.NOT_FOUND, "Mureka did not find the song task")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, f"Mureka rejected HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Mureka response is oversized"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Mureka response was interrupted")
            try:
                wire: Any = json.loads(body)
                if (
                    not isinstance(wire, dict)
                    or wire.get("id") != input.task_id
                    or wire.get("status") not in _STATUSES
                ):
                    raise ValueError("invalid task metadata")
                output = GetTaskStatusOutput(wire["id"], wire["status"], wire.get("model", ""))
            except (KeyError, TypeError, ValueError, UnicodeDecodeError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Mureka returned invalid task metadata"
                )
            return QueryAttempt.branch_result("found", output, Receipt())
        finally:
            await response.aclose()
