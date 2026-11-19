# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Useasy signed agent-list transport and Go-compatible Query decisions."""

import base64
import hmac
import json
import secrets
import time
from collections.abc import Callable
from datetime import UTC, datetime
from hashlib import sha256
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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_AGENTS_DEFINITION
from .models import AgentSummary, ListAgentsInput, ListAgentsOutput


def _valid_app_id(value: str) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 32 and value.isascii() and value.isdecimal()


def _valid_secret(value: str) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 512 and all(0x21 <= ord(ch) <= 0x7E for ch in value)


class Client:
    """Resolve rotated Useasy credentials for each Query and own the HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock_seconds: Callable[[], int] | None = None,
        nonce_factory: Callable[[], str] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Useasy configuration and credentials are required")
        parsed = urlsplit(config.base_url)
        if (
            parsed.scheme != "https"
            or parsed.netloc != "openapi.useasy.cn"
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("Useasy base URL must be the official HTTPS origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.clock_seconds = clock_seconds or (lambda: int(time.time()))
        self.nonce_factory = nonce_factory or (lambda: str(100000 + secrets.randbelow(900000)))

    def list_agents(self) -> "ListAgentsOperation":
        """Bind the listAgents Query."""
        return ListAgentsOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListAgentsOperation:
    """Read one bounded page of Useasy agent metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_AGENTS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListAgentsOutput]:
        return QueryAttempt.branch_result(
            branch,
            ListAgentsOutput(),
            Receipt(),
            Failure(kind, "useasy", "listAgents", "Useasy agent query failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListAgentsOutput]:
        return QueryAttempt.retry(Failure(kind, "useasy", "listAgents", "Useasy request failed"), delay)

    async def invoke(self, call: Call, input: ListAgentsInput) -> QueryAttempt[ListAgentsOutput]:
        """Read an agent page without copying personal or secret provider fields."""
        if not isinstance(input, ListAgentsInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        app_id, secret = credentials.app_id.reveal(), credentials.secret_key.reveal()
        if not _valid_app_id(app_id) or not _valid_secret(secret):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        page, page_size = input.page or 1, input.page_size or 20
        if page < 1 or page > 50000 or page_size < 1 or page_size > 100:
            return self._branch("defect", FailureKind.VALIDATION)
        timestamp, nonce = str(self.client.clock_seconds()), self.client.nonce_factory()
        signature = base64.b64encode(
            hmac.new(secret.encode(), (app_id + timestamp + nonce).encode(), sha256).digest()
        ).decode()
        request = self.client.http_client.build_request(
            "POST",
            self.client.config.base_url.rstrip("/") + "/openapi/platform/v1/agent/list",
            content=json.dumps({"page": page, "pageSize": page_size}, separators=(",", ":")).encode(),
            headers={
                "Accept": "application/json",
                "Content-Type": "application/json;charset=utf-8",
                "appid": app_id,
                "nonce": nonce,
                "timestamp": timestamp,
                "signature": signature,
            },
        )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(wire, dict) or not isinstance(wire.get("code"), str) or not wire["code"]:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if wire.get("success") is not True or wire["code"] != "200":
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        data = wire.get("data")
        if not isinstance(data, dict) or not isinstance(data.get("list"), list):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        page_info = data.get("page")
        if not isinstance(page_info, dict):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        returned_page = page_info.get("pageNumber")
        returned_size = page_info.get("pageSize")
        total = page_info.get("total")
        total_pages = page_info.get("totalPage")
        if (
            returned_page != page
            or not isinstance(returned_size, int)
            or returned_size <= 0
            or not isinstance(total, int)
            or total < 0
            or not isinstance(total_pages, int)
            or total_pages < 0
            or len(data["list"]) > page_size
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        agents: list[AgentSummary] = []
        for item in data["list"]:
            if not isinstance(item, dict):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            agent_id, number, enabled = item.get("_id"), item.get("agentNumber"), item.get("status")
            if (
                not isinstance(agent_id, str)
                or not 0 < len(agent_id) <= 128
                or not isinstance(number, str)
                or not 0 < len(number) <= 128
                or not isinstance(enabled, int)
                or isinstance(enabled, bool)
                or enabled not in (0, 1)
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            agents.append(AgentSummary(agent_id, number, enabled == 1))
        return QueryAttempt.branch_result(
            "listed", ListAgentsOutput(tuple(agents), page, total, total_pages), Receipt()
        )
