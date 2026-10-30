# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""NewsAPI provider call and Go-compatible Query classification."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any

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
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from .models import Article, ArticlePage, ArticleSource, SearchArticlesInput

CONNECTOR_ID = "newsapi"
SEARCH_ARTICLES_OPERATION_ID = "searchArticles"
SEARCHED = "searched"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
SEARCH_IN_FIELDS = ("title", "description", "content")
LANGUAGES = ("ar", "de", "en", "es", "fr", "he", "it", "nl", "no", "pt", "ru", "sv", "ud", "zh")
SORT_ORDERS = ("publishedAt", "relevancy", "popularity")
_SOURCE_ID = re.compile(r"^[a-z0-9][a-z0-9-]{0,99}$")
_DOMAIN = re.compile(r"^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$")
_AUTHENTICATION_CODES = ("apiKeyDisabled", "apiKeyInvalid", "apiKeyMissing")
_PARAMETER_CODES = ("parameterInvalid", "parametersMissing", "sourcesTooMany", "sourceDoesNotExist")
_DOCUMENTED_CODES = (
    "apiKeyExhausted",
    "rateLimited",
    "unexpectedError",
    "maximumResultsReached",
    *_AUTHENTICATION_CODES,
    *_PARAMETER_CODES,
)
SEARCH_ARTICLES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_ARTICLES_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "NewsAPI returned one page of matching articles, possibly empty."),
        BranchDefinition(
            PROVIDER_REJECTED,
            "NewsAPI conclusively rejected the search, such as an invalid or exhausted API key, "
            "a request the plan does not permit, or an unknown source.",
            True,
        ),
        BranchDefinition(INVALID_RESPONSE, "NewsAPI returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()),
)


@dataclass(frozen=True, slots=True)
class Config:
    """NewsAPI HTTP settings; zero or blank values use manifest defaults."""

    base_url: str = "https://newsapi.org/v2"
    timeout_seconds: float = 15
    max_response_bytes: int = 2_097_152


@dataclass(frozen=True, slots=True)
class Credentials:
    """NewsAPI API key, resolved anew for every provider attempt."""

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
            raise ValueError("NewsAPI configuration is required")
        if credentials is None:
            raise ValueError("NewsAPI credential source is required")
        self.base_url = validate_base_url(config.base_url or "https://newsapi.org/v2")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("NewsAPI timeout and max response bytes must be positive")
        timeout_seconds = config.timeout_seconds or 15
        self.max_response_bytes = config.max_response_bytes or 2_097_152
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(timeout_seconds)

    def search_articles(self) -> "SearchArticlesOperation":
        """Bind the everything Query to this client."""
        return SearchArticlesOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()


class SearchArticlesOperation:
    """Classify one NewsAPI everything request per Dex Execute attempt."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("NewsAPI client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return SEARCH_ARTICLES_DEFINITION

    async def invoke(self, call: Call, input: SearchArticlesInput) -> QueryAttempt[ArticlePage]:
        """Validate, send once, then select a branch or safe Query retry."""
        try:
            parameters = _encode_search_query(input)
        except ValueError as error:
            return _failed_branch(DEFECT, FailureKind.VALIDATION, str(error))
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _failed_branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        key = credentials.api_key.reveal()
        if not is_header_safe_credential(key):
            return _failed_branch(DEFECT, FailureKind.AUTHENTICATION, "connection credentials are invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/everything",
                params=parameters,
                headers={
                    "X-Api-Key": key,
                    "Accept": "application/json",
                    "User-Agent": "superdurable-newsapi/0.21",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _failed_branch(DEFECT, FailureKind.LOCAL_DEFECT, "NewsAPI request could not be built")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(_failure(FailureKind.AVAILABILITY, "NewsAPI request failed"))
        if response.status_code != 200:
            return await _classify_error_response(response)
        try:
            body = await read_bounded_body(response, self.client.max_response_bytes)
        except BodyTooLargeError:
            return _failed_branch(
                INVALID_RESPONSE,
                FailureKind.RESPONSE_TOO_LARGE,
                "NewsAPI response exceeded the configured limit",
            )
        except ProviderTransportError:
            return QueryAttempt.retry(_failure(FailureKind.AVAILABILITY, "NewsAPI response was interrupted"))
        return _decode_article_page(body, input)


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, SEARCH_ARTICLES_OPERATION_ID, message)


def _failed_branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ArticlePage]:
    return QueryAttempt.branch_result(branch, ArticlePage(), Receipt(), _failure(kind, message))


def _encode_search_query(input: SearchArticlesInput) -> dict[str, str]:
    if not isinstance(input, SearchArticlesInput):
        raise ValueError("search input is required")
    if any(
        not isinstance(value, str)
        for value in (
            input.query,
            input.from_time,
            input.to_time,
            input.language,
            input.sort_by,
        )
    ):
        raise ValueError("search input text fields must be strings")
    if any(
        not isinstance(values, (tuple, list)) or any(not isinstance(value, str) for value in values)
        for values in (input.search_in, input.sources, input.domains, input.exclude_domains)
    ):
        raise ValueError("search input selections must be strings")
    if type(input.page_size) is not int or type(input.page) is not int:
        raise ValueError("search input page fields must be integers")
    parameters: dict[str, str] = {}
    if len(input.query) > 500:
        raise ValueError("query must be at most 500 characters")
    if not input.query.strip() and not input.sources and not input.domains:
        raise ValueError("query, sources, or domains must be set")
    if input.query.strip():
        parameters["q"] = input.query
    if any(field not in SEARCH_IN_FIELDS for field in input.search_in):
        raise ValueError("searchIn accepts only title, description, and content")
    if input.search_in:
        parameters["searchIn"] = ",".join(input.search_in)
    if len(input.sources) > 20:
        raise ValueError("sources accepts at most 20 source IDs")
    for name, values, pattern in (
        ("sources", input.sources, _SOURCE_ID),
        ("domains", input.domains, _DOMAIN),
        ("excludeDomains", input.exclude_domains, _DOMAIN),
    ):
        if any(pattern.fullmatch(value) is None for value in values):
            raise ValueError(f"{name} contains an invalid entry")
        if values:
            parameters[name] = ",".join(values)
    from_time = _parse_time(input.from_time, "from")
    to_time = _parse_time(input.to_time, "to")
    if from_time and to_time and from_time > to_time:
        raise ValueError("from must not be after to")
    if input.from_time:
        parameters["from"] = input.from_time
    if input.to_time:
        parameters["to"] = input.to_time
    if input.language:
        if input.language not in LANGUAGES:
            raise ValueError("language must be one of " + ", ".join(LANGUAGES))
        parameters["language"] = input.language
    if input.sort_by:
        if input.sort_by not in SORT_ORDERS:
            raise ValueError("sortBy must be publishedAt, relevancy, or popularity")
        parameters["sortBy"] = input.sort_by
    if not 0 <= input.page_size <= 100:
        raise ValueError("pageSize must be from 1 through 100, or zero for NewsAPI's default")
    if input.page_size:
        parameters["pageSize"] = str(input.page_size)
    if input.page < 0:
        raise ValueError("page must be positive, or zero for the first page")
    if input.page:
        parameters["page"] = str(input.page)
    return parameters


def _parse_time(value: str, name: str) -> datetime | None:
    if not value:
        return None
    try:
        if re.fullmatch(r"\d{4}-\d{2}-\d{2}", value):
            return datetime.strptime(value, "%Y-%m-%d").replace(tzinfo=UTC)
        if re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}", value):
            return datetime.strptime(value, "%Y-%m-%dT%H:%M:%S").replace(tzinfo=UTC)
        if not re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})", value):
            raise ValueError
        return datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(UTC)
    except ValueError:
        raise ValueError(f"{name} must be a date such as 2026-01-05 or an ISO 8601 time") from None


async def _classify_error_response(response: httpx.Response) -> QueryAttempt[ArticlePage]:
    try:
        body = await read_bounded_body(response, MAX_ERROR_BODY_BYTES)
    except (BodyTooLargeError, ProviderTransportError):
        body = b""
    codes = read_error_tokens(body, ("/code",))
    code = codes[0] if len(codes) == 1 else ""
    described = code if code in _DOCUMENTED_CODES else "no documented error code"
    status = response.status_code
    if code == "apiKeyExhausted":
        return _failed_branch(
            PROVIDER_REJECTED,
            FailureKind.QUOTA_EXHAUSTED,
            f"NewsAPI reports the API key's request allowance is exhausted ({described})",
        )
    if status == 429:
        delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
        return QueryAttempt.retry(
            _failure(FailureKind.RATE_LIMIT, f"NewsAPI rate limited the request ({described})"), delay
        )
    if status == 408 or status >= 500:
        delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
        return QueryAttempt.retry(
            _failure(FailureKind.AVAILABILITY, f"NewsAPI is temporarily unavailable ({described})"), delay
        )
    if status == 401 or code in _AUTHENTICATION_CODES:
        return _failed_branch(
            PROVIDER_REJECTED, FailureKind.AUTHENTICATION, f"NewsAPI rejected the API key ({described})"
        )
    if status == 426:
        return _failed_branch(
            PROVIDER_REJECTED,
            FailureKind.AUTHORIZATION,
            f"NewsAPI's plan does not permit this request ({described})",
        )
    if code in _PARAMETER_CODES:
        return _failed_branch(
            PROVIDER_REJECTED,
            FailureKind.VALIDATION,
            f"NewsAPI rejected the search parameters ({described})",
        )
    return _failed_branch(
        PROVIDER_REJECTED,
        FailureKind.PROVIDER_REJECTION,
        f"NewsAPI rejected the request with HTTP {status} ({described})",
    )


def _decode_article_page(body: bytes, input: SearchArticlesInput) -> QueryAttempt[ArticlePage]:
    try:
        document = json.loads(body)
        if not isinstance(document, dict) or document.get("status") != "ok":
            raise ValueError
        total = document["totalResults"]
        articles_wire = document["articles"]
        if type(total) is not int or total < 0 or not isinstance(articles_wire, list):
            raise ValueError
        if len(articles_wire) > (input.page_size or 100):
            return _failed_branch(
                INVALID_RESPONSE,
                FailureKind.PROTOCOL,
                "NewsAPI returned more articles than the page size",
            )
        articles = tuple(_decode_article(article) for article in articles_wire)
    except (ValueError, TypeError, KeyError, UnicodeDecodeError):
        return _failed_branch(
            INVALID_RESPONSE, FailureKind.PROTOCOL, "NewsAPI returned an incomplete article page"
        )
    page = max(input.page, 1)
    page_size = input.page_size or 100
    return QueryAttempt.branch_result(
        SEARCHED, ArticlePage(total, articles, page, page * page_size < total), Receipt()
    )


def _decode_article(value: Any) -> Article:
    if not isinstance(value, dict):
        raise ValueError
    source = value.get("source")
    if source is None:
        source = {}
    if not isinstance(source, dict):
        raise ValueError
    publication_text = _nullable_string_field(value, "publishedAt")
    if publication_text is None:
        publication = datetime.min.replace(tzinfo=UTC)
    else:
        if not re.fullmatch(
            r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})",
            publication_text,
        ):
            raise ValueError
        publication = datetime.fromisoformat(publication_text.replace("Z", "+00:00")).astimezone(UTC)
    return Article(
        ArticleSource(_nullable_string_field(source, "id"), _string_field(source, "name")),
        _nullable_string_field(value, "author"),
        _string_field(value, "title"),
        _nullable_string_field(value, "description"),
        _string_field(value, "url"),
        _nullable_string_field(value, "urlToImage"),
        publication,
        _nullable_string_field(value, "content"),
    )


def _string_field(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


def _nullable_string_field(document: dict[str, Any], name: str) -> str | None:
    value = document.get(name)
    if value is not None and not isinstance(value, str):
        raise ValueError
    return value
