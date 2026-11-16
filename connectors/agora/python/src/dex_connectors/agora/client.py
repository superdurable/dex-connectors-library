# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Agora agent listing with Go-compatible provider outcome classification."""

import base64
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import LIST_RUNNING_AGENTS_DEFINITION as LIST_RUNNING_AGENTS_DEFINITION
from ._generated.definitions import LIST_RUNNING_AGENTS_OPERATION_ID as LIST_RUNNING_AGENTS_OPERATION_ID
from ._generated.definitions import LISTED as LISTED
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from .models import ListRunningAgentsInput, RunningAgent, RunningAgents

_APP_ID = re.compile(r"[0-9a-fA-F]{32}\Z")
_OPERATION = LIST_RUNNING_AGENTS_OPERATION_ID


class Client:
    """Read an Agora project with credentials resolved for every attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Agora configuration and credential source are required")
        self.base_url = validate_base_url(config.base_url)
        if httpx.URL(self.base_url).path not in ("", "/"):
            raise ValueError("Agora API URL must be a host root")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Agora timeout and response limit must be positive")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def list_running_agents(self) -> "ListRunningAgentsOperation":
        """Bind the one-page agent list Query to this client."""
        return ListRunningAgentsOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport after the Worker stops."""
        await self.http_client.aclose()


class ListRunningAgentsOperation:
    """Read one page of running Agora Conversational AI agents."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Agora client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated operation identity and branches."""
        return LIST_RUNNING_AGENTS_DEFINITION

    async def invoke(self, call: Call, input: ListRunningAgentsInput) -> QueryAttempt[RunningAgents]:
        """Send one GET and classify the response without persisting credentials."""
        if not isinstance(input, ListRunningAgentsInput):
            return _branch(DEFECT, FailureKind.VALIDATION, "Agora query input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Agora credentials are unavailable")
        app_id = credentials.app_id
        customer_id = credentials.customer_id
        secret = credentials.customer_secret.reveal()
        if (
            not isinstance(app_id, str)
            or _APP_ID.fullmatch(app_id) is None
            or not _valid_customer_id(customer_id)
            or not isinstance(secret, str)
            or not secret
        ):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Agora credentials are unavailable or invalid")
        endpoint = (
            self.client.base_url
            + "/api/conversational-ai-agent/v2/projects/"
            + app_id
            + "/agents"
        )
        try:
            encoded = base64.b64encode(f"{customer_id}:{secret}".encode()).decode("ascii")
            request = self.client.http_client.build_request(
                "GET", endpoint, headers={"Accept": "application/json", "Authorization": "Basic " + encoded}
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.TRANSPORT, "Agora request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Agora rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Agora is temporarily unavailable", retry_after)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE,
                    "Agora response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.TRANSPORT, "Agora response was interrupted")
            if status != 200:
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION,
                    f"Agora rejected HTTP {status}",
                )
            try:
                output = _decode_agents(body)
            except (UnicodeDecodeError, ValueError, TypeError, KeyError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Agora returned invalid agents JSON")
            return QueryAttempt.branch_result(LISTED, output, Receipt())
        finally:
            await response.aclose()


def _valid_customer_id(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 128
        and all(0x21 <= ord(character) <= 0x7E and character != ":" for character in value)
    )


def _decode_agents(body: bytes) -> RunningAgents:
    document: Any = json.loads(body.decode("utf-8"))
    if not isinstance(document, dict) or document.get("status") != "ok":
        raise ValueError("invalid status")
    data = document.get("data")
    meta = document.get("meta")
    if not isinstance(data, dict) or not isinstance(meta, dict):
        raise ValueError("missing page")
    count = data.get("count")
    rows = data.get("list")
    total = meta.get("total")
    cursor = meta.get("cursor", "")
    if (
        type(count) is not int
        or not isinstance(rows, list)
        or not 0 <= count <= 20
        or count != len(rows)
        or type(total) is not int
        or total < count
        or not isinstance(cursor, str)
    ):
        raise ValueError("inconsistent page")
    agents: list[RunningAgent] = []
    for row in rows:
        if not isinstance(row, dict):
            raise ValueError("invalid agent")
        agent_id = row.get("agent_id")
        status = row.get("status")
        started = row.get("start_ts")
        if (
            not isinstance(agent_id, str)
            or not agent_id
            or status != "RUNNING"
            or type(started) is not int
            or started <= 0
        ):
            raise ValueError("invalid agent")
        agents.append(RunningAgent(agent_id, status, started))
    return RunningAgents(count, tuple(agents), total, cursor)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[RunningAgents]:
    return QueryAttempt.branch_result(
        branch, RunningAgents(), Receipt(), Failure(kind, CONNECTOR_ID, _OPERATION, message)
    )


def _retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[RunningAgents]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, _OPERATION, message), after)
