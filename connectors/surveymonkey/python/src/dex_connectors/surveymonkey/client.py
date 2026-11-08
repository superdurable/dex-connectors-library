# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated SurveyMonkey reads with Go-compatible decisions."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeVar

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
from ._generated.definitions import GET_RESPONSE_DEFINITION, LIST_SURVEYS_DEFINITION
from .models import GetResponseInput, ListSurveysInput, ResponseMetadata, SurveyMetadata, SurveysPage

_NUMERIC_ID = re.compile(r"[0-9]{1,24}\Z")
Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class _Outcome:
    """One classified provider failure or retry."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self, empty: Output) -> QueryAttempt[Output]:
        """Apply the decision to an operation's empty output type."""
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


def _failure(operation: str, branch: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(branch, Failure(kind, "surveymonkey", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "SurveyMonkey returned invalid JSON"
    ).with_empty(empty)


def _is_numeric_id(value: object) -> bool:
    return isinstance(value, str) and _NUMERIC_ID.fullmatch(value) is not None


def _page_and_size(input: ListSurveysInput) -> tuple[int, int] | None:
    if type(input.page) is not int or type(input.per_page) is not int:
        return None
    if input.page < 0 or input.per_page < 0 or input.per_page > 100:
        return None
    return input.page or 1, input.per_page or 50


class Client:
    """One redirect-safe provider transport with call-time credential resolution."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("SurveyMonkey configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("SurveyMonkey response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("SurveyMonkey timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_surveys(self) -> "ListSurveysOperation":
        """Bind the survey metadata Query."""
        return ListSurveysOperation(self)

    def get_response(self) -> "GetResponseOperation":
        """Bind the response metadata Query."""
        return GetResponseOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str, has_not_found: bool) -> Any:
        """Fetch JSON or return a classified failure without exposing provider data."""
        try:
            credential = await self.credentials.resolve(call)
            token = credential.access_token.reveal()
            if not is_header_safe_credential(token):
                raise ValueError("invalid access token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _failure(
                operation,
                "defect",
                FailureKind.AUTHENTICATION,
                "connection credentials are unavailable or invalid",
            )
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={"Accept": "application/json", "Authorization": "Bearer " + token},
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "surveymonkey", operation, "SurveyMonkey request failed"),
                True,
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "surveymonkey", operation, "SurveyMonkey request failed"),
                True,
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.RATE_LIMIT,
                        "surveymonkey",
                        operation,
                        "SurveyMonkey rate limited the request",
                    ),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.AVAILABILITY,
                        "surveymonkey",
                        operation,
                        "SurveyMonkey is temporarily unavailable",
                    ),
                    True,
                    retry_after,
                )
            if status == 404 and has_not_found:
                return _failure(
                    operation, "notFound", FailureKind.NOT_FOUND, "SurveyMonkey did not find the resource"
                )
            if status != 200:
                return _failure(
                    operation,
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"SurveyMonkey rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    operation,
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "SurveyMonkey response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.TRANSPORT,
                        "surveymonkey",
                        operation,
                        "SurveyMonkey response was interrupted",
                    ),
                    True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "SurveyMonkey returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListSurveysOperation:
    """List one page of surveys accessible to the connected account."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return LIST_SURVEYS_DEFINITION

    async def invoke(self, call: Call, input: ListSurveysInput) -> QueryAttempt[SurveysPage]:
        """Fetch one survey page or select a Go-compatible optional branch."""
        empty = SurveysPage()
        pagination = _page_and_size(input) if isinstance(input, ListSurveysInput) else None
        if pagination is None:
            return _failure(
                "listSurveys", "defect", FailureKind.VALIDATION, "page or perPage is invalid"
            ).with_empty(empty)
        page, per_page = pagination
        response = await self.client.get(
            call, "listSurveys", f"/surveys?page={page}&per_page={per_page}", False
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, dict):
            return _invalid("listSurveys", empty)
        data = response.get("data")
        actual_page = response.get("page")
        actual_size = response.get("per_page")
        total = response.get("total")
        if (
            not isinstance(data, list)
            or type(actual_page) is not int
            or actual_page != page
            or type(actual_size) is not int
            or actual_size < 1
            or type(total) is not int
            or total < 0
        ):
            return _invalid("listSurveys", empty)
        if any(not _valid_survey(item) for item in data):
            return _invalid("listSurveys", empty)
        output = SurveysPage(
            tuple(SurveyMetadata(item["id"], item.get("title", "")) for item in data),
            actual_page,
            actual_size,
            total,
        )
        return QueryAttempt.branch_result("listed", output, Receipt())


class GetResponseOperation:
    """Read one response's identity and status without answers."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_RESPONSE_DEFINITION

    async def invoke(self, call: Call, input: GetResponseInput) -> QueryAttempt[ResponseMetadata]:
        """Fetch metadata after validating both numeric IDs."""
        empty = ResponseMetadata()
        if (
            not isinstance(input, GetResponseInput)
            or not _is_numeric_id(input.survey_id)
            or not _is_numeric_id(input.response_id)
        ):
            return _failure(
                "getResponse",
                "defect",
                FailureKind.VALIDATION,
                "survey and response IDs must be numeric",
            ).with_empty(empty)
        response = await self.client.get(
            call, "getResponse", f"/surveys/{input.survey_id}/responses/{input.response_id}", True
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if (
            not isinstance(response, dict)
            or not _is_numeric_id(response.get("id"))
            or not _is_numeric_id(response.get("survey_id"))
            or not isinstance(response.get("response_status"), str)
            or not response["response_status"]
            or not isinstance(response.get("date_created", ""), str)
            or not isinstance(response.get("date_modified", ""), str)
        ):
            return _invalid("getResponse", empty)
        output = ResponseMetadata(
            response["id"],
            response["survey_id"],
            response["response_status"],
            response.get("date_created", ""),
            response.get("date_modified", ""),
        )
        return QueryAttempt.branch_result("found", output, Receipt())


def _valid_survey(item: object) -> bool:
    return (
        isinstance(item, dict) and _is_numeric_id(item.get("id")) and isinstance(item.get("title", ""), str)
    )
