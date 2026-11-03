# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typeform provider operations and Go-compatible attempt classification."""

import json
import re
from datetime import UTC, datetime, timedelta
from typing import Any, Literal, TypeVar, overload
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    QueryAttempt,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    parse_retry_after,
)

from ._generated import definitions as definitions
from .client import Client, Credentials
from .models import (
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

_ID = re.compile(r"^[A-Za-z0-9_-]{1,128}$")
_TAG = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$")
_TIMESTAMP = re.compile(r"^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$")
_ZERO = datetime.min.replace(tzinfo=UTC)
_HOST_ERROR = "Typeform could not be reached or its answer was lost"
_MALFORMED = "Typeform returned a malformed response"
OutputT = TypeVar("OutputT")


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "typeform", operation, message)


@overload
def _attempt(
    operation: str,
    mutation: Literal[False],
    branch: str,
    empty: OutputT,
    call: Call,
    kind: FailureKind,
    message: str,
    receipt: bool = False,
    object_id: str = "",
    retry_seconds: float = 0,
) -> QueryAttempt[OutputT]: ...


@overload
def _attempt(
    operation: str,
    mutation: Literal[True],
    branch: str,
    empty: OutputT,
    call: Call,
    kind: FailureKind,
    message: str,
    receipt: bool = False,
    object_id: str = "",
    retry_seconds: float = 0,
) -> MutationAttempt[OutputT]: ...


@overload
def _attempt(
    operation: str,
    mutation: bool,
    branch: str,
    empty: OutputT,
    call: Call,
    kind: FailureKind,
    message: str,
    receipt: bool = False,
    object_id: str = "",
    retry_seconds: float = 0,
) -> QueryAttempt[OutputT] | MutationAttempt[OutputT]: ...


def _attempt(
    operation: str,
    mutation: bool,
    branch: str,
    empty: Any,
    call: Call,
    kind: FailureKind,
    message: str,
    receipt: bool = False,
    object_id: str = "",
    retry_seconds: float = 0,
) -> Any:
    result = MutationAttempt if mutation else QueryAttempt
    failure = _failure(operation, kind, message)
    if branch == "retry":
        return result.retry(failure, retry_seconds)
    return result.branch_result(
        branch,
        empty,
        Receipt(call.id, "typeform", datetime.now(UTC), call.idempotency_key, object_id)
        if receipt
        else Receipt(),
        failure,
    )


def _timestamp(value: str | None) -> datetime:
    if not value:
        return _ZERO
    if not isinstance(value, str):
        raise ValueError(_MALFORMED)
    match = _TIMESTAMP.fullmatch(value)
    if match is None:
        raise ValueError(_MALFORMED)
    try:
        fraction = (match.group(3) or "")[:6].ljust(6, "0")
        parsed = datetime.fromisoformat(f"{match.group(1)}T{match.group(2)}").replace(
            microsecond=int(fraction or "0"), tzinfo=UTC
        )
        offset = match.group(4)
        if offset != "Z":
            hours, minutes = int(offset[1:3]), int(offset[4:6])
            if minutes > 59:
                raise ValueError
            distance = timedelta(hours=hours, minutes=minutes)
            parsed += -distance if offset[0] == "+" else distance
        return _ZERO if parsed.year <= 1 else parsed
    except ValueError as error:
        raise ValueError(_MALFORMED) from error


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError(_MALFORMED)
    return value


def _string(value: Any, default: str = "") -> str:
    if value is None:
        return default
    if not isinstance(value, str):
        raise ValueError(_MALFORMED)
    return value


def _integer(value: Any) -> int:
    if isinstance(value, bool) or not isinstance(value, int):
        raise ValueError(_MALFORMED)
    return value


def _boolean(value: Any) -> bool:
    if not isinstance(value, bool):
        raise ValueError(_MALFORMED)
    return value


def _optional_number(value: Any) -> float | int | None:
    if value is not None and (isinstance(value, bool) or not isinstance(value, (int, float))):
        raise ValueError(_MALFORMED)
    return value


def _string_list(value: Any) -> tuple[str, ...]:
    if not isinstance(value, list) or any(not isinstance(item, str) for item in value):
        raise ValueError(_MALFORMED)
    return tuple(value)


def _parse_body(body: bytes) -> dict[str, Any]:
    try:
        return _object(json.loads(body))
    except (ValueError, UnicodeDecodeError) as error:
        raise ValueError(_MALFORMED) from error


def _classify_status(status: int, headers: httpx.Headers, body: bytes) -> tuple[str, FailureKind, str, float]:
    if status == 429:
        delay = parse_retry_after(headers.get("Retry-After", ""), datetime.now(UTC))
        return (
            "retry",
            FailureKind.RATE_LIMIT,
            "Typeform rate limited the request; it allows two requests per second per account",
            delay,
        )
    if status == 408 or status >= 500:
        return "retry", FailureKind.AVAILABILITY, f"Typeform answered HTTP {status}", 0.0
    if status == 401:
        return (
            "providerRejected",
            FailureKind.AUTHENTICATION,
            "Typeform rejected the access token; generate a new personal access token",
            0.0,
        )
    if status == 403:
        return (
            "providerRejected",
            FailureKind.AUTHORIZATION,
            "Typeform denied the request; check that the token is valid and has the scopes "
            "in the connector guide",
            0.0,
        )
    if status == 402:
        return (
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            "the Typeform account's plan does not include a feature this request uses",
            0.0,
        )
    if status == 404:
        return "notFound", FailureKind.NOT_FOUND, "the Typeform form was not found", 0.0
    if status == 409:
        return "providerRejected", FailureKind.CONFLICT, "Typeform reported a conflicting change", 0.0
    try:
        code = _object(json.loads(body)).get("code")
    except (ValueError, UnicodeDecodeError):
        code = None
    if status == 400 and code == "webhook_url_https_required":
        return "providerRejected", FailureKind.VALIDATION, "Typeform requires an HTTPS webhook URL", 0.0
    return (
        "providerRejected",
        FailureKind.PROVIDER_REJECTION,
        f"Typeform rejected the request with HTTP {status}",
        0.0,
    )


class _Operation:
    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Typeform client is required")
        self.client = client

    @overload
    async def _exchange(
        self,
        call: Call,
        operation: str,
        mutation: Literal[False],
        empty: OutputT,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        body: dict[str, Any] | None = None,
        object_id: str = "",
        has_not_found: bool = True,
        credentials: Credentials | None = None,
    ) -> tuple[bytes | None, QueryAttempt[OutputT] | None]: ...

    @overload
    async def _exchange(
        self,
        call: Call,
        operation: str,
        mutation: Literal[True],
        empty: OutputT,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        body: dict[str, Any] | None = None,
        object_id: str = "",
        has_not_found: bool = True,
        credentials: Credentials | None = None,
    ) -> tuple[bytes | None, MutationAttempt[OutputT] | None]: ...

    async def _exchange(
        self,
        call: Call,
        operation: str,
        mutation: bool,
        empty: Any,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        body: dict[str, Any] | None = None,
        object_id: str = "",
        has_not_found: bool = True,
        credentials: Credentials | None = None,
    ) -> tuple[bytes | None, Any]:
        if credentials is None:
            try:
                credentials = await self.client.credentials.resolve(call)
            except Exception:
                return None, _attempt(
                    operation,
                    mutation,
                    "retry",
                    empty,
                    call,
                    FailureKind.AVAILABILITY,
                    "the Typeform connection could not be loaded yet",
                )
        token = credentials.access_token.reveal() if credentials.access_token is not None else ""
        if not is_header_safe_credential(token):
            return None, _attempt(
                operation,
                mutation,
                "defect",
                empty,
                call,
                FailureKind.AUTHENTICATION,
                "the Typeform access token is blank or contains characters a header cannot carry",
            )
        try:
            response, response_body = await self.client.request(token, method, path, query, body)
        except BodyTooLargeError:
            return None, _attempt(
                operation,
                mutation,
                "invalidResponse",
                empty,
                call,
                FailureKind.RESPONSE_TOO_LARGE,
                "Typeform response exceeds the configured maxResponseBytes",
                True,
                object_id,
            )
        except ProviderTransportError:
            return None, _attempt(
                operation, mutation, "retry", empty, call, FailureKind.TRANSPORT, _HOST_ERROR
            )
        except (ValueError, TypeError):
            return None, _attempt(
                operation,
                mutation,
                "defect",
                empty,
                call,
                FailureKind.LOCAL_DEFECT,
                "Typeform request could not be built",
            )
        if not 200 <= response.status_code < 300:
            branch, kind, message, delay = _classify_status(
                response.status_code, response.headers, response_body
            )
            if branch == "notFound" and not has_not_found:
                branch = "providerRejected"
            return None, _attempt(
                operation, mutation, branch, empty, call, kind, message, branch != "retry", object_id, delay
            )
        return response_body, None


def _form_summary(source: dict[str, Any]) -> FormSummary:
    source = _object(source)
    form_id = _string(source.get("id"))
    if not _ID.fullmatch(form_id):
        raise ValueError(_MALFORMED)
    settings = _object(source.get("settings") or {})
    links = _object(source.get("_links") or {})
    return FormSummary(
        id=form_id,
        title=_string(source.get("title")),
        is_public=_boolean(settings["is_public"]) if settings.get("is_public") is not None else True,
        display_url=_string(links.get("display")),
        created_at=_timestamp(source.get("created_at")),
        last_updated_at=_timestamp(source.get("last_updated_at")),
    )


def _form_fields(sources: list[Any], parent: str = "", depth: int = 1) -> tuple[FormField, ...]:
    if depth > 4 or not isinstance(sources, list):
        raise ValueError(_MALFORMED)
    fields = []
    for source in sources:
        source = _object(source)
        field_id, kind = _string(source.get("id")), _string(source.get("type"))
        if not field_id or not kind:
            raise ValueError(_MALFORMED)
        properties = _object(source.get("properties") or {})
        validations = _object(source.get("validations") or {})
        choices = tuple(
            FormFieldChoice(
                id=_string(_object(choice).get("id")),
                ref=_string(choice.get("ref")),
                label=_string(choice.get("label")),
            )
            for choice in properties.get("choices") or []
        )
        fields.append(
            FormField(
                id=field_id,
                ref=_string(source.get("ref")),
                title=_string(source.get("title")),
                type=kind,
                is_required=_boolean(validations.get("required") or False),
                group_field_id=parent,
                choices=choices,
            )
        )
        fields.extend(_form_fields(properties.get("fields") or [], field_id, depth + 1))
    return tuple(fields)


def _form(source: dict[str, Any]) -> Form:
    source = _object(source)
    form_id = _string(source.get("id"))
    if not _ID.fullmatch(form_id):
        raise ValueError(_MALFORMED)
    hidden = source.get("hidden") or []
    if not isinstance(hidden, list) or any(not isinstance(item, str) for item in hidden):
        raise ValueError(_MALFORMED)
    return Form(
        id=form_id,
        title=_string(source.get("title")),
        language=_string(source.get("language")),
        fields=_form_fields(source.get("fields") or []),
        hidden_fields=tuple(hidden),
        display_url=_string(_object(source.get("_links") or {}).get("display")),
    )


def _response(source: dict[str, Any]) -> FormResponse:
    source = _object(source)
    token = _string(source.get("token") or source.get("response_id"))
    if not _ID.fullmatch(token):
        raise ValueError(_MALFORMED)
    answers = []
    for item in source.get("answers") or []:
        item = _object(item)
        field = _object(item.get("field") or {})
        answer_type = _string(item.get("type"))
        field_id, field_type = _string(field.get("id")), _string(field.get("type"))
        if not field_id or not field_type or not answer_type:
            raise ValueError(_MALFORMED)
        values: dict[str, Any] = {
            "field_id": field_id,
            "field_ref": _string(field.get("ref")),
            "field_type": field_type,
            "type": answer_type,
        }
        if answer_type in ("text", "email", "url", "file_url", "date", "phone_number"):
            if not isinstance(item.get(answer_type), str):
                raise ValueError(_MALFORMED)
            values[answer_type] = item[answer_type]
        elif answer_type in ("number", "boolean"):
            value = item.get(answer_type)
            if (
                answer_type == "number" and (isinstance(value, bool) or not isinstance(value, (int, float)))
            ) or (answer_type == "boolean" and not isinstance(value, bool)):
                raise ValueError(_MALFORMED)
            values[answer_type] = value
        elif answer_type == "choice":
            choice = _object(item.get("choice"))
            values["choice"] = FormAnswerChoice(
                id=_string(choice.get("id")),
                ref=_string(choice.get("ref")),
                label=_string(choice.get("label")),
                other=_string(choice.get("other")),
            )
        elif answer_type == "choices":
            choices = item.get("choices")
            if isinstance(choices, list):
                choices = [_object(choice) for choice in choices]
                values["choices"] = FormAnswerChoices(
                    ids=tuple(_string(choice.get("id")) for choice in choices),
                    refs=tuple(_string(choice.get("ref")) for choice in choices),
                    labels=tuple(_string(choice.get("label")) for choice in choices),
                    other=next(
                        (_string(choice.get("other")) for choice in reversed(choices) if choice.get("other")),
                        "",
                    ),
                )
            else:
                choices = _object(choices)
                values["choices"] = FormAnswerChoices(
                    ids=_string_list(choices.get("ids") or []),
                    labels=_string_list(choices.get("labels") or []),
                    refs=_string_list(choices.get("refs") or []),
                    other=_string(choices.get("other")),
                )
        elif answer_type == "payment":
            payment = _object(item.get("payment"))
            values["payment"] = FormAnswerPayment(
                amount=_string(payment.get("amount")), is_successful=_boolean(payment.get("success", False))
            )
        elif answer_type == "signature":
            signature = _object(item.get("signature"))
            values["signature"] = FormAnswerSignature(
                url=_string(signature.get("url")), method=_string(signature.get("type"))
            )
        elif answer_type == "multi_format":
            multimedia = _object(item.get("multi_format"))
            values["multi_format"] = FormAnswerMultiFormat(
                text=_string(multimedia.get("text")),
                video_id=_string(multimedia.get("video_id")),
                audio_id=_string(multimedia.get("audio_id")),
                transcript=_string(multimedia.get("transcript")),
            )
        answers.append(FormAnswer(**values))
    hidden_source = _object(source.get("hidden") or {})
    hidden = {
        name: value
        if isinstance(value, str)
        else ""
        if value is None
        else json.dumps(value, separators=(",", ":"), ensure_ascii=False)
        for name, value in hidden_source.items()
    }
    variables = []
    for variable in source.get("variables") or []:
        variable = _object(variable)
        if not _string(variable.get("key")):
            raise ValueError(_MALFORMED)
        variables.append(
            FormVariable(
                key=variable["key"],
                type=_string(variable.get("type")),
                text=_string(variable.get("text")),
                number=_optional_number(variable.get("number")),
            )
        )
    calculated = source.get("calculated")
    score = _optional_number(_object(calculated).get("score")) if calculated is not None else None
    ending = source.get("ending")
    ending_ref = _string(_object(ending).get("ref")) if ending is not None else ""
    return FormResponse(
        token=token,
        landed_at=_timestamp(source.get("landed_at")),
        submitted_at=_timestamp(source.get("submitted_at")),
        answers=tuple(answers),
        hidden_fields=hidden,
        variables=tuple(variables),
        score=score,
        ending_ref=ending_ref,
    )


class ListFormsOperation(_Operation):
    """Read one page of Typeform forms."""

    def definition(self) -> Any:
        """Return the generated listForms definition."""
        return definitions.LIST_FORMS_DEFINITION

    async def invoke(self, call: Call, input: ListFormsInput) -> QueryAttempt[FormPage]:
        """Invoke listForms once for the Dex Execute attempt."""
        operation, empty = "listForms", FormPage()
        if (
            not isinstance(input.search, str)
            or len(input.search.encode()) > 256
            or not isinstance(input.workspace_id, str)
            or (input.workspace_id and not _ID.fullmatch(input.workspace_id))
            or not isinstance(input.page, int)
            or input.page < 0
            or input.page > 10000
            or not isinstance(input.page_size, int)
            or input.page_size < 0
            or input.page_size > 200
        ):
            return _attempt(
                operation, False, "defect", empty, call, FailureKind.VALIDATION, "invalid listForms input"
            )
        page = max(input.page, 1)
        query = {"page": str(page)}
        if input.page_size:
            query["page_size"] = str(input.page_size)
        if input.search:
            query["search"] = input.search
        if input.workspace_id:
            query["workspace_id"] = input.workspace_id
        body, result = await self._exchange(
            call, operation, False, empty, "GET", "/forms", query, has_not_found=False
        )
        if result is not None:
            return result
        try:
            assert body is not None
            data = _parse_body(body)
            total, count = _integer(data.get("total_items")), _integer(data.get("page_count"))
            if total < 0 or count < 0:
                raise ValueError(_MALFORMED)
            forms = tuple(_form_summary(item) for item in data.get("items") or [])
            page_value = FormPage(
                forms=forms,
                page=page,
                page_count=count,
                total_items=total,
                next_page=page + 1 if page < count else 0,
            )
        except (ValueError, TypeError, AttributeError):
            return _attempt(
                operation, False, "invalidResponse", empty, call, FailureKind.PROTOCOL, _MALFORMED, True
            )
        return QueryAttempt.branch_result("listed", page_value, self.client.receipt(call))


class GetFormOperation(_Operation):
    """Read one Typeform form definition."""

    def definition(self) -> Any:
        """Return the generated getForm definition."""
        return definitions.GET_FORM_DEFINITION

    async def invoke(self, call: Call, input: GetFormInput) -> QueryAttempt[Form]:
        """Invoke getForm once for the Dex Execute attempt."""
        operation, empty = "getForm", Form()
        if not isinstance(input.form_id, str) or not _ID.fullmatch(input.form_id):
            return _attempt(
                operation,
                False,
                "defect",
                empty,
                call,
                FailureKind.VALIDATION,
                "formId must be a Typeform form ID such as u6nXL7, the part after /to/ in the form link",
            )
        body, result = await self._exchange(
            call, operation, False, empty, "GET", "/forms/" + input.form_id, object_id=input.form_id
        )
        if result is not None:
            return result
        try:
            assert body is not None
            form = _form(_parse_body(body))
            if form.id != input.form_id:
                raise ValueError(_MALFORMED)
        except (ValueError, TypeError, AttributeError):
            return _attempt(
                operation,
                False,
                "invalidResponse",
                empty,
                call,
                FailureKind.PROTOCOL,
                _MALFORMED,
                True,
                input.form_id,
            )
        return QueryAttempt.branch_result("found", form, self.client.receipt(call, form.id))


class ListResponsesOperation(_Operation):
    """Read one page of completed responses."""

    def definition(self) -> Any:
        """Return the generated listResponses definition."""
        return definitions.LIST_RESPONSES_DEFINITION

    async def invoke(self, call: Call, input: ListResponsesInput) -> QueryAttempt[ResponsePage]:
        """Invoke listResponses once for the Dex Execute attempt."""
        operation, empty = "listResponses", ResponsePage()
        if (
            not isinstance(input.form_id, str)
            or not _ID.fullmatch(input.form_id)
            or input.page_size < 0
            or input.page_size > 1000
            or (input.before and input.after)
            or (input.before and not _ID.fullmatch(input.before))
            or (input.after and not _ID.fullmatch(input.after))
            or (input.since != _ZERO and input.until != _ZERO and input.since > input.until)
        ):
            return _attempt(
                operation, False, "defect", empty, call, FailureKind.VALIDATION, "invalid listResponses input"
            )
        query = {}
        if input.page_size:
            query["page_size"] = str(input.page_size)
        if input.since != _ZERO:
            query["since"] = input.since.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S")
        if input.until != _ZERO:
            query["until"] = input.until.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S")
        if input.before:
            query["before"] = input.before
        if input.after:
            query["after"] = input.after
        body, result = await self._exchange(
            call,
            operation,
            False,
            empty,
            "GET",
            "/forms/" + input.form_id + "/responses",
            query,
            object_id=input.form_id,
        )
        if result is not None:
            return result
        try:
            assert body is not None
            data = _parse_body(body)
            total, count = _integer(data.get("total_items")), _integer(data.get("page_count"))
            if total < 0 or count < 0:
                raise ValueError(_MALFORMED)
            items = data.get("items") or []
            if not isinstance(items, list) or len(items) > (input.page_size or 25):
                raise ValueError(_MALFORMED)
            responses = tuple(_response(item) for item in items)
            next_token = (
                responses[-1].token
                if not input.after and len(responses) == (input.page_size or 25) and count > 1
                else ""
            )
            page = ResponsePage(
                form_id=input.form_id,
                responses=responses,
                total_items=total,
                page_count=count,
                next_before_token=next_token,
            )
        except (ValueError, TypeError, AttributeError, KeyError):
            return _attempt(
                operation,
                False,
                "invalidResponse",
                empty,
                call,
                FailureKind.PROTOCOL,
                _MALFORMED,
                True,
                input.form_id,
            )
        return QueryAttempt.branch_result("listed", page, self.client.receipt(call, input.form_id))


class UpsertWebhookOperation(_Operation):
    """Replace one form webhook by tag."""

    def definition(self) -> Any:
        """Return the generated upsertWebhook definition."""
        return definitions.UPSERT_WEBHOOK_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpsertWebhookInput) -> IdempotencyKey:
        """Use the Call ID because Typeform PUT by tag is absolute replacement."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpsertWebhookInput) -> MutationAttempt[Webhook]:
        """Invoke upsertWebhook once for the Dex Execute attempt."""
        operation, empty = "upsertWebhook", Webhook()
        try:
            parsed = urlsplit(input.url)
        except ValueError:
            parsed = None
        if (
            not _ID.fullmatch(input.form_id)
            or not _TAG.fullmatch(input.tag)
            or len(input.url.encode()) > 2048
            or any(ord(character) < 33 or ord(character) > 126 for character in input.url)
            or parsed is None
            or parsed.scheme != "https"
            or not parsed.hostname
            or parsed.username is not None
            or parsed.password is not None
            or parsed.fragment
        ):
            return _attempt(
                operation, True, "defect", empty, call, FailureKind.VALIDATION, "invalid upsertWebhook input"
            )
        try:
            credentials = await self.client.credentials.resolve(call)
        except Exception:
            return _attempt(
                operation,
                True,
                "retry",
                empty,
                call,
                FailureKind.AVAILABILITY,
                "the Typeform connection could not be loaded yet",
            )
        secret = credentials.webhook_secret.reveal() if credentials.webhook_secret is not None else ""
        if not secret:
            return _attempt(
                operation,
                True,
                "defect",
                empty,
                call,
                FailureKind.VALIDATION,
                "set webhook_secret on the Typeform connection before registering a webhook, "
                "so deliveries can be verified",
            )
        request_body = {
            "url": input.url,
            "enabled": not input.disabled,
            "secret": secret,
            "event_types": {"form_response": True},
        }
        body, result = await self._exchange(
            call,
            operation,
            True,
            empty,
            "PUT",
            "/forms/" + input.form_id + "/webhooks/" + input.tag,
            body=request_body,
            object_id=input.form_id,
            credentials=credentials,
        )
        if result is not None:
            return result
        try:
            assert body is not None
            data = _parse_body(body)
            webhook_id = _string(data.get("id"))
            form_id, tag, url = (
                _string(data.get("form_id")),
                _string(data.get("tag")),
                _string(data.get("url")),
            )
            enabled = _boolean(data.get("enabled"))
            if (
                not webhook_id
                or form_id != input.form_id
                or tag != input.tag
                or not url
                or enabled == input.disabled
            ):
                raise ValueError(_MALFORMED)
            events = _object(data.get("event_types") or {})
            webhook = Webhook(
                id=webhook_id,
                form_id=form_id,
                tag=tag,
                url=url,
                is_enabled=enabled,
                event_types=tuple(sorted(key for key, active in events.items() if _boolean(active))),
                created_at=_timestamp(data.get("created_at")),
                updated_at=_timestamp(data.get("updated_at")),
            )
        except (ValueError, TypeError, AttributeError):
            return _attempt(
                operation,
                True,
                "invalidResponse",
                empty,
                call,
                FailureKind.PROTOCOL,
                _MALFORMED,
                True,
                input.form_id,
            )
        return MutationAttempt.branch_result("upserted", webhook, self.client.receipt(call, webhook.id))
