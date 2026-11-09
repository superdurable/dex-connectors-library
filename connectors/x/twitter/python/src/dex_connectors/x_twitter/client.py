# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded X post reads with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote

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
from ._generated.definitions import GET_POST_DEFINITION
from .models import GetPostInput, PostMetadata


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "x-twitter", "getPost", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[PostMetadata]:
    return QueryAttempt.branch_result(branch, PostMetadata(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[PostMetadata]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_post_id(post_id: object) -> bool:
    return (
        isinstance(post_id, str)
        and 1 <= len(post_id) <= 20
        and post_id[0] != "0"
        and post_id.isascii()
        and post_id.isdecimal()
    )


_PROBLEM_TYPE_PREFIXES = ("https://api.x.com/2/problems/", "https://api.twitter.com/2/problems/")


def _problem_type(problem_type: str) -> str:
    """Return the final segment of an api.x.com or legacy api.twitter.com problem type."""
    for prefix in _PROBLEM_TYPE_PREFIXES:
        if problem_type.startswith(prefix):
            return problem_type[len(prefix) :]
    return ""


def _decode_problem(raw: dict[str, Any]) -> str:
    """Return notFound or providerRejected for an HTTP 200 errors array without data."""
    errors = raw.get("errors")
    if errors is None:
        errors = []
    if not isinstance(errors, list):
        raise ValueError("Post errors are not an array")
    problem = ""
    for provider_error in errors:
        if provider_error is None:
            continue
        if not isinstance(provider_error, dict):
            raise ValueError("Post error is not an object")
        problem_type = provider_error.get("type")
        if problem_type is None:
            continue
        if not isinstance(problem_type, str):
            raise ValueError("Post error type is not a string")
        segment = _problem_type(problem_type)
        if segment == "resource-not-found":
            return "notFound"
        if segment in ("not-authorized-for-resource", "client-forbidden"):
            problem = "providerRejected"
    if not problem:
        raise ValueError("Post data missing")
    return problem


def _decode_post(body: bytes, requested_post_id: str) -> PostMetadata | str:
    """Return the Post, or the branch for a problem X reported without data."""
    raw: Any = json.loads(body)
    if not isinstance(raw, dict):
        raise ValueError("Post response is not an object")
    data = raw.get("data")
    if data is None:
        return _decode_problem(raw)
    if (
        not isinstance(data, dict)
        or data.get("id") != requested_post_id
        or not isinstance(data.get("text"), str)
        or not data["text"].strip()
    ):
        raise ValueError("Post identity or text missing")
    return PostMetadata(data["id"], data["text"])


class Client:
    """Resolve a Bearer token per call and read selected post metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("X configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("X request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_post(self) -> GetPostOperation:
        """Bind the getPost Query to this client."""
        return GetPostOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, post_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            token = credential.access_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/tweets/" + quote(post_id, safe=""),
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
            if token.encode() in body:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="credential")
            return _Exchange(status=response.status_code, body=body, retry_after=retry_after)
        finally:
            await response.aclose()


class GetPostOperation:
    """Read one Post's ID and text."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_POST_DEFINITION

    async def invoke(self, call: Call, input: GetPostInput) -> QueryAttempt[PostMetadata]:
        """Classify post lookup outcomes consistently with Go."""
        if not isinstance(input, GetPostInput) or not _valid_post_id(input.post_id):
            return _branch("defect", FailureKind.VALIDATION, "Post ID must be 1–20 decimal digits")
        exchange = await self.client._get(call, input.post_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "X credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "X request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "X rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "X is temporarily unavailable", exchange.retry_after)
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "X did not expose the Post")
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"X rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "X response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "X response was interrupted")
        try:
            post = _decode_post(exchange.body, input.post_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "X returned invalid Post JSON")
        if isinstance(post, str):
            if post == "notFound":
                return _branch("notFound", FailureKind.NOT_FOUND, "X did not expose the Post")
            return _branch("providerRejected", FailureKind.PROVIDER_REJECTION, "X denied access to the Post")
        return QueryAttempt.branch_result("found", post, Receipt())
