# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded GitCode repository lookup matching the Go provider contract."""

from __future__ import annotations

import json
from datetime import UTC, datetime
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
    FETCHED,
    GET_REPOSITORY_DEFINITION,
    GET_REPOSITORY_OPERATION_ID,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
)
from .models import GetRepositoryInput, GetRepositoryOutput


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, GET_REPOSITORY_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetRepositoryOutput]:
    return QueryAttempt.branch_result(branch, GetRepositoryOutput(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetRepositoryOutput]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_segment(segment: object) -> bool:
    if not isinstance(segment, str) or not segment or len(segment.encode()) > 128:
        return False
    if segment in (".", "..") or segment.strip() != segment:
        return False
    return all(character.isascii() and (character.isalnum() or character in "-_.") for character in segment)


def _valid_token(token: object) -> bool:
    return (
        isinstance(token, str)
        and 0 < len(token.encode()) <= 512
        and all("!" <= char <= "~" for char in token)
    )


class Client:
    """Own immutable GitCode settings and resolve rotated credentials for each request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate the endpoint and response bounds before contacting GitCode."""
        if config is None or credentials is None:
            raise ValueError("GitCode settings and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("GitCode timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def get_repository(self) -> GetRepositoryOperation:
        """Bind the read-only repository Query to this client."""
        return GetRepositoryOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class GetRepositoryOperation:
    """Read one repository's bounded public metadata."""

    def __init__(self, client: Client) -> None:
        """Bind a validated GitCode client."""
        if client is None:
            raise ValueError("GitCode client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated repository Query contract."""
        return GET_REPOSITORY_DEFINITION

    async def invoke(self, call: Call, input: GetRepositoryInput) -> QueryAttempt[GetRepositoryOutput]:
        """Classify the repository response, transport failure, or retryable status."""
        if not _valid_segment(input.owner) or not _valid_segment(input.repo):
            return _branch(DEFECT, FailureKind.VALIDATION, "owner or repo path is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "connection token is unavailable or invalid")
        token = credentials.access_token.reveal()
        if not _valid_token(token):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "connection token is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url
                + "/api/v5/repos/"
                + quote(input.owner, safe="")
                + "/"
                + quote(input.repo, safe=""),
                headers={"Accept": "application/json", "Authorization": "Bearer " + token},
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _branch(DEFECT, FailureKind.VALIDATION, "repository request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "GitCode request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "GitCode rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "GitCode is temporarily unavailable", retry_after)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "GitCode response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "GitCode response was interrupted")
            if status == 404:
                return _branch(NOT_FOUND, FailureKind.PROVIDER_REJECTION, "GitCode repository was not found")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.VALIDATION
                    if status == 400
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, "GitCode rejected the repository lookup")
            try:
                document = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
                if not isinstance(document, dict):
                    raise ValueError
                if (
                    type(document.get("id")) is not int
                    or document["id"] <= 0
                    or not isinstance(document.get("full_name"), str)
                    or not document["full_name"]
                    or type(document.get("private")) is not bool
                ):
                    raise ValueError
                output = GetRepositoryOutput(
                    document["id"],
                    document["full_name"],
                    document["private"],
                    document.get("default_branch") or "",
                )
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "GitCode returned invalid repository metadata"
                )
            return QueryAttempt.branch_result(FETCHED, output, Receipt())
        finally:
            await response.aclose()


Config.__doc__ = "GitCode HTTPS origin, request timeout in seconds, and response bound in bytes."
Credentials.__doc__ = "Secret GitCode personal access token, resolved afresh for every Query."
