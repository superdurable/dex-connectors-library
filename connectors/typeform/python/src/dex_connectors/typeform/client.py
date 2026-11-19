# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typeform form and response reads and webhook upserts.

Requests, response decoding, and branch classification follow the Go connector.
upsertWebhook is a PUT keyed by the webhook tag, so a repeated attempt leaves one webhook.
"""

import json
import re
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any, NoReturn, TypeVar
from urllib.parse import quote, urlencode, urlsplit

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    Receipt,
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
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from .credentials import Credentials
from .models import (
    ZERO_TIME,
    Form,
    FormAnswer,
    FormAnswerChoice,
    FormAnswerChoices,
    FormAnswerMultiFormat,
    FormAnswerPayment,
    FormAnswerSignature,
    FormField,
    FormFieldChoice,
    FormPage,
    FormResponse,
    FormSummary,
    FormVariable,
    GetFormInput,
    ListFormsInput,
    ListResponsesInput,
    ResponsePage,
    UpsertWebhookInput,
    Webhook,
)

CONNECTOR_ID = "typeform"
LIST_FORMS_OPERATION_ID = "listForms"
GET_FORM_OPERATION_ID = "getForm"
LIST_RESPONSES_OPERATION_ID = "listResponses"
UPSERT_WEBHOOK_OPERATION_ID = "upsertWebhook"
LISTED = "listed"
FOUND = "found"
UPSERTED = "upserted"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
DATA_CENTER_US = "us"
DATA_CENTER_EU = "eu"
DATA_CENTER_NEW_EU = "newEu"
WEBHOOK_EVENT_TYPE_FORM_RESPONSE = "form_response"
ANSWER_TYPE_TEXT = "text"
ANSWER_TYPE_EMAIL = "email"
ANSWER_TYPE_URL = "url"
ANSWER_TYPE_FILE_URL = "file_url"
ANSWER_TYPE_DATE = "date"
ANSWER_TYPE_PHONE_NUMBER = "phone_number"
ANSWER_TYPE_NUMBER = "number"
ANSWER_TYPE_BOOLEAN = "boolean"
ANSWER_TYPE_CHOICE = "choice"
ANSWER_TYPE_CHOICES = "choices"
ANSWER_TYPE_PAYMENT = "payment"
ANSWER_TYPE_SIGNATURE = "signature"
ANSWER_TYPE_MULTI_FORMAT = "multi_format"
API_BASE_URLS = {
    DATA_CENTER_US: "https://api.typeform.com",
    DATA_CENTER_EU: "https://api.eu.typeform.com",
    DATA_CENTER_NEW_EU: "https://api.typeform.eu",
}
DEFAULT_MAX_RESPONSE_BYTES = 1_048_576
DEFAULT_WEBHOOK_MAX_BODY_BYTES = 1_048_576
_REQUEST_TIMEOUT_SECONDS = 25.0
_MAXIMUM_FORM_PAGE_SIZE = 200
_MAXIMUM_FORM_PAGE = 10000
_MAXIMUM_FORM_SEARCH_BYTES = 256
_MAXIMUM_FIELD_NESTING_DEPTH = 4
_MAXIMUM_RESPONSE_PAGE_SIZE = 1000
_DEFAULT_RESPONSE_PAGE_SIZE = 25
_MAXIMUM_WEBHOOK_URL_BYTES = 2048
_TYPEFORM_ID_PATTERN = re.compile(r"^[A-Za-z0-9_-]{1,128}\Z")
_WEBHOOK_TAG_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}\Z")
_RFC3339_PATTERN = re.compile(
    r"^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(\.\d+)?([Zz]|[+-]\d{2}:\d{2})\Z"
)
_INVALID_PERCENT_ESCAPE = re.compile(r"%(?![0-9A-Fa-f]{2})")
_CREDENTIALS_UNAVAILABLE = "the Typeform connection could not be loaded yet"
_ACCESS_TOKEN_UNUSABLE = "the Typeform access token is blank or contains characters a header cannot carry"
_REQUEST_INVALID = "Typeform request could not be built"
_RESPONSE_TOO_LARGE = "Typeform response exceeds the configured maxResponseBytes"
_RESPONSE_MALFORMED = "Typeform returned a malformed response"
_FORM_ID_MESSAGE = "formId must be a Typeform form ID such as u6nXL7, the part after /to/ in the form link"
_DEFECT_BRANCH = BranchDefinition(
    DEFECT, "Local input, connection configuration, or connector definition is invalid.", True
)
_INVALID_RESPONSE_BRANCH = BranchDefinition(
    INVALID_RESPONSE, "Typeform returned a malformed or oversized response.", True
)
_FORM_NOT_FOUND_BRANCH = BranchDefinition(
    NOT_FOUND, "The form does not exist or the token's account does not own it.", True
)
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 0, execute_retry=default_connector_step_execute_retry_policy()
)
LIST_FORMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_FORMS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of forms was returned; it may be empty."),
        BranchDefinition(
            PROVIDER_REJECTED,
            "Typeform conclusively rejected the request, such as an invalid token, a missing forms:read "
            "scope, or an unknown workspace.",
            True,
        ),
        _INVALID_RESPONSE_BRANCH,
        _DEFECT_BRANCH,
    ),
    _STEP_DEFAULTS,
)
GET_FORM_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_FORM_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The form's fields and hidden field names were read."),
        _FORM_NOT_FOUND_BRANCH,
        BranchDefinition(
            PROVIDER_REJECTED,
            "Typeform conclusively rejected the request, such as an invalid token or a missing forms:read "
            "scope.",
            True,
        ),
        _INVALID_RESPONSE_BRANCH,
        _DEFECT_BRANCH,
    ),
    _STEP_DEFAULTS,
)
LIST_RESPONSES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_RESPONSES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of completed responses was returned; it may be empty."),
        _FORM_NOT_FOUND_BRANCH,
        BranchDefinition(
            PROVIDER_REJECTED,
            "Typeform conclusively rejected the request, such as an invalid token, a missing "
            "responses:read scope, or an invalid filter.",
            True,
        ),
        _INVALID_RESPONSE_BRANCH,
        _DEFECT_BRANCH,
    ),
    _STEP_DEFAULTS,
)
UPSERT_WEBHOOK_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPSERT_WEBHOOK_OPERATION_ID),
    (
        BranchDefinition(UPSERTED, "The webhook with this tag holds the requested URL and enabled state."),
        _FORM_NOT_FOUND_BRANCH,
        BranchDefinition(
            PROVIDER_REJECTED,
            "Typeform conclusively rejected the webhook, such as a URL that is not HTTPS, a plan without "
            "webhooks, or a token without the webhooks:write scope.",
            True,
        ),
        BranchDefinition(
            INVALID_RESPONSE,
            "Typeform answered 2xx with a malformed or oversized body, so it may have stored the webhook "
            "without this Result describing it.",
            True,
        ),
        BranchDefinition(
            DEFECT,
            "Local input, connection configuration such as a blank webhook_secret, or connector definition "
            "is invalid.",
            True,
        ),
    ),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


class _MalformedResponseError(ValueError):
    """A Typeform JSON document that Go's decoding or conversion rejects."""


@dataclass(frozen=True, slots=True)
class Config:
    """The Responses Data Center and the response and webhook body limits; zero uses defaults."""

    data_center: str = DATA_CENTER_US
    max_response_bytes: int = DEFAULT_MAX_RESPONSE_BYTES
    webhook_max_body_bytes: int = DEFAULT_WEBHOOK_MAX_BODY_BYTES


@dataclass(frozen=True, slots=True)
class _Response:
    status: int
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""


@dataclass(frozen=True, slots=True)
class _Outcome:
    """A terminal branch or a retry, before the operation supplies its empty value."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0
    has_receipt: bool = False


@dataclass(frozen=True, slots=True)
class _FailureBranches:
    not_found: str
    provider_rejected: str
    invalid_response: str


class Client:
    """One data center's API host, redirect-safe HTTP client, and credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        api_base_url: str = "",
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Typeform configuration is required")
        data_center = config.data_center or DATA_CENTER_US
        if data_center not in API_BASE_URLS:
            raise ValueError("configuration dataCenter is invalid")
        if config.max_response_bytes < 0:
            raise ValueError("configuration maxResponseBytes is invalid")
        if config.webhook_max_body_bytes < 0:
            raise ValueError("configuration webhookMaxBodyBytes is invalid")
        if credentials is None:
            raise ValueError("Typeform credential provider is required")
        self.api_base_url = validate_base_url(api_base_url) if api_base_url else API_BASE_URLS[data_center]
        self.max_response_bytes = config.max_response_bytes or DEFAULT_MAX_RESPONSE_BYTES
        self.webhook_max_body_bytes = config.webhook_max_body_bytes or DEFAULT_WEBHOOK_MAX_BODY_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(_REQUEST_TIMEOUT_SECONDS)
        self.now = now or (lambda: datetime.now(UTC))

    def list_forms(self) -> "ListFormsOperation":
        """Bind the listForms Query to this client."""
        return ListFormsOperation(self)

    def get_form(self) -> "GetFormOperation":
        """Bind the getForm Query to this client."""
        return GetFormOperation(self)

    def list_responses(self) -> "ListResponsesOperation":
        """Bind the listResponses Query to this client."""
        return ListResponsesOperation(self)

    def upsert_webhook(self) -> "UpsertWebhookOperation":
        """Bind the upsertWebhook Mutation to this client."""
        return UpsertWebhookOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def resolve_credentials(self, call: Call, operation: str) -> Credentials | _Outcome:
        """Resolve the token; a load failure retries and an unusable token is a defect."""
        try:
            credentials = await self.credentials.resolve(call)
        except Exception:
            failure = _failure(operation, FailureKind.AVAILABILITY, _CREDENTIALS_UNAVAILABLE)
            return _Outcome("", failure, is_retry=True)
        if not is_header_safe_credential(credentials.access_token.reveal()):
            return _Outcome(DEFECT, _failure(operation, FailureKind.AUTHENTICATION, _ACCESS_TOKEN_UNUSABLE))
        return credentials

    async def exchange(
        self,
        credentials: Credentials,
        operation: str,
        method: str,
        path: str,
        branches: _FailureBranches,
        query: dict[str, str] | None = None,
        payload: dict[str, Any] | None = None,
    ) -> _Response | _Outcome:
        """Send once and return a 2xx response, or the branch or retry Go's classifyExchange selects."""
        target = self.api_base_url + path
        if query:
            target += "?" + urlencode(sorted(query.items()))
        headers = {"Authorization": "Bearer " + credentials.access_token.reveal(), "Accept": "application/json"}
        content = None
        if payload is not None:
            headers["Content-Type"] = "application/json"
            content = _go_json(payload)
        try:
            request = self.http_client.build_request(method, target, content=content, headers=headers)
        except (ValueError, TypeError, httpx.HTTPError):
            return _Outcome(DEFECT, _failure(operation, FailureKind.LOCAL_DEFECT, _REQUEST_INVALID))
        transport_failure = _Outcome(
            "",
            _failure(operation, FailureKind.TRANSPORT, "Typeform could not be reached or its answer was lost"),
            is_retry=True,
        )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return transport_failure
        try:
            status = response.status_code
            if 200 <= status < 300:
                try:
                    body = await read_bounded_body(response, self.max_response_bytes)
                except BodyTooLargeError:
                    failure = _failure(operation, FailureKind.RESPONSE_TOO_LARGE, _RESPONSE_TOO_LARGE)
                    return _Outcome(branches.invalid_response, failure, has_receipt=True)
                except ProviderTransportError:
                    return transport_failure
                return _Response(status, response.headers, body)
            try:
                body = await read_bounded_body(response, MAX_ERROR_BODY_BYTES)
            except (BodyTooLargeError, ProviderTransportError):
                body = b""
        finally:
            await response.aclose()
        return self._classify_failure(operation, _Response(status, response.headers, body), branches)

    def receipt(self, object_id: str) -> Receipt:
        """Correlate a provider outcome with the Typeform object it concerns."""
        return Receipt(provider=CONNECTOR_ID, provider_object_id=object_id, observed_at=self.now().astimezone(UTC))

    def query_attempt(self, outcome: _Outcome, empty: Output, object_id: str) -> QueryAttempt[Output]:
        """Convert an outcome into a Query retry or branch with an empty value."""
        if outcome.is_retry:
            return QueryAttempt.retry(outcome.failure, outcome.retry_after_seconds)
        receipt = self.receipt(object_id) if outcome.has_receipt else Receipt()
        return QueryAttempt.branch_result(outcome.branch, empty, receipt, outcome.failure)

    def mutation_attempt(self, outcome: _Outcome, empty: Output, object_id: str) -> MutationAttempt[Output]:
        """Convert an outcome into a Mutation retry or branch with an empty value."""
        if outcome.is_retry:
            return MutationAttempt.retry(outcome.failure, outcome.retry_after_seconds)
        receipt = self.receipt(object_id) if outcome.has_receipt else Receipt()
        return MutationAttempt.branch_result(outcome.branch, empty, receipt, outcome.failure)

    def _classify_failure(self, operation: str, response: _Response, branches: _FailureBranches) -> _Outcome:
        status = response.status

        def terminal(kind: FailureKind, message: str) -> _Outcome:
            branch = branches.provider_rejected
            if status == 404 and branches.not_found:
                branch = branches.not_found
            return _Outcome(branch, _failure(operation, kind, message), has_receipt=True)

        if status == 429:
            retry_after = parse_retry_after(str(response.headers.get("Retry-After", "")), self.now())
            message = "Typeform rate limited the request; it allows two requests per second per account"
            return _Outcome("", _failure(operation, FailureKind.RATE_LIMIT, message), True, retry_after)
        if status == 408 or status >= 500:
            message = f"Typeform answered HTTP {status}"
            return _Outcome("", _failure(operation, FailureKind.AVAILABILITY, message), is_retry=True)
        if status == 401:
            return terminal(
                FailureKind.AUTHENTICATION,
                "Typeform rejected the access token; generate a new personal access token",
            )
        if status == 403:
            return terminal(
                FailureKind.AUTHORIZATION,
                "Typeform denied the request; check that the token is valid and has the scopes in the "
                "connector guide",
            )
        if status == 402:
            return terminal(
                FailureKind.PROVIDER_REJECTION,
                "the Typeform account's plan does not include a feature this request uses",
            )
        if status == 404:
            return terminal(FailureKind.NOT_FOUND, "the Typeform form was not found")
        if status == 409:
            return terminal(FailureKind.CONFLICT, "Typeform reported a conflicting change")
        if status == 400 and _typeform_error_code(response.body) == "webhook_url_https_required":
            return terminal(FailureKind.VALIDATION, "Typeform requires an HTTPS webhook URL")
        return terminal(FailureKind.PROVIDER_REJECTION, f"Typeform rejected the request with HTTP {status}")


class ListFormsOperation:
    """List one page of the account's forms, optionally filtered by search text or workspace."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Typeform client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_FORMS_DEFINITION

    async def invoke(self, call: Call, input: ListFormsInput) -> QueryAttempt[FormPage]:
        """Validate, read one page, and convert it."""
        operation, client, empty = LIST_FORMS_OPERATION_ID, self.client, FormPage()
        try:
            query, page = _list_forms_query(input)
        except ValueError as error:
            return client.query_attempt(_validation_outcome(operation, error), empty, "")
        credentials = await client.resolve_credentials(call, operation)
        if isinstance(credentials, _Outcome):
            return client.query_attempt(credentials, empty, "")
        branches = _FailureBranches("", PROVIDER_REJECTED, INVALID_RESPONSE)
        response = await client.exchange(credentials, operation, "GET", "/forms", branches, query)
        if isinstance(response, _Outcome):
            return client.query_attempt(response, empty, "")
        try:
            form_page = _convert_form_page(_decode_object(response.body), page)
        except _MalformedResponseError:
            return client.query_attempt(_malformed_outcome(operation, INVALID_RESPONSE), empty, "")
        return QueryAttempt.branch_result(LISTED, form_page, client.receipt(""))


class GetFormOperation:
    """Read one form's fields, choices, and hidden field names."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Typeform client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_FORM_DEFINITION

    async def invoke(self, call: Call, input: GetFormInput) -> QueryAttempt[Form]:
        """Validate, read the form, and flatten its question groups."""
        operation, client, empty = GET_FORM_OPERATION_ID, self.client, Form()
        if not isinstance(input, GetFormInput) or not _is_typeform_id(input.form_id):
            return client.query_attempt(_validation_outcome(operation, ValueError(_FORM_ID_MESSAGE)), empty, "")
        credentials = await client.resolve_credentials(call, operation)
        if isinstance(credentials, _Outcome):
            return client.query_attempt(credentials, empty, "")
        branches = _FailureBranches(NOT_FOUND, PROVIDER_REJECTED, INVALID_RESPONSE)
        response = await client.exchange(credentials, operation, "GET", "/forms/" + input.form_id, branches)
        if isinstance(response, _Outcome):
            return client.query_attempt(response, empty, input.form_id)
        try:
            form = _convert_form(_decode_object(response.body))
        except _MalformedResponseError:
            form = None
        if form is None or form.id != input.form_id:
            return client.query_attempt(_malformed_outcome(operation, INVALID_RESPONSE), empty, input.form_id)
        return QueryAttempt.branch_result(FOUND, form, client.receipt(form.id))


class ListResponsesOperation:
    """List one bounded page of a form's completed responses, newest first."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Typeform client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_RESPONSES_DEFINITION

    async def invoke(self, call: Call, input: ListResponsesInput) -> QueryAttempt[ResponsePage]:
        """Validate filters, read one page, and convert its answers."""
        operation, client, empty = LIST_RESPONSES_OPERATION_ID, self.client, ResponsePage()
        try:
            query = _list_responses_query(input)
        except ValueError as error:
            return client.query_attempt(_validation_outcome(operation, error), empty, "")
        credentials = await client.resolve_credentials(call, operation)
        if isinstance(credentials, _Outcome):
            return client.query_attempt(credentials, empty, "")
        branches = _FailureBranches(NOT_FOUND, PROVIDER_REJECTED, INVALID_RESPONSE)
        path = "/forms/" + input.form_id + "/responses"
        response = await client.exchange(credentials, operation, "GET", path, branches, query)
        if isinstance(response, _Outcome):
            return client.query_attempt(response, empty, input.form_id)
        try:
            page = _convert_response_page(_decode_object(response.body), input)
        except _MalformedResponseError:
            return client.query_attempt(_malformed_outcome(operation, INVALID_RESPONSE), empty, input.form_id)
        return QueryAttempt.branch_result(LISTED, page, client.receipt(input.form_id))


class UpsertWebhookOperation:
    """Create or replace a form's form_response webhook by tag, signed with webhook_secret."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Typeform client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return UPSERT_WEBHOOK_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpsertWebhookInput) -> IdempotencyKey:
        """Use the Call ID; the tag-keyed PUT is itself safe to repeat."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpsertWebhookInput) -> MutationAttempt[Webhook]:
        """Validate, PUT the webhook, and confirm the stored form, tag, and enabled state."""
        operation, client, empty = UPSERT_WEBHOOK_OPERATION_ID, self.client, Webhook()
        try:
            _validate_upsert_webhook_input(input)
        except ValueError as error:
            return client.mutation_attempt(_validation_outcome(operation, error), empty, "")
        credentials = await client.resolve_credentials(call, operation)
        if isinstance(credentials, _Outcome):
            return client.mutation_attempt(credentials, empty, "")
        secret = credentials.webhook_secret.reveal()
        if not secret:
            message = (
                "set webhook_secret on the Typeform connection before registering a webhook, so deliveries "
                "can be verified"
            )
            return client.mutation_attempt(_validation_outcome(operation, ValueError(message)), empty, "")
        payload = {
            "url": input.url,
            "enabled": not input.is_disabled,
            "secret": secret,
            "event_types": {WEBHOOK_EVENT_TYPE_FORM_RESPONSE: True},
        }
        path = "/forms/" + input.form_id + "/webhooks/" + quote(input.tag, safe="")
        branches = _FailureBranches(NOT_FOUND, PROVIDER_REJECTED, INVALID_RESPONSE)
        response = await client.exchange(credentials, operation, "PUT", path, branches, payload=payload)
        if isinstance(response, _Outcome):
            return client.mutation_attempt(response, empty, input.form_id)
        try:
            webhook = _convert_webhook(_decode_object(response.body))
        except _MalformedResponseError:
            webhook = None
        if (
            webhook is None
            or webhook.form_id != input.form_id
            or webhook.tag != input.tag
            or webhook.is_enabled == input.is_disabled
        ):
            return client.mutation_attempt(_malformed_outcome(operation, INVALID_RESPONSE), empty, input.form_id)
        return MutationAttempt.branch_result(UPSERTED, webhook, client.receipt(webhook.id))


def _list_forms_query(input: ListFormsInput) -> tuple[dict[str, str], int]:
    if not isinstance(input, ListFormsInput):
        raise ValueError("listForms input is invalid")
    if not isinstance(input.search, str) or not _is_bounded_utf8(input.search, _MAXIMUM_FORM_SEARCH_BYTES):
        raise ValueError(f"search must be valid UTF-8 of at most {_MAXIMUM_FORM_SEARCH_BYTES} bytes")
    if input.workspace_id and not _is_typeform_id(input.workspace_id):
        raise ValueError("workspaceId must be a Typeform workspace ID")
    if not _is_int(input.page) or input.page < 0 or input.page > _MAXIMUM_FORM_PAGE:
        raise ValueError(f"page must be from 1 through {_MAXIMUM_FORM_PAGE}, or 0 for the first page")
    if not _is_int(input.page_size) or input.page_size < 0 or input.page_size > _MAXIMUM_FORM_PAGE_SIZE:
        raise ValueError(
            f"pageSize must be from 1 through {_MAXIMUM_FORM_PAGE_SIZE}, or 0 for Typeform's default of 10"
        )
    page = max(input.page, 1)
    query = {"page": str(page)}
    if input.page_size > 0:
        query["page_size"] = str(input.page_size)
    if input.search:
        query["search"] = input.search
    if input.workspace_id:
        query["workspace_id"] = input.workspace_id
    return query, page


def _list_responses_query(input: ListResponsesInput) -> dict[str, str]:
    if not isinstance(input, ListResponsesInput) or not _is_typeform_id(input.form_id):
        raise ValueError(_FORM_ID_MESSAGE)
    if not _is_int(input.page_size) or input.page_size < 0 or input.page_size > _MAXIMUM_RESPONSE_PAGE_SIZE:
        raise ValueError(
            f"pageSize must be from 1 through {_MAXIMUM_RESPONSE_PAGE_SIZE}, or 0 for Typeform's default of "
            f"{_DEFAULT_RESPONSE_PAGE_SIZE}"
        )
    for name, value in (("since", input.since), ("until", input.until)):
        if not isinstance(value, datetime) or value.tzinfo is None:
            raise ValueError(f"{name} must be a timezone-aware timestamp")
    has_since, has_until = not _is_zero_time(input.since), not _is_zero_time(input.until)
    if has_since and has_until and input.since > input.until:
        raise ValueError("since must not be later than until")
    if input.before and input.after:
        raise ValueError("set before or after, not both")
    if input.before and not _is_typeform_id(input.before):
        raise ValueError("before must be the token of a Typeform response")
    if input.after and not _is_typeform_id(input.after):
        raise ValueError("after must be the token of a Typeform response")
    query: dict[str, str] = {}
    if input.page_size > 0:
        query["page_size"] = str(input.page_size)
    if has_since:
        query["since"] = _format_typeform_timestamp(input.since)
    if has_until:
        query["until"] = _format_typeform_timestamp(input.until)
    if input.before:
        query["before"] = input.before
    if input.after:
        query["after"] = input.after
    return query


def _validate_upsert_webhook_input(input: UpsertWebhookInput) -> None:
    if not isinstance(input, UpsertWebhookInput) or not _is_typeform_id(input.form_id):
        raise ValueError(_FORM_ID_MESSAGE)
    if not isinstance(input.tag, str) or _WEBHOOK_TAG_PATTERN.match(input.tag) is None:
        raise ValueError("tag must be 1 to 100 letters, digits, '.', '_', or '-', starting with a letter or digit")
    if not isinstance(input.is_disabled, bool) or not _is_webhook_url(input.url):
        raise ValueError(
            f"url must be an absolute HTTPS URL of at most {_MAXIMUM_WEBHOOK_URL_BYTES} bytes without user "
            "information or a fragment"
        )


def _is_webhook_url(value: object) -> bool:
    if not isinstance(value, str) or len(value.encode()) > _MAXIMUM_WEBHOOK_URL_BYTES:
        return False
    if any(ord(character) < 0x20 or ord(character) == 0x7F for character in value):
        return False
    if _INVALID_PERCENT_ESCAPE.search(value):
        return False
    try:
        parts = urlsplit(value)
        parts.port  # noqa: B018 - Go's url.Parse rejects an invalid port.
        hostname = parts.hostname
    except ValueError:
        return False
    return parts.scheme == "https" and bool(hostname) and "@" not in parts.netloc and parts.fragment == ""


def _convert_form_page(document: dict[str, Any], page: int) -> FormPage:
    total_items, page_count = _required_count(document, "total_items"), _required_count(document, "page_count")
    forms: list[FormSummary] = []
    for item in _array(document, "items"):
        item = _as_object(item)
        form_id = _string(item, "id")
        if not _is_typeform_id(form_id):
            _malformed()
        settings = _object(item, "settings")
        is_public = _optional_bool(settings, "is_public")
        forms.append(
            FormSummary(
                form_id,
                _string(item, "title"),
                is_public is None or is_public,
                _string(_object(item, "_links"), "display"),
                _optional_timestamp(_string(item, "created_at")),
                _optional_timestamp(_string(item, "last_updated_at")),
            )
        )
    next_page = page + 1 if page < page_count else 0
    return FormPage(tuple(forms), page, page_count, total_items, next_page)


def _convert_form(document: dict[str, Any]) -> Form:
    form_id = _string(document, "id")
    if not _is_typeform_id(form_id):
        _malformed()
    title, language = _string(document, "title"), _string(document, "language")
    hidden = tuple(_as_string(value) for value in _array(document, "hidden"))
    display_url = _string(_object(document, "_links"), "display")
    fields: list[FormField] = []
    _append_form_fields(fields, _array(document, "fields"), "", 1)
    return Form(form_id, title, language, tuple(fields), hidden, display_url)


def _append_form_fields(fields: list[FormField], source: list[Any], group_field_id: str, depth: int) -> None:
    if depth > _MAXIMUM_FIELD_NESTING_DEPTH:
        _malformed()
    for value in source:
        item = _as_object(value)
        field_id, field_type = _string(item, "id"), _string(item, "type")
        ref, title = _string(item, "ref"), _string(item, "title")
        properties, validations = _object(item, "properties"), _object(item, "validations")
        is_required = _optional_bool(validations, "required") or False
        nested = _array(properties, "fields")
        choices = tuple(
            FormFieldChoice(_string(choice, "id"), _string(choice, "ref"), _string(choice, "label"))
            for choice in (_as_object(entry) for entry in _array(properties, "choices"))
        )
        if not field_id or not field_type:
            _malformed()
        fields.append(FormField(field_id, ref, title, field_type, is_required, group_field_id, choices))
        _append_form_fields(fields, nested, field_id, depth + 1)


def _convert_response_page(document: dict[str, Any], input: ListResponsesInput) -> ResponsePage:
    total_items, page_count = _required_count(document, "total_items"), _required_count(document, "page_count")
    items = _array(document, "items")
    page_size = input.page_size or _DEFAULT_RESPONSE_PAGE_SIZE
    responses: list[FormResponse] = []
    for value in items:
        item = _as_object(value)
        response_id = _string(item, "response_id")
        responses.append(_convert_form_response(item, response_id))
    if len(items) > page_size:
        _malformed()
    next_before_token = ""
    if not input.after and len(responses) == page_size and page_count > 1:
        next_before_token = responses[-1].token
    return ResponsePage(input.form_id, tuple(responses), total_items, page_count, next_before_token)


def _convert_form_response(document: dict[str, Any], fallback_token: str = "") -> FormResponse:
    token = _string(document, "token") or fallback_token
    landed_at, submitted_at = _string(document, "landed_at"), _string(document, "submitted_at")
    raw_answers = _array(document, "answers")
    hidden = _object_or_none(document, "hidden")
    raw_variables = _array(document, "variables")
    calculated, ending = _object_or_none(document, "calculated"), _object_or_none(document, "ending")
    score = _optional_float(calculated, "score") if calculated is not None else None
    ending_ref = _string(ending, "ref") if ending is not None else ""
    answers = tuple(_convert_answer(_as_object(answer)) for answer in raw_answers)
    variables = []
    for value in raw_variables:
        variable = _as_object(value)
        variables.append(
            FormVariable(
                _string(variable, "key"),
                _string(variable, "type"),
                _string(variable, "text"),
                _optional_float(variable, "number"),
            )
        )
    if not _is_typeform_id(token):
        _malformed()
    response = FormResponse(token, _optional_timestamp(landed_at), _optional_timestamp(submitted_at), answers)
    hidden_fields = {name: _hidden_field_text(value) for name, value in (hidden or {}).items()}
    if any(not variable.key for variable in variables):
        _malformed()
    return FormResponse(
        response.token,
        response.landed_at,
        response.submitted_at,
        response.answers,
        hidden_fields,
        tuple(variables),
        score,
        ending_ref,
    )


def _convert_answer(document: dict[str, Any]) -> FormAnswer:
    answer_type = _string(document, "type")
    field_document = _object(document, "field")
    field_id, field_ref = _string(field_document, "id"), _string(field_document, "ref")
    field_type = _string(field_document, "type")
    strings = {
        name: _optional_string(document, name)
        for name in ("text", "email", "url", "file_url", "date", "phone_number")
    }
    number, boolean = _optional_float(document, "number"), _optional_bool(document, "boolean")
    choice_document = _object_or_none(document, "choice")
    choice = _answer_choice(choice_document) if choice_document is not None else None
    raw_choices = document.get("choices")
    payment, signature = _object_or_none(document, "payment"), _object_or_none(document, "signature")
    multi_format = _object_or_none(document, "multi_format")
    converted_payment = (
        FormAnswerPayment(_string(payment, "amount"), _optional_bool(payment, "success") or False)
        if payment is not None
        else None
    )
    converted_signature = (
        FormAnswerSignature(_string(signature, "url"), _string(signature, "type")) if signature is not None else None
    )
    converted_multi_format = (
        FormAnswerMultiFormat(
            _string(multi_format, "text"),
            _string(multi_format, "video_id"),
            _string(multi_format, "audio_id"),
            _string(multi_format, "transcript"),
        )
        if multi_format is not None
        else None
    )
    if raw_choices is not None and not isinstance(raw_choices, list | dict):
        _malformed()
    if not field_id or not field_type or not answer_type:
        _malformed()
    answer = FormAnswer(field_id, field_ref, field_type, "", answer_type)
    string_answer_fields = {
        ANSWER_TYPE_TEXT: ("text", "text"),
        ANSWER_TYPE_EMAIL: ("email", "email"),
        ANSWER_TYPE_URL: ("url", "url"),
        ANSWER_TYPE_FILE_URL: ("file_url", "file_url"),
        ANSWER_TYPE_DATE: ("date", "date"),
        ANSWER_TYPE_PHONE_NUMBER: ("phone_number", "phone_number"),
    }
    if answer_type in string_answer_fields:
        source, attribute = string_answer_fields[answer_type]
        text = strings[source]
        if text is None:
            _malformed()
        return _replace_answer(answer, **{attribute: text})
    if answer_type == ANSWER_TYPE_NUMBER:
        return _replace_answer(answer, number=_present(number))
    if answer_type == ANSWER_TYPE_BOOLEAN:
        return _replace_answer(answer, boolean=_present(boolean))
    if answer_type == ANSWER_TYPE_CHOICE:
        return _replace_answer(answer, choice=_present(choice))
    if answer_type == ANSWER_TYPE_CHOICES:
        return _replace_answer(answer, choices=_answer_choices(raw_choices))
    if answer_type == ANSWER_TYPE_PAYMENT:
        return _replace_answer(answer, payment=_present(converted_payment))
    if answer_type == ANSWER_TYPE_SIGNATURE:
        return _replace_answer(answer, signature=_present(converted_signature))
    if answer_type == ANSWER_TYPE_MULTI_FORMAT:
        return _replace_answer(answer, multi_format=_present(converted_multi_format))
    return answer


def _replace_answer(answer: FormAnswer, **changes: Any) -> FormAnswer:
    from dataclasses import replace

    return replace(answer, **changes)


def _answer_choice(document: dict[str, Any]) -> FormAnswerChoice:
    return FormAnswerChoice(
        _string(document, "id"), _string(document, "ref"), _string(document, "label"), _string(document, "other")
    )


def _answer_choices(value: Any) -> FormAnswerChoices:
    if value is None:
        _malformed()
    if isinstance(value, list):
        selected = [_answer_choice(_as_object(entry)) for entry in value]
        other = ""
        for choice in selected:
            if choice.other:
                other = choice.other
        return FormAnswerChoices(
            tuple(choice.id for choice in selected),
            tuple(choice.label for choice in selected),
            tuple(choice.ref for choice in selected),
            other,
        )
    document = _as_object(value)
    return FormAnswerChoices(
        tuple(_as_string(entry) for entry in _array(document, "ids")),
        tuple(_as_string(entry) for entry in _array(document, "labels")),
        tuple(_as_string(entry) for entry in _array(document, "refs")),
        _string(document, "other"),
    )


def _convert_webhook(document: dict[str, Any]) -> Webhook:
    webhook_id, form_id = _string(document, "id"), _string(document, "form_id")
    tag, url = _string(document, "tag"), _string(document, "url")
    is_enabled = _optional_bool(document, "enabled")
    event_types = _object_or_none(document, "event_types") or {}
    created_at, updated_at = _string(document, "created_at"), _string(document, "updated_at")
    subscribed: list[str] = []
    for event_type, is_subscribed in event_types.items():
        if is_subscribed is not None and not isinstance(is_subscribed, bool):
            _malformed()
        if is_subscribed:
            subscribed.append(event_type)
    if not webhook_id or not form_id or not tag or not url or is_enabled is None:
        _malformed()
    return Webhook(
        webhook_id,
        form_id,
        tag,
        url,
        is_enabled,
        tuple(sorted(subscribed)),
        _optional_timestamp(created_at),
        _optional_timestamp(updated_at),
    )


def _hidden_field_text(value: Any) -> str:
    """Return a string as itself, null as blank, and any other JSON value as compact JSON."""
    if isinstance(value, str):
        return value
    if value is None:
        return ""
    return json.dumps(value, separators=(",", ":"), ensure_ascii=False)


def _typeform_error_code(body: bytes) -> str:
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return ""
    if not isinstance(document, dict):
        return ""
    code = document.get("code")
    return code if isinstance(code, str) else ""


def _decode_object(body: bytes) -> dict[str, Any]:
    if not body:
        _malformed()
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError, RecursionError):
        _malformed()
    return _as_object(document)


def _malformed() -> NoReturn:
    raise _MalformedResponseError(_RESPONSE_MALFORMED)


def _present(value: Output | None) -> Output:
    if value is None:
        _malformed()
    return value


def _as_object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        _malformed()
    return value


def _as_string(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        _malformed()
    return value


def _string(document: dict[str, Any], key: str) -> str:
    return _as_string(document.get(key))


def _optional_string(document: dict[str, Any], key: str) -> str | None:
    value = document.get(key)
    return None if value is None else _as_string(value)


def _optional_bool(document: dict[str, Any], key: str) -> bool | None:
    value = document.get(key)
    if value is not None and not isinstance(value, bool):
        _malformed()
    return value


def _optional_float(document: dict[str, Any], key: str) -> float | None:
    value = document.get(key)
    if value is None:
        return None
    if isinstance(value, bool) or not isinstance(value, int | float):
        _malformed()
    return float(value)


def _required_count(document: dict[str, Any], key: str) -> int:
    value = document.get(key)
    if not _is_int(value) or value < 0 or value > 2**63 - 1:
        _malformed()
    return int(value)


def _object(document: dict[str, Any], key: str) -> dict[str, Any]:
    return _object_or_none(document, key) or {}


def _object_or_none(document: dict[str, Any], key: str) -> dict[str, Any] | None:
    value = document.get(key)
    return None if value is None else _as_object(value)


def _array(document: dict[str, Any], key: str) -> list[Any]:
    value = document.get(key)
    if value is None:
        return []
    if not isinstance(value, list):
        _malformed()
    return value


def _optional_timestamp(value: str) -> datetime:
    """Parse Go's RFC3339Nano layout as UTC; blank and year-one timestamps are zero."""
    if not value:
        return ZERO_TIME
    match = _RFC3339_PATTERN.match(value)
    if match is None:
        _malformed()
    year, month, day, hour, minute, second, fraction, zone = match.groups()
    microseconds = int((fraction[1:] + "000000")[:6]) if fraction else 0
    try:
        offset = timedelta(0)
        if zone not in ("Z", "z"):
            sign = -1 if zone[0] == "-" else 1
            hours, minutes = int(zone[1:3]), int(zone[4:6])
            if hours > 23 or minutes > 59:
                _malformed()
            offset = sign * timedelta(hours=hours, minutes=minutes)
        local = datetime(int(year), int(month), int(day), int(hour), int(minute), int(second), microseconds)
        parsed = (local - offset).replace(tzinfo=UTC) if local.year > 1 else local.replace(tzinfo=UTC) - offset
    except (ValueError, OverflowError):
        _malformed()
    if parsed.year <= 1:
        return ZERO_TIME
    return parsed


def _format_typeform_timestamp(value: datetime) -> str:
    return value.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S")


def _is_zero_time(value: datetime) -> bool:
    return value == ZERO_TIME


def _is_typeform_id(value: object) -> bool:
    return isinstance(value, str) and _TYPEFORM_ID_PATTERN.match(value) is not None


def _is_int(value: object) -> bool:
    return isinstance(value, int) and not isinstance(value, bool)


def _is_bounded_utf8(value: str, maximum_bytes: int) -> bool:
    try:
        return len(value.encode()) <= maximum_bytes
    except UnicodeEncodeError:
        return False


def _go_json(document: dict[str, Any]) -> bytes:
    """Encode like Go's json.Marshal: compact, with HTML-sensitive characters escaped."""
    text = json.dumps(document, separators=(",", ":"), ensure_ascii=False)
    for character, escape in (
        ("<", "\\u003c"),
        (">", "\\u003e"),
        ("&", "\\u0026"),
        (" ", "\\u2028"),
        (" ", "\\u2029"),
    ):
        text = text.replace(character, escape)
    return text.encode()


def _validation_outcome(operation: str, error: ValueError) -> _Outcome:
    return _Outcome(DEFECT, _failure(operation, FailureKind.VALIDATION, str(error)))


def _malformed_outcome(operation: str, branch: str) -> _Outcome:
    return _Outcome(branch, _failure(operation, FailureKind.PROTOCOL, _RESPONSE_MALFORMED), has_receipt=True)


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)
