# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Enlvon policy search with Go-compatible result filtering."""

from __future__ import annotations

import json
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
    encode_query_values,
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
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from ._generated.definitions import SEARCH_POLICIES_DEFINITION as SEARCH_POLICIES_DEFINITION
from ._generated.definitions import SEARCH_POLICIES_OPERATION_ID as SEARCH_POLICIES_OPERATION_ID
from .models import PolicyCard, SearchPoliciesInput, SearchPoliciesOutput

_DEFAULT_BASE_URL = "https://enlvon.com"
_DEFAULT_TIMEOUT_SECONDS = 10
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_INT64_MIN = -(2**63)
_INT64_MAX = 2**63 - 1


def _valid_api_key(value: str) -> bool:
    return value.startswith("enl_") and len(value.encode()) <= 2_048 and all(
        33 <= ord(character) <= 126 for character in value
    )


def _wire_field(document: dict[str, Any], name: str) -> Any:
    if name in document:
        return document[name]
    return next((value for key, value in document.items() if key.casefold() == name.casefold()), None)


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, SEARCH_POLICIES_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SearchPoliciesOutput]:
    return QueryAttempt.branch_result(branch, SearchPoliciesOutput(), Receipt(), _failure(kind, message))


class Client:
    """Own the production Enlvon origin and resolve the private API Key per search."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate the documented HTTPS origin and provider response bound."""
        if config is None or credentials is None:
            raise ValueError("Enlvon settings and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Enlvon timeout and maxResponseBytes must be positive")
        base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        parsed = urlsplit(base_url)
        if parsed.scheme != "https" or parsed.netloc != "enlvon.com" or parsed.path not in ("", "/"):
            raise ValueError("Enlvon base URL must be the documented HTTPS production origin")
        self.base_url = base_url.rstrip("/")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def search_policies(self) -> SearchPoliciesOperation:
        """Bind the read-only policy-search Query."""
        return SearchPoliciesOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()


class SearchPoliciesOperation:
    """Search policy cards without returning generated analysis or full text."""

    def __init__(self, client: Client) -> None:
        """Bind a validated Enlvon client."""
        if client is None:
            raise ValueError("Enlvon client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated Query identity, branches, and bounded retry policy."""
        return SEARCH_POLICIES_DEFINITION

    async def invoke(
        self, call: Call, input: SearchPoliciesInput
    ) -> QueryAttempt[SearchPoliciesOutput]:
        """GET policy cards and classify quota, provider, and protocol outcomes."""
        if (
            not input.query or len(input.query.encode()) > 1_024
            or type(input.top_k) is not int or input.top_k < 0 or input.top_k > 50
        ):
            return _branch(DEFECT, FailureKind.VALIDATION, "Enlvon search query or topK is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
        except Exception:  # noqa: BLE001
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Enlvon API key is unavailable or invalid"
            )
        if not _valid_api_key(api_key):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Enlvon API key is unavailable or invalid"
            )
        values = {"q": [input.query]}
        if input.top_k > 0:
            values["top_k"] = [str(input.top_k)]
        try:
            request = self.client.http_client.build_request(
                "GET", self.client.base_url + "/api/search?" + encode_query_values(values),
                headers={"X-API-Key": api_key, "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _branch(DEFECT, FailureKind.VALIDATION, "Enlvon search request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(
                _failure(FailureKind.AVAILABILITY, "Enlvon search transport failed")
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return QueryAttempt.retry(
                    _failure(FailureKind.RATE_LIMIT, "Enlvon search quota or rate limit reached"),
                    retry_after,
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Enlvon search temporarily unavailable"),
                    retry_after,
                )
            try:
                content = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE,
                    "Enlvon search response exceeded the configured limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Enlvon search response was interrupted")
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION if status in (401, 403)
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(
                    PROVIDER_REJECTED, kind, f"Enlvon rejected search with HTTP {status}"
                )
            try:
                document = json.loads(content)
                if not isinstance(document, dict):
                    raise ValueError("search response is not an object")
            except (ValueError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Enlvon returned invalid search JSON"
                )
            count = _wire_field(document, "count")
            results = _wire_field(document, "results")
            if (
                type(count) is not int or not _INT64_MIN <= count <= _INT64_MAX
                or not isinstance(results, list)
            ):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Enlvon returned invalid search JSON"
                )
            if count != len(results) or len(results) > 50:
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL,
                    "Enlvon returned an inconsistent result count",
                )
            policies: list[PolicyCard] = []
            for item in results:
                if not isinstance(item, dict):
                    return _branch(
                        INVALID_RESPONSE, FailureKind.PROTOCOL,
                        "Enlvon returned invalid search JSON",
                    )
                title = _wire_field(item, "标题")
                date = _wire_field(item, "日期")
                source = _wire_field(item, "来源")
                original_url = _wire_field(item, "原文链接")
                if date is None:
                    date = ""
                if source is None:
                    source = ""
                if original_url is None:
                    original_url = ""
                if not (
                    isinstance(title, str) and bool(title) and len(title.encode()) <= 2_048
                    and isinstance(date, str) and len(date.encode()) <= 128
                    and isinstance(source, str) and len(source.encode()) <= 128
                    and isinstance(original_url, str) and len(original_url.encode()) <= 4_096
                ):
                    return _branch(
                        INVALID_RESPONSE, FailureKind.PROTOCOL,
                        "Enlvon returned invalid policy metadata",
                    )
                policies.append(PolicyCard(title, date, source, original_url))
            return QueryAttempt.branch_result(
                FOUND, SearchPoliciesOutput(tuple(policies)), Receipt()
            )
        finally:
            await response.aclose()


Config.__doc__ = "Enlvon HTTPS origin and response bounds; zero uses manifest defaults."
Credentials.__doc__ = "Private enl_ API Key resolved for each search."
