# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Halo article listing matching the Go provider contract."""

from __future__ import annotations

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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    INVALID_RESPONSE,
    LIST_POSTS_DEFINITION,
    LIST_POSTS_OPERATION_ID,
    LISTED,
    PROVIDER_REJECTED,
)
from .models import ListPostsInput, ListPostsOutput, PostSummary

POSTS_PATH = "/apis/content.halo.run/v1alpha1/posts"


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, LIST_POSTS_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListPostsOutput]:
    return QueryAttempt.branch_result(branch, ListPostsOutput(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListPostsOutput]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_api_key(value: object) -> bool:
    return (
        isinstance(value, str)
        and value.startswith("pat_")
        and len(value.encode()) <= 512
        and all("!" <= character <= "~" for character in value)
    )


def _parse_page(body: bytes, page: int, page_size: int) -> ListPostsOutput:
    document = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
    if not isinstance(document, dict):
        raise ValueError("article page must be an object")
    items, has_next = document.get("items"), document.get("hasNext")
    if (
        not isinstance(items, list)
        or type(has_next) is not bool
        or type(document.get("page")) is not int
        or document["page"] != page
        or type(document.get("size")) is not int
        or document["size"] != page_size
        or type(document.get("total")) is not int
        or document["total"] < 0
        or len(items) > page_size
    ):
        raise ValueError("article page metadata is invalid")
    posts: list[PostSummary] = []
    for item in items:
        if not isinstance(item, dict):
            raise ValueError("article item is invalid")
        metadata, spec = item.get("metadata"), item.get("spec")
        if not isinstance(metadata, dict) or not isinstance(spec, dict):
            raise ValueError("article metadata is invalid")
        name, title, slug = metadata.get("name"), spec.get("title"), spec.get("slug")
        published, visibility = spec.get("publish"), spec.get("visible")
        if (
            not isinstance(name, str)
            or not name
            or len(name.encode()) > 512
            or not isinstance(title, str)
            or not title
            or len(title.encode()) > 1024
            or not isinstance(slug, str)
            or not slug
            or len(slug.encode()) > 1024
            or slug.strip() != slug
            or type(published) is not bool
            or visibility not in ("PUBLIC", "INTERNAL", "PRIVATE")
        ):
            raise ValueError("article fields are invalid")
        posts.append(PostSummary(name, title, slug, published, visibility))
    if has_next and page == 50000:
        raise ValueError("pagination exceeds the supported range")
    return ListPostsOutput(tuple(posts), page + 1 if has_next else 0)


class Client:
    """Own validated Halo settings and resolve rotated credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate the HTTPS endpoint, timeout, and response bound."""
        if config is None or credentials is None:
            raise ValueError("Halo settings and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Halo timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def list_posts(self) -> ListPostsOperation:
        """Bind the article-list Query to this client."""
        return ListPostsOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class ListPostsOperation:
    """Read one page of Halo article metadata."""

    def __init__(self, client: Client) -> None:
        """Bind a validated Halo client."""
        if client is None:
            raise ValueError("Halo client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated article-list Query contract."""
        return LIST_POSTS_DEFINITION

    async def invoke(self, call: Call, input: ListPostsInput) -> QueryAttempt[ListPostsOutput]:
        """Classify article pages, definite failures, and retryable failures."""
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Halo token is unavailable or invalid")
        api_key = credentials.api_key.reveal()
        if not _valid_api_key(api_key):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Halo token is unavailable or invalid")
        page, page_size = input.page or 1, input.page_size or 20
        if (
            type(page) is not int
            or type(page_size) is not int
            or not 1 <= page <= 50000
            or not 1 <= page_size <= 100
        ):
            return _branch(DEFECT, FailureKind.VALIDATION, "page or pageSize is outside the supported range")
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + POSTS_PATH,
                params={"page": str(page), "size": str(page_size)},
                headers={"Accept": "application/json", "Authorization": "Bearer " + api_key},
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _branch(DEFECT, FailureKind.VALIDATION, "article-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Halo request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Halo rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Halo temporarily unavailable", retry_after)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "article response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "article response was interrupted")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"Halo rejected the request with HTTP {status}")
            try:
                output = _parse_page(body, page, page_size)
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Halo returned invalid article data")
            return QueryAttempt.branch_result(LISTED, output, Receipt())
        finally:
            await response.aclose()


Config.__doc__ = "Halo HTTPS origin, request timeout in seconds, and response bound in bytes."
Credentials.__doc__ = "Secret Halo personal access token, resolved afresh for every Query."
