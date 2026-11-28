# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Gitee repository lookup matching the Go provider contract."""

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
    FOUND,
    GET_REPOSITORY_DEFINITION,
    GET_REPOSITORY_OPERATION_ID,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
)
from .models import GetRepositoryInput, GetRepositoryOutput, RepositoryMetadata


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
    """Own immutable Gitee settings and resolve rotated credentials for each request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate the endpoint and response bounds before contacting Gitee."""
        if config is None or credentials is None:
            raise ValueError("Gitee settings and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Gitee timeout and maxResponseBytes must be positive")
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
        """Bind a validated Gitee client."""
        if client is None:
            raise ValueError("Gitee client is required")
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
            token = credentials.access_token.reveal()
        except Exception:
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "connection token is unavailable or invalid")
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
                params={"access_token": token},
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _branch(DEFECT, FailureKind.VALIDATION, "repository request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Gitee request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Gitee rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Gitee is temporarily unavailable", retry_after)
            if status == 404:
                return _branch(
                    NOT_FOUND, FailureKind.PROVIDER_REJECTION, "repository was not found or is not visible"
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"Gitee rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Gitee response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Gitee response was interrupted")
            try:
                document = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
                if not isinstance(document, dict):
                    raise ValueError("expected a repository object")
                fields = {key.lower(): value for key, value in document.items()}
                identifier = _repository_integer(fields.get("id"))
                full_name = _repository_text(fields.get("full_name"))
                name = _repository_text(fields.get("name"))
                is_private = fields.get("private")
                if is_private is None:
                    is_private = False
                if identifier == 0 or not full_name or not name or not isinstance(is_private, bool):
                    raise ValueError("invalid repository metadata")
                repository = RepositoryMetadata(
                    identifier, full_name, name, _repository_text(fields.get("description")),
                    _repository_text(fields.get("html_url")), is_private,
                    _repository_text(fields.get("default_branch")),
                    _repository_integer(fields.get("stargazers_count")),
                    _repository_integer(fields.get("forks_count")),
                )
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Gitee returned invalid repository metadata"
                )
            return QueryAttempt.branch_result(FOUND, GetRepositoryOutput(repository), Receipt())
        finally:
            await response.aclose()


def _repository_text(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("invalid repository text")
    return value


def _repository_integer(value: object) -> int:
    if value is None:
        return 0
    if type(value) is not int or not 0 <= value <= (1 << 63) - 1:
        raise ValueError("invalid repository integer")
    return value


Config.__doc__ = "Gitee HTTPS origin, request timeout in seconds, and response bound in bytes."
Credentials.__doc__ = "Secret Gitee personal access token, resolved afresh for every Query."
