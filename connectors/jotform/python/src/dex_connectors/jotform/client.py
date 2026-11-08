# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Jotform metadata reads with Go-compatible provider decisions."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeVar
from urllib.parse import urlencode

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
from ._generated.definitions import GET_SUBMISSION_DEFINITION, LIST_FORMS_DEFINITION
from .models import FormMetadata, FormsPage, GetSubmissionInput, ListFormsInput, SubmissionMetadata

Output = TypeVar("Output")
_NUMERIC_ID = re.compile(r"[0-9]{1,24}\Z")


@dataclass(frozen=True, slots=True)
class _Outcome:
    """A classified provider branch or retry."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self, empty: Output) -> QueryAttempt[Output]:
        """Apply this decision to the operation's empty output."""
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


def _failure(operation: str, branch: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(branch, Failure(kind, "jotform", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Jotform returned invalid JSON"
    ).with_empty(empty)


def _numeric_id(value: object) -> bool:
    return isinstance(value, str) and _NUMERIC_ID.fullmatch(value) is not None


class Client:
    """Resolve Jotform credentials per call and bound each API response."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Jotform configuration and credentials are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Jotform response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Jotform timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_forms(self) -> "ListFormsOperation":
        """Bind the form-list Query."""
        return ListFormsOperation(self)

    def get_submission(self) -> "GetSubmissionOperation":
        """Bind the submission metadata Query."""
        return GetSubmissionOperation(self)

    async def aclose(self) -> None:
        """Close the transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str, has_not_found: bool) -> Any:
        """Fetch JSON or classify a provider failure without retaining its body."""
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
            if not is_header_safe_credential(api_key):
                raise ValueError("invalid API key")
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
                headers={"Accept": "application/json", "APIKEY": api_key},
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Outcome(
                "", Failure(FailureKind.TRANSPORT, "jotform", operation, "Jotform request failed"), True
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "",
                    Failure(FailureKind.RATE_LIMIT, "jotform", operation, "Jotform rate limited the request"),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.AVAILABILITY, "jotform", operation, "Jotform is temporarily unavailable"
                    ),
                    True,
                    retry_after,
                )
            if status == 404 and has_not_found:
                return _failure(
                    operation, "notFound", FailureKind.NOT_FOUND, "Jotform did not find the submission"
                )
            if status != 200:
                return _failure(
                    operation,
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Jotform rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    operation,
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Jotform response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(FailureKind.TRANSPORT, "jotform", operation, "Jotform response was interrupted"),
                    True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "Jotform returned invalid JSON"
                )
        finally:
            await response.aclose()


def _provider_code(
    operation: str, data: dict[str, Any], empty: Output, has_not_found: bool
) -> QueryAttempt[Output] | None:
    code = data.get("responseCode")
    if code is not None and type(code) is not int:
        return _invalid(operation, empty)
    if code == 200:
        return None
    if code == 429 or (isinstance(code, int) and code >= 500):
        return QueryAttempt.retry(
            Failure(
                FailureKind.AVAILABILITY, "jotform", operation, "Jotform reported temporary unavailability"
            )
        )
    if code == 404 and has_not_found:
        return _failure(
            operation, "notFound", FailureKind.NOT_FOUND, "Jotform did not find the submission"
        ).with_empty(empty)
    return _failure(
        operation, "providerRejected", FailureKind.PROVIDER_REJECTION, "Jotform rejected the request"
    ).with_empty(empty)


class ListFormsOperation:
    """List form IDs, titles, and statuses without questions or answers."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated form-list contract."""
        return LIST_FORMS_DEFINITION

    async def invoke(self, call: Call, input: ListFormsInput) -> QueryAttempt[FormsPage]:
        """Read one form page or select a Go-compatible optional branch."""
        empty = FormsPage()
        if (
            not isinstance(input, ListFormsInput)
            or type(input.offset) is not int
            or type(input.limit) is not int
            or input.offset < 0
            or not 0 <= input.limit <= 100
        ):
            return _failure(
                "listForms", "defect", FailureKind.VALIDATION, "offset or limit is invalid"
            ).with_empty(empty)
        limit = input.limit or 20
        response = await self.client.get(
            call, "listForms", "/user/forms?" + urlencode({"offset": input.offset, "limit": limit}), False
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, dict):
            return _invalid("listForms", empty)
        code_result = _provider_code("listForms", response, empty, False)
        if code_result is not None:
            return code_result
        records = response.get("content")
        if not isinstance(records, list):
            return _invalid("listForms", empty)
        forms = []
        for item in records:
            if (
                not isinstance(item, dict)
                or not _numeric_id(item.get("id"))
                or not isinstance(item.get("title"), str)
                or not item["title"]
            ):
                return _invalid("listForms", empty)
            status = item.get("status") or ""
            if not isinstance(status, str):
                return _invalid("listForms", empty)
            forms.append(FormMetadata(item["id"], item["title"], status))
        return QueryAttempt.branch_result("listed", FormsPage(tuple(forms), input.offset, limit), Receipt())


class GetSubmissionOperation:
    """Read one submission's identity and status without answers."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated submission contract."""
        return GET_SUBMISSION_DEFINITION

    async def invoke(self, call: Call, input: GetSubmissionInput) -> QueryAttempt[SubmissionMetadata]:
        """Read one submission or select a Go-compatible optional branch."""
        empty = SubmissionMetadata()
        if not isinstance(input, GetSubmissionInput) or not _numeric_id(input.submission_id):
            return _failure(
                "getSubmission", "defect", FailureKind.VALIDATION, "submission ID must be numeric"
            ).with_empty(empty)
        response = await self.client.get(call, "getSubmission", "/submission/" + input.submission_id, True)
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, dict):
            return _invalid("getSubmission", empty)
        code_result = _provider_code("getSubmission", response, empty, True)
        if code_result is not None:
            return code_result
        content = response.get("content")
        if (
            not isinstance(content, dict)
            or not _numeric_id(content.get("id"))
            or not _numeric_id(content.get("form_id"))
            or not isinstance(content.get("created_at"), str)
            or not content["created_at"]
        ):
            return _invalid("getSubmission", empty)
        status = content.get("status") or ""
        if not isinstance(status, str):
            return _invalid("getSubmission", empty)
        output = SubmissionMetadata(content["id"], content["form_id"], status, content["created_at"])
        return QueryAttempt.branch_result("found", output, Receipt())
