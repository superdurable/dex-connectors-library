# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""TMDB provider calls and Go-compatible branch classification."""

import json
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    SecretString,
    StepDefaults,
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
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from .models import GetMovieInput, Movie, MoviePage, SearchMoviesInput

CONNECTOR_ID = "tmdb"
SEARCH_MOVIES_OPERATION_ID = "searchMovies"
GET_MOVIE_OPERATION_ID = "getMovie"
SEARCHED = "searched"
FOUND = "found"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
_DEFAULT_BASE_URL = "https://api.themoviedb.org/3"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_SHARED_BRANCHES = (
    BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
    BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
    BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
)
SEARCH_MOVIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_MOVIES_OPERATION_ID),
    (BranchDefinition(SEARCHED, "TMDB returned the requested result."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)
GET_MOVIE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_MOVIE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "TMDB returned the requested result."),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist.", True),
        *_SHARED_BRANCHES,
    ),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """TMDB HTTP settings; zero or blank values use manifest defaults."""

    base_url: str = _DEFAULT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """TMDB API Read Access Token, resolved anew for every provider attempt."""

    api_key: SecretString


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("TMDB configuration is required")
        if credentials is None:
            raise ValueError("TMDB credential source is required")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("TMDB timeout and max response bytes must be positive")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def search_movies(self) -> "SearchMoviesOperation":
        """Bind the movie search Query to this client."""
        return SearchMoviesOperation(self)

    def get_movie(self) -> "GetMovieOperation":
        """Bind the movie lookup Query to this client."""
        return GetMovieOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def _run_query(
        self,
        call: Call,
        operation: str,
        path: str,
        empty: Output,
        success: str,
        decode: Callable[[dict[str, Any]], Output],
        has_not_found_branch: bool = False,
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go runQuery helper."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _branch(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        token = credentials.api_key.reveal()
        if not token.strip() or not is_header_safe_credential(token):
            return _branch(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are invalid"
            )
        headers = {
            "Authorization": "Bearer " + token,
            "Accept": "application/vnd.tmdb+json;version=2",
            "User-Agent": "superdurable-tmdb/0.1",
        }
        try:
            request = self.http_client.build_request("GET", self.base_url + path, headers=headers)
        except (httpx.HTTPError, ValueError):
            return _branch(
                operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "TMDB request could not be built"
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(
                _failure(operation, FailureKind.AVAILABILITY, "provider request failed")
            )
        try:
            status = response.status_code
            if status == 429:
                return QueryAttempt.retry(
                    _failure(operation, FailureKind.RATE_LIMIT, "provider rate limited the request"),
                    _retry_after_seconds(response),
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    _failure(operation, FailureKind.AVAILABILITY, "provider is temporarily unavailable"),
                    _retry_after_seconds(response),
                )
            if status == 404 and has_not_found_branch:
                return _branch(
                    operation, NOT_FOUND, empty, FailureKind.NOT_FOUND, "provider resource was not found"
                )
            if status != 200:
                return _branch(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected the request with HTTP {status}",
                )
            try:
                content = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(operation, FailureKind.AVAILABILITY, "provider response was interrupted")
                )
            try:
                document = json.loads(content)
                if not isinstance(document, dict):
                    raise ValueError
                value = decode(document)
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.PROTOCOL,
                    "provider response is incomplete or malformed",
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class SearchMoviesOperation:
    """Search TMDB movies by title text, one page at a time."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("TMDB client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return SEARCH_MOVIES_DEFINITION

    async def invoke(self, call: Call, input: SearchMoviesInput) -> QueryAttempt[MoviePage]:
        """Validate the query and page, read once, then select a branch or safe retry."""
        operation = SEARCH_MOVIES_OPERATION_ID
        if (
            not isinstance(input, SearchMoviesInput)
            or not isinstance(input.query, str)
            or type(input.page) is not int
            or not input.query.strip()
            or not 0 <= input.page <= 500
        ):
            return _branch(
                operation,
                DEFECT,
                MoviePage(),
                FailureKind.VALIDATION,
                "query is required and page must be 1–500",
            )
        parameters = {"query": input.query}
        if input.page > 0:
            parameters["page"] = str(input.page)
        path = "/search/movie?" + urlencode(parameters)
        return await self.client._run_query(call, operation, path, MoviePage(), SEARCHED, _decode_movie_page)


class GetMovieOperation:
    """Read one TMDB movie by ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("TMDB client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_MOVIE_DEFINITION

    async def invoke(self, call: Call, input: GetMovieInput) -> QueryAttempt[Movie]:
        """Validate the movie ID, read once, then select a branch or safe retry."""
        operation = GET_MOVIE_OPERATION_ID
        if not isinstance(input, GetMovieInput) or type(input.id) is not int or input.id <= 0:
            return _branch(operation, DEFECT, Movie(), FailureKind.VALIDATION, "movie ID must be positive")

        def decode_requested_movie(document: dict[str, Any]) -> Movie:
            movie = _decode_movie(document)
            if movie.id != input.id:
                raise ValueError
            return movie

        return await self.client._run_query(
            call,
            operation,
            f"/movie/{input.id}",
            Movie(),
            FOUND,
            decode_requested_movie,
            has_not_found_branch=True,
        )


def _retry_after_seconds(response: httpx.Response) -> float:
    return parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)


def _branch(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(branch, value, Receipt(), _failure(operation, kind, message))


def _decode_movie_page(document: dict[str, Any]) -> MoviePage:
    results = _field(document, "results")
    page = _integer(document, "page")
    if not isinstance(results, list) or page <= 0:
        raise ValueError
    return MoviePage(
        page,
        _integer(document, "total_pages"),
        _integer(document, "total_results"),
        tuple(_decode_movie(movie) for movie in results),
    )


def _decode_movie(document: Any) -> Movie:
    if not isinstance(document, dict):
        raise ValueError
    return Movie(
        _integer(document, "id"),
        _string(document, "title"),
        _string(document, "overview"),
        _string(document, "release_date"),
    )


def _field(document: dict[str, Any], name: str) -> Any:
    """Match Go's case-insensitive JSON field lookup, preferring an exact key."""
    if name in document:
        return document[name]
    folded = name.casefold()
    for key, value in document.items():
        if key.casefold() == folded:
            return value
    return None


def _string(document: dict[str, Any], name: str) -> str:
    value = _field(document, name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


def _integer(document: dict[str, Any], name: str) -> int:
    value = _field(document, name)
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError
    return value
