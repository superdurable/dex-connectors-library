# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Vidu model catalog matching the Go provider contract."""

import json
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
from ._generated.definitions import GET_TASK_STATUS_DEFINITION
from .models import GetTaskStatusInput, GetTaskStatusOutput

_ENDPOINT = "https://api.vidu.cn/ent/v2/tasks/"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetTaskStatusOutput]:
    return QueryAttempt.branch_result(
        branch, GetTaskStatusOutput(), Receipt(), Failure(kind, "shengshu", "getTaskStatus", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetTaskStatusOutput]:
    return QueryAttempt.retry(Failure(kind, "shengshu", "getTaskStatus", message), delay)


def _valid_api_key(value: str) -> bool:
    return (
        bool(value)
        and len(value.encode()) <= 1024
        and value.strip() == value
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


class Client:
    """Own the HTTP transport and resolve the Vidu API Key per attempt."""

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
            raise ValueError("Vidu configuration and credential source are required")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_task_status(self) -> "GetTaskStatusOperation":
        """Bind the available-model Query."""
        return GetTaskStatusOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport only if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetTaskStatusOperation:
    """List at most fifty model IDs visible to the API Key."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Vidu client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated model-list Query contract."""
        return GET_TASK_STATUS_DEFINITION

    async def invoke(self, call: Call, input: GetTaskStatusInput) -> QueryAttempt[GetTaskStatusOutput]:
        """Classify one model-list response and safe retry outcomes."""
        if (
            not input.task_id
            or len(input.task_id) > 128
            or any(not (c.isascii() and (c.isalnum() or c in "-_")) for c in input.task_id)
        ):
            return _branch("defect", FailureKind.VALIDATION, "taskId is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not _valid_api_key(api_key):
                raise ValueError("invalid API Key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Vidu API Key is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                _ENDPOINT + input.task_id + "/creations",
                headers={"Authorization": "Token " + api_key, "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Vidu model request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Vidu model request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Vidu rate limited the model request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Vidu API is temporarily unavailable", delay)
            if status == 404:
                return _branch("notFound", FailureKind.NOT_FOUND, "Vidu did not find the task")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, f"Vidu rejected model request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Vidu response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Vidu response was interrupted")
        finally:
            await response.aclose()
        try:
            wire = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
            if (
                not isinstance(wire, dict)
                or wire.get("id") != input.task_id
                or wire.get("state") not in ("created", "queueing", "processing", "success", "failed")
            ):
                raise ValueError("invalid task")
            credits = wire.get("credits")
            if type(credits) is not int or credits < 0:
                raise ValueError("invalid credits")
            creations = wire.get("creations") or []
            if not isinstance(creations, list):
                raise ValueError("invalid creations")
            creation_ids: list[str] = []
            for item in creations:
                if not isinstance(item, dict) or not isinstance(item.get("id"), str) or not item["id"]:
                    raise ValueError("invalid creation")
                creation_ids.append(item["id"])
            output = GetTaskStatusOutput(
                task_id=input.task_id,
                state=wire["state"],
                error_code=wire.get("err_code") or "",
                credits=credits,
                creation_ids=tuple(creation_ids),
            )
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Vidu returned invalid task metadata")
        return QueryAttempt.branch_result("found", output, Receipt())
