# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Productboard feature-page reads with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import parse_qs, urlencode, urlparse

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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_FEATURES_DEFINITION
from .models import FeatureMetadata, FeaturesPage, ListFeaturesInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "productboard", "listFeatures", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[FeaturesPage]:
    return QueryAttempt.branch_result(branch, FeaturesPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[FeaturesPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_cursor(cursor: object) -> bool:
    return (
        isinstance(cursor, str)
        and len(cursor.encode("utf-8")) <= 2048
        and not any(ord(character) < 32 or 127 <= ord(character) <= 159 for character in cursor)
    )


def _decode_page(body: bytes) -> FeaturesPage:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("data"), list):
        raise ValueError("missing feature data")
    features: list[FeatureMetadata] = []
    for entity in raw["data"]:
        if not isinstance(entity, dict) or not isinstance(entity.get("fields"), dict):
            raise ValueError("invalid feature")
        fields = entity["fields"]
        if (
            not isinstance(entity.get("id"), str)
            or not entity["id"]
            or entity.get("type") != "feature"
            or not isinstance(fields.get("name"), str)
            or not fields["name"].strip()
        ):
            raise ValueError("missing feature identity or name")
        features.append(FeatureMetadata(entity["id"], fields["name"]))
    links = raw.get("links", {})
    if not isinstance(links, dict):
        raise ValueError("invalid links")
    next_url = links.get("next", "")
    if not isinstance(next_url, str):
        raise ValueError("invalid next page URL")
    next_cursor = ""
    if next_url:
        parsed = urlparse(next_url)
        if (
            parsed.scheme != "https"
            or parsed.netloc != "api.productboard.com"
            or parsed.path != "/v2/entities"
        ):
            raise ValueError("invalid next page URL")
        cursors = parse_qs(parsed.query).get("pageCursor", [])
        if len(cursors) != 1 or not cursors[0] or not _valid_cursor(cursors[0]):
            raise ValueError("invalid next page cursor")
        next_cursor = cursors[0]
    return FeaturesPage(tuple(features), next_cursor)


class Client:
    """Resolve an API token per call and read selected feature metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Productboard configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Productboard request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_features(self) -> ListFeaturesOperation:
        """Bind the listFeatures Query to this client."""
        return ListFeaturesOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, page_cursor: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            token = credential.api_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        query = {"fields[]": "name", "type[]": "feature"}
        if page_cursor:
            query["pageCursor"] = page_cursor
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/entities?" + urlencode(query),
                headers={"Accept": "application/json", "Authorization": "Bearer " + token},
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            return _Exchange(status=response.status_code, body=body, retry_after=retry_after)
        finally:
            await response.aclose()


class ListFeaturesOperation:
    """Read one feature page and classify provider outcomes."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return LIST_FEATURES_DEFINITION

    async def invoke(self, call: Call, input: ListFeaturesInput) -> QueryAttempt[FeaturesPage]:
        """Classify feature listing outcomes consistently with Go."""
        if not isinstance(input, ListFeaturesInput) or not _valid_cursor(input.page_cursor):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "page cursor contains invalid characters or exceeds 2048 bytes",
            )
        exchange = await self.client._get(call, input.page_cursor)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Productboard credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Productboard request failed")
        if exchange.status == 429:
            return _retry(
                FailureKind.RATE_LIMIT, "Productboard rate limited the request", exchange.retry_after
            )
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Productboard is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Productboard rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Productboard response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Productboard response was interrupted")
        try:
            page = _decode_page(exchange.body)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Productboard returned invalid feature JSON"
            )
        return QueryAttempt.branch_result("listed", page, Receipt())
