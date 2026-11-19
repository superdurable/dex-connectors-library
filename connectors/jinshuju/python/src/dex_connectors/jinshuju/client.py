# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Jinshuju provider transport and Go-compatible Query decisions."""

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
from ._generated.definitions import LIST_FORMS_PAGE_DEFINITION
from .models import FormMetadata, ListFormsPageInput, ListFormsPageOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListFormsPageOutput]:
    return QueryAttempt.branch_result(
        branch, ListFormsPageOutput(), Receipt(), Failure(kind, "jinshuju", "listFormsPage", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListFormsPageOutput]:
    return QueryAttempt.retry(Failure(kind, "jinshuju", "listFormsPage", message), delay)


def _ascii_value(value: str, limit: int, allow_empty: bool = False) -> bool:
    return (
        isinstance(value, str)
        and (allow_empty or bool(value))
        and len(value.encode()) <= limit
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


class Client:
    """Resolve credentials per listFormsPage request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Jinshuju configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Jinshuju timeout or response limit is invalid")

        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_forms_page(self) -> "ListFormsPageOperation":
        """Bind the listFormsPage Query to this client."""
        return ListFormsPageOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListFormsPageOperation:
    """Perform one listFormsPage read and filter the provider response."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Jinshuju client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return LIST_FORMS_PAGE_DEFINITION

    async def invoke(self, call: Call, input: ListFormsPageInput) -> QueryAttempt[ListFormsPageOutput]:
        """Classify transport and provider outcomes without exposing secrets."""
        try:
            if not _ascii_value(input.next, 256, allow_empty=True):
                return _branch("defect", FailureKind.VALIDATION, "Jinshuju page cursor is invalid")
            credentials = await self.client.credentials.resolve(call)
            token = credentials.access_token.reveal()
            if not _ascii_value(token, 1024):
                return _branch(
                    "defect", FailureKind.AUTHENTICATION, "Jinshuju Access Token is unavailable or invalid"
                )
            params = {"next": input.next} if input.next else None
            request = self.client.http_client.build_request(
                "GET",
                "https://jinshuju.net/api/v1/forms",
                params=params,
                headers={"Accept": "application/json", "Authorization": f"Bearer {token}"},
            )
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Jinshuju credentials or request are invalid"
            )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Jinshuju request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Jinshuju rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Jinshuju is temporarily unavailable", delay)
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"Jinshuju rejected request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Jinshuju response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Jinshuju response was interrupted")
        finally:
            await response.aclose()

        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Jinshuju returned invalid JSON")
        if not isinstance(document, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Jinshuju returned invalid form metadata")
        total, count, forms = document.get("total"), document.get("count"), document.get("data")
        next_cursor = document.get("next")
        if next_cursor is None:
            next_cursor = ""
        if (
            type(total) is not int
            or type(count) is not int
            or not isinstance(forms, list)
            or total < 0
            or not 0 <= count <= 50
            or len(forms) != count
            or total < count
            or not isinstance(next_cursor, str)
            or not _ascii_value(next_cursor, 256, allow_empty=True)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Jinshuju returned invalid form metadata")
        result = []
        for form in forms:
            if (
                not isinstance(form, dict)
                or not isinstance(form.get("name"), str)
                or not isinstance(form.get("token"), str)
                or not form["name"]
                or not form["token"]
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Jinshuju returned incomplete form metadata"
                )
            result.append(FormMetadata(name=form["name"], token=form["token"]))
        return QueryAttempt.branch_result(
            "listed",
            ListFormsPageOutput(total=total, count=count, forms=tuple(result), next=next_cursor),
            Receipt(),
        )
