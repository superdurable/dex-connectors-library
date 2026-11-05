# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Forms read operations and Go-compatible branch classification."""

import json
import re
from datetime import UTC, datetime
from typing import TypeVar
from urllib.parse import quote

import httpx
from dex_connectors.sdk import (
    Call,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
    RFC3339NanoTimestamp,
)
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
)

from ._generated.definitions import (
    GET_FORM_DEFINITION,
    GET_RESPONSE_DEFINITION,
    LIST_RESPONSES_DEFINITION,
)
from .client import Client, is_google_id
from .conversion import decode_form, decode_page, decode_response
from .models import Form, FormResponse, GetFormInput, GetResponseInput, ListResponsesInput, ResponsePage

Output = TypeVar("Output", Form, FormResponse, ResponsePage)

_GOOGLE_ERROR_POINTERS = ("/error/status", "/error/details/0/reason", "/error/errors/0/reason")
_RATE_LIMIT_REASONS = frozenset(("RATE_LIMIT_EXCEEDED", "rateLimitExceeded", "userRateLimitExceeded"))


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "google-forms", operation, message)


def _receipt(call: Call, headers: httpx.Headers, object_id: str = "") -> Receipt:
    request_id = headers.get("X-Goog-Request-Id") or headers.get("X-Request-Id", "")
    return Receipt(call.id, "google-forms", datetime.now(UTC), call.idempotency_key, object_id, request_id)


def _path_escape(value: str) -> str:
    return quote(value, safe="-._~$&+=:@")


def _query_escape(value: str) -> str:
    # Go url.QueryEscape escapes '~' and uses '+' for spaces.
    return quote(value, safe="").replace("%20", "+").replace("~", "%7E")


def _query_string(fields: dict[str, str]) -> str:
    return "&".join(f"{_query_escape(key)}={_query_escape(fields[key])}" for key in sorted(fields))


def _utc_timestamp(value: str) -> str:
    timestamp = RFC3339NanoTimestamp.from_wire(value)
    second = timestamp.parsed.astimezone(UTC).isoformat(timespec="seconds").replace("+00:00", "Z")
    fraction = re.match(r"\.\d+", timestamp.wire[19:])
    return second[:-1] + (fraction.group() if fraction else "") + "Z"


def _status_failure_kind(status: int, tokens: tuple[str, ...]) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 429 or status == 403 and any(token in _RATE_LIMIT_REASONS for token in tokens):
        return FailureKind.RATE_LIMIT
    if status == 403:
        return FailureKind.AUTHORIZATION
    if status == 404:
        return FailureKind.NOT_FOUND
    if status == 409:
        return FailureKind.CONFLICT
    if status == 408 or status >= 500:
        return FailureKind.AVAILABILITY
    return FailureKind.PROVIDER_REJECTION


async def _read(
    client: Client,
    call: Call,
    operation: str,
    target: str,
    empty_value: Output,
) -> tuple[bytes | None, QueryAttempt[Output] | None, httpx.Headers]:
    try:
        credential = await client.resolve_credentials(call)
    except Exception:
        return (
            None,
            QueryAttempt.branch_result(
                "defect",
                empty_value,
                Receipt(),
                _failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            ),
            httpx.Headers(),
        )
    try:
        response = await client.get(call, credential, target)
    except (httpx.TransportError, ProviderTransportError):
        return (
            None,
            QueryAttempt.retry(_failure(operation, FailureKind.TRANSPORT, "provider is unavailable")),
            httpx.Headers(),
        )
    except (ValueError, RuntimeError):
        return (
            None,
            QueryAttempt.branch_result(
                "defect",
                empty_value,
                Receipt(),
                _failure(operation, FailureKind.LOCAL_DEFECT, "provider request could not be built"),
            ),
            httpx.Headers(),
        )
    headers = response.headers
    status = response.status_code
    try:
        content = await read_bounded_body(response, max(client.max_response_bytes, MAX_ERROR_BODY_BYTES))
        if 200 <= status < 300 and len(content) > client.max_response_bytes:
            raise BodyTooLargeError(status, headers)
    except BodyTooLargeError:
        if 200 <= status < 300:
            return (
                None,
                QueryAttempt.branch_result(
                    "invalidResponse",
                    empty_value,
                    _receipt(call, headers),
                    _failure(
                        operation,
                        FailureKind.RESPONSE_TOO_LARGE,
                        "provider response exceeds the configured limit",
                    ),
                ),
                headers,
            )
        content = b""
    except ProviderTransportError:
        return (
            None,
            QueryAttempt.retry(_failure(operation, FailureKind.TRANSPORT, "provider is unavailable")),
            headers,
        )
    finally:
        await response.aclose()
    if 200 <= status < 300:
        return content, None, headers
    tokens = read_error_tokens(content, _GOOGLE_ERROR_POINTERS)
    kind = _status_failure_kind(status, tokens)
    if status == 408 or status >= 500 or kind == FailureKind.RATE_LIMIT:
        return (
            None,
            QueryAttempt.retry(
                _failure(operation, kind, "provider temporarily rejected the request"),
                parse_retry_after(headers.get("Retry-After", ""), datetime.now(UTC)),
            ),
            headers,
        )
    if status == 404:
        return (
            None,
            QueryAttempt.branch_result(
                "notFound",
                empty_value,
                _receipt(call, headers),
                _failure(
                    operation,
                    FailureKind.NOT_FOUND,
                    "form or response was not found or is not visible to the connection",
                ),
            ),
            headers,
        )
    return (
        None,
        QueryAttempt.branch_result(
            "providerRejected",
            empty_value,
            _receipt(call, headers),
            _failure(operation, kind, "provider rejected the request"),
        ),
        headers,
    )


class GetFormOperation:
    """Read one form and all its ordered questions."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Google Forms client is required")
        self._client = client

    def definition(self) -> QueryDefinition:
        """Return the generated stable getForm definition."""
        return GET_FORM_DEFINITION

    async def invoke(self, call: Call, input: GetFormInput) -> QueryAttempt[Form]:
        """Call forms.get and classify the response."""
        if not is_google_id(input.form_id):
            return QueryAttempt.branch_result(
                "defect",
                Form(),
                Receipt(),
                _failure("getForm", FailureKind.VALIDATION, "formId must be a Google Forms form ID"),
            )
        target = self._client.api_base_url + "/forms/" + _path_escape(input.form_id)
        body, outcome, headers = await _read(self._client, call, "getForm", target, Form())
        if outcome is not None:
            return outcome
        assert body is not None
        try:
            value = decode_form(json.loads(body), input.form_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                Form(),
                _receipt(call, headers, input.form_id),
                _failure("getForm", FailureKind.PROTOCOL, "provider returned an invalid form"),
            )
        return QueryAttempt.branch_result("found", value, _receipt(call, headers, input.form_id))


class ListResponsesOperation:
    """Read one bounded Google Forms response page."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Google Forms client is required")
        self._client = client

    def definition(self) -> QueryDefinition:
        """Return the generated stable listResponses definition."""
        return LIST_RESPONSES_DEFINITION

    async def invoke(self, call: Call, input: ListResponsesInput) -> QueryAttempt[ResponsePage]:
        """Call forms.responses.list with the exact Go filter and query fields."""
        if (
            not is_google_id(input.form_id)
            or (input.submitted_after[:4] != "0001" and input.submitted_at_or_after[:4] != "0001")
            or len(input.page_token.encode()) > 4096
            or any(ord(char) <= 32 or ord(char) == 127 for char in input.page_token)
        ):
            return QueryAttempt.branch_result(
                "defect",
                ResponsePage(),
                Receipt(),
                _failure("listResponses", FailureKind.VALIDATION, "list responses input is invalid"),
            )
        page_size = input.page_size or self._client.response_page_size
        if not 1 <= page_size <= 1000:
            return QueryAttempt.branch_result(
                "defect",
                ResponsePage(),
                Receipt(),
                _failure(
                    "listResponses",
                    FailureKind.VALIDATION,
                    "pageSize must be from 1 to 1000, or 0 for the connection default",
                ),
            )
        query = {"pageSize": str(page_size)}
        if input.page_token:
            query["pageToken"] = input.page_token
        selected = (
            input.submitted_after if input.submitted_after[:4] != "0001" else input.submitted_at_or_after
        )
        if selected[:4] != "0001":
            timestamp = _utc_timestamp(selected)
            operator = ">" if input.submitted_after[:4] != "0001" else ">="
            query["filter"] = f"timestamp {operator} {timestamp}"
        target = (
            self._client.api_base_url
            + "/forms/"
            + _path_escape(input.form_id)
            + "/responses?"
            + _query_string(query)
        )
        body, outcome, headers = await _read(self._client, call, "listResponses", target, ResponsePage())
        if outcome is not None:
            return outcome
        assert body is not None
        try:
            value = decode_page(json.loads(body), input.form_id, page_size)
        except (ValueError, TypeError, UnicodeDecodeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                ResponsePage(),
                _receipt(call, headers, input.form_id),
                _failure(
                    "listResponses", FailureKind.PROTOCOL, "provider returned an invalid page of responses"
                ),
            )
        return QueryAttempt.branch_result("listed", value, _receipt(call, headers, input.form_id))


class GetResponseOperation:
    """Read one response by form and response identity."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Google Forms client is required")
        self._client = client

    def definition(self) -> QueryDefinition:
        """Return the generated stable getResponse definition."""
        return GET_RESPONSE_DEFINITION

    async def invoke(self, call: Call, input: GetResponseInput) -> QueryAttempt[FormResponse]:
        """Call forms.responses.get and validate the returned response identity."""
        if not is_google_id(input.form_id) or not is_google_id(input.response_id):
            return QueryAttempt.branch_result(
                "defect",
                FormResponse(),
                Receipt(),
                _failure(
                    "getResponse", FailureKind.VALIDATION, "formId and responseId must be Google Forms IDs"
                ),
            )
        target = (
            self._client.api_base_url
            + "/forms/"
            + _path_escape(input.form_id)
            + "/responses/"
            + _path_escape(input.response_id)
        )
        body, outcome, headers = await _read(self._client, call, "getResponse", target, FormResponse())
        if outcome is not None:
            return outcome
        assert body is not None
        try:
            value = decode_response(json.loads(body), input.form_id)
            if value.response_id != input.response_id:
                raise ValueError("response identity differs")
        except (ValueError, TypeError, UnicodeDecodeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                FormResponse(),
                _receipt(call, headers, input.response_id),
                _failure("getResponse", FailureKind.PROTOCOL, "provider returned an invalid response record"),
            )
        return QueryAttempt.branch_result("found", value, _receipt(call, headers, input.response_id))
