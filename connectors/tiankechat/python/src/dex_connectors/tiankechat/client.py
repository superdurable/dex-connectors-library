# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""TiankeChat knowledge catalog Query and signed HTTP transport."""

import hashlib
import hmac
import json
import os
from collections.abc import Callable
from datetime import UTC, datetime
from urllib.parse import urlencode, urlsplit

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
from ._generated.definitions import LIST_KNOWLEDGE_ENTRIES_DEFINITION
from .models import KnowledgeEntry, ListKnowledgeEntriesInput, ListKnowledgeEntriesOutput


def _safe_credential(value: str) -> bool:
    return isinstance(value, str) and value != "" and all(0x21 <= ord(char) <= 0x7E for char in value)


class Client:
    """Own configuration and resolve the current app credentials per request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
        random_bytes: Callable[[int], bytes] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("TiankeChat configuration and credentials are required")
        origin = urlsplit(config.base_url)
        if (
            origin.scheme != "https"
            or not origin.netloc
            or origin.path not in ("", "/")
            or origin.query
            or origin.fragment
            or origin.username
            or origin.password
        ):
            raise ValueError("TiankeChat base URL must be an HTTPS origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))
        self.random_bytes = random_bytes or os.urandom

    def list_knowledge_entries(self) -> "ListKnowledgeEntriesOperation":
        """Bind the listKnowledgeEntries Query to this client."""
        return ListKnowledgeEntriesOperation(self)

    async def aclose(self) -> None:
        """Close only a transport owned by this client."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListKnowledgeEntriesOperation:
    """Read a page of TiankeChat knowledge metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query definition."""
        return LIST_KNOWLEDGE_ENTRIES_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListKnowledgeEntriesOutput]:
        return QueryAttempt.branch_result(
            branch,
            ListKnowledgeEntriesOutput(),
            Receipt(),
            Failure(kind, "tiankechat", "listKnowledgeEntries", "TiankeChat knowledge query failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListKnowledgeEntriesOutput]:
        return QueryAttempt.retry(
            Failure(kind, "tiankechat", "listKnowledgeEntries", "TiankeChat request failed"), delay
        )

    async def invoke(
        self, call: Call, input: ListKnowledgeEntriesInput
    ) -> QueryAttempt[ListKnowledgeEntriesOutput]:
        """Sign and read one Go-compatible knowledge page."""
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id, app_secret = credentials.app_id, credentials.app_secret.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        if (
            not _safe_credential(app_id)
            or not app_id.startswith(("ap_", "sb_"))
            or len(app_id) > 128
            or not _safe_credential(app_secret)
            or not app_secret.startswith("tks_")
            or len(app_secret) > 512
        ):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        if not isinstance(input, ListKnowledgeEntriesInput):
            return self._branch("defect", FailureKind.VALIDATION)
        page, limit = input.page or 1, input.limit or 20
        if (
            not isinstance(page, int)
            or not isinstance(limit, int)
            or page < 1
            or page > 1_000_000
            or limit < 1
            or limit > 100
        ):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            nonce = self.client.random_bytes(16).hex()
        except (ValueError, OSError):
            return self._retry(FailureKind.AVAILABILITY)
        timestamp = str(int(self.client.clock().timestamp()))
        path = "/api/openapi/knowledge?" + urlencode({"limit": limit, "page": page})
        sign_text = "\n".join((timestamp, nonce, "GET", path, ""))
        signature = hmac.new(app_secret.encode(), sign_text.encode(), hashlib.sha256).hexdigest()
        request = self.client.http_client.build_request(
            "GET",
            self.client.config.base_url.rstrip("/") + path,
            headers={
                "Accept": "application/json",
                "X-App-Id": app_id,
                "X-Timestamp": timestamp,
                "X-Nonce": nonce,
                "X-Signature": signature,
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
        if not isinstance(wire, dict) or not isinstance(wire.get("ok"), bool):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if wire["ok"] is False:
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        total, response_page, response_limit, items = (
            wire.get("total"),
            wire.get("page"),
            wire.get("limit"),
            wire.get("items"),
        )
        if (
            not isinstance(total, int)
            or isinstance(total, bool)
            or total < 0
            or not isinstance(response_page, int)
            or isinstance(response_page, bool)
            or response_page < 1
            or not isinstance(response_limit, int)
            or isinstance(response_limit, bool)
            or response_limit < 1
            or not isinstance(items, list)
            or len(items) > limit
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        entries: list[KnowledgeEntry] = []
        for item in items:
            if not isinstance(item, dict):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            identifier, active, hit_count, category = (
                item.get("id"),
                item.get("active"),
                item.get("hit_count"),
                item.get("category", ""),
            )
            if (
                not isinstance(identifier, int)
                or isinstance(identifier, bool)
                or identifier <= 0
                or not isinstance(active, bool)
                or not isinstance(hit_count, int)
                or isinstance(hit_count, bool)
                or hit_count < 0
                or not isinstance(category, str)
                or len(category) > 256
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            entries.append(KnowledgeEntry(identifier, category, active, hit_count))
        return QueryAttempt.branch_result(
            "listed",
            ListKnowledgeEntriesOutput(tuple(entries), total, response_page, response_limit),
            Receipt(),
        )
