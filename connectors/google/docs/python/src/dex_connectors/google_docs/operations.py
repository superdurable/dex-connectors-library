# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Docs operation invocation and provider outcome classification."""

import json
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import (
    Call,
    CallID,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    QueryAttempt,
    Receipt,
)
from dex_connectors.sdk.providerhttp import parse_retry_after

from ._generated.definitions import (
    APPEND_TEXT_DEFINITION,
    CREATE_DOCUMENT_DEFINITION,
    GET_DOCUMENT_TEXT_DEFINITION,
    REPLACE_DOCUMENT_TEXT_DEFINITION,
)
from .client import DRIVE_ID, Client, Exchange, failure, google_error_tokens, retryable, status_kind
from .document import (
    body_text,
    decode_document,
    first_content,
    render_document,
    utf16_length,
    validate_write_text,
)
from .json_values import strict_json_loads
from .models import (
    AppendTextInput,
    AppendTextOutput,
    CreateDocumentInput,
    CreateDocumentOutput,
    GetDocumentTextInput,
    GetDocumentTextOutput,
    PlaceholderReplacementResult,
    ReplaceDocumentTextInput,
    ReplaceDocumentTextOutput,
)


def _read_result(
    client: Client,
    call: Call,
    operation: str,
    exchange: Exchange,
    object_id: str,
    *,
    too_large_branch: str = "tooLarge",
    not_found_branch: str = "notFound",
) -> tuple[str, Any, Receipt | None, float]:
    receipt = client.receipt(call, exchange, object_id)
    if exchange.error == "notBuilt":
        return (
            "defect",
            failure(operation, FailureKind.LOCAL_DEFECT, "provider request could not be built"),
            Receipt(),
            0,
        )
    if exchange.error == "tooLarge":
        return (
            too_large_branch,
            failure(
                operation, FailureKind.RESPONSE_TOO_LARGE, "provider response exceeds the configured limit"
            ),
            receipt,
            0,
        )
    if exchange.error:
        return "retry", failure(operation, FailureKind.TRANSPORT, "provider is unavailable"), None, 0
    if 200 <= exchange.status < 300:
        return "success", None, receipt, 0
    tokens = google_error_tokens(exchange.body)
    if retryable(exchange.status, tokens):
        delay = parse_retry_after(exchange.headers.get("Retry-After", ""), datetime.now(UTC))
        return (
            "retry",
            failure(
                operation, status_kind(exchange.status, tokens), "provider temporarily rejected the request"
            ),
            None,
            delay,
        )
    if exchange.status == 404:
        return (
            not_found_branch,
            failure(
                operation, FailureKind.NOT_FOUND, "document was not found or is not visible to the connection"
            ),
            receipt,
            0,
        )
    return (
        "providerRejected",
        failure(operation, status_kind(exchange.status, tokens), "provider rejected the request"),
        receipt,
        0,
    )


class GetDocumentTextOperation:
    """Read and render the first tab of one Google Doc."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the immutable query definition."""
        return GET_DOCUMENT_TEXT_DEFINITION

    async def invoke(self, call: Call, input: GetDocumentTextInput) -> QueryAttempt[GetDocumentTextOutput]:
        """Render a bounded document and classify a definite read outcome."""
        format_name = input.format or "markdown"
        empty = GetDocumentTextOutput(format="")
        if not DRIVE_ID.fullmatch(input.document_id) or format_name not in ("markdown", "plainText"):
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    "getDocumentText",
                    FailureKind.VALIDATION,
                    "documentId must be a Google Docs document ID and format must be markdown or plainText",
                ),
            )
        credential = await self.client.credential(call)
        if credential is None:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    "getDocumentText",
                    FailureKind.AUTHENTICATION,
                    "connection credentials are unavailable",
                ),
            )
        exchange = await self.client.exchange(
            call, credential, "GET", self.client.document_url(input.document_id)
        )
        branch, problem, receipt, delay = _read_result(self.client, call, "getDocumentText", exchange, "")
        if branch == "retry":
            return QueryAttempt.retry(problem, delay)
        if branch != "success":
            return QueryAttempt.branch_result(branch, empty, receipt or Receipt(), problem)
        receipt = self.client.receipt(call, exchange, input.document_id)
        try:
            document = decode_document(exchange.body)
        except ValueError:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    "getDocumentText",
                    FailureKind.PROTOCOL,
                    "provider returned an invalid document",
                ),
            )
        output = GetDocumentTextOutput(
            document_id=document["documentId"],
            title=document.get("title", ""),
            revision_id=document.get("revisionId", ""),
            format=format_name,
            has_other_tabs=len(document["tabs"]) > 1 or bool(document["tabs"][0].get("childTabs")),
        )
        try:
            text = render_document(document, format_name)
        except (TypeError, KeyError, ValueError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure(
                    "getDocumentText",
                    FailureKind.PROTOCOL,
                    "document structure could not be rendered",
                ),
            )
        if len(text.encode()) > self.client.max_text_bytes:
            return QueryAttempt.branch_result(
                "tooLarge",
                output,
                receipt,
                failure(
                    "getDocumentText",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "document text exceeds the configured limit",
                ),
            )
        return QueryAttempt.branch_result(
            "read",
            GetDocumentTextOutput(
                document_id=output.document_id,
                title=output.title,
                revision_id=output.revision_id,
                format=format_name,
                text=text,
                byte_count=len(text.encode()),
                has_other_tabs=output.has_other_tabs,
            ),
            receipt,
        )


def _validate_target(document_id: str, required_revision_id: str) -> None:
    if not DRIVE_ID.fullmatch(document_id):
        raise ValueError("documentId must be a Google Docs document ID")
    if (
        not required_revision_id
        or len(required_revision_id.encode()) > 1024
        or any(not character.isprintable() or character.isspace() for character in required_revision_id)
    ):
        raise ValueError("requiredRevisionId must be the revisionId a getDocumentText Step returned")


def _validate_replace(input: ReplaceDocumentTextInput, max_text_bytes: int) -> None:
    _validate_target(input.document_id, input.required_revision_id)
    if input.target == "wholeBody":
        if input.placeholders:
            raise ValueError("wholeBody takes text, not placeholders")
        if len(input.text.encode()) > max_text_bytes:
            raise ValueError("text exceeds the configured text limit")
        validate_write_text("text", input.text)
    elif input.target == "placeholders":
        if input.text:
            raise ValueError("placeholders takes placeholders, not text")
        if not 1 <= len(input.placeholders) <= 50:
            raise ValueError("placeholders must list 1 to 50 placeholders")
        if sum(len(item.text.encode()) for item in input.placeholders) > max_text_bytes:
            raise ValueError("placeholder replacements exceed the configured text limit")
        for index, item in enumerate(input.placeholders):
            name = item.placeholder
            if not name.strip() or len(name.encode()) > 256 or "\n" in name or "\v" in name:
                raise ValueError(f"placeholders[{index}].placeholder must be 1 to 256 bytes on one line")
            validate_write_text(f"placeholders[{index}].placeholder", name)
            validate_write_text(f"placeholders[{index}].text", item.text)
            if any(other.placeholder != name and name in other.placeholder for other in input.placeholders):
                raise ValueError(
                    f"placeholders[{index}].placeholder repeats or is part of another placeholder"
                )
            if any(other.placeholder in item.text for other in input.placeholders):
                raise ValueError(f"placeholders[{index}].text cannot contain a requested placeholder")
    else:
        raise ValueError("target must be wholeBody or placeholders")


def _validate_append(input: AppendTextInput, max_text_bytes: int) -> None:
    _validate_target(input.document_id, input.required_revision_id)
    if not input.text.strip():
        raise ValueError("text must not be blank")
    if len(input.text.encode()) > max_text_bytes:
        raise ValueError("text exceeds the configured text limit")
    validate_write_text("text", input.text)


def _normal_style(tab_id: str, start: int, end: int) -> list[dict[str, Any]]:
    range_value = {"startIndex": start, "endIndex": end, "tabId": tab_id}
    return [
        {"deleteParagraphBullets": {"range": range_value}},
        {
            "updateParagraphStyle": {
                "range": range_value,
                "paragraphStyle": {"namedStyleType": "NORMAL_TEXT"},
                "fields": "*",
            }
        },
        {"updateTextStyle": {"range": range_value, "textStyle": {}, "fields": "*"}},
    ]


class _GuardedWrite:
    """Share revision guard, read-back, and response classification between text writes."""

    def __init__(self, client: Client, operation: str) -> None:
        self.client, self.operation = client, operation

    async def invoke(
        self, call: Call, input: ReplaceDocumentTextInput | AppendTextInput
    ) -> MutationAttempt[Any]:
        """Read the revision, send one guarded batch, and recover a lost reply on retry."""
        zero: Any = (
            ReplaceDocumentTextOutput() if self.operation == "replaceDocumentText" else AppendTextOutput()
        )
        try:
            if isinstance(input, ReplaceDocumentTextInput):
                _validate_replace(input, self.client.max_text_bytes)
            else:
                _validate_append(input, self.client.max_text_bytes)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", zero, Receipt(), failure(self.operation, FailureKind.VALIDATION, str(error))
            )
        credential = await self.client.credential(call)
        if credential is None:
            return MutationAttempt.branch_result(
                "defect",
                zero,
                Receipt(),
                failure(self.operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        return await self._read_and_write(call, input, credential)

    async def _read_and_write(self, call: Call, input: Any, credential: Any) -> MutationAttempt[Any]:
        zero: Any = (
            ReplaceDocumentTextOutput() if self.operation == "replaceDocumentText" else AppendTextOutput()
        )
        exchange = await self.client.exchange(
            call, credential, "GET", self.client.document_url(input.document_id, True)
        )
        branch, problem, receipt, delay = _read_result(
            self.client, call, self.operation, exchange, "", too_large_branch="invalidResponse"
        )
        if branch == "retry":
            return MutationAttempt.retry(problem, delay)
        if branch != "success":
            return MutationAttempt.branch_result(branch, zero, receipt or Receipt(), problem)
        receipt = self.client.receipt(call, exchange, input.document_id)
        try:
            document = decode_document(exchange.body)
        except ValueError:
            return MutationAttempt.branch_result(
                "invalidResponse",
                zero,
                receipt,
                failure(self.operation, FailureKind.PROTOCOL, "provider returned an invalid document"),
            )
        revision = document.get("revisionId", "")
        if not revision:
            return MutationAttempt.branch_result(
                "providerRejected",
                zero,
                receipt,
                failure(self.operation, FailureKind.AUTHORIZATION, "connection cannot edit the document"),
            )
        try:
            current_text = body_text(document)
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                zero,
                receipt,
                failure(self.operation, FailureKind.PROTOCOL, "document structure could not be read"),
            )
        if revision != input.required_revision_id:
            return self._changed(input, revision, current_text, receipt)
        requests, early = self._requests(input, document, current_text)
        if early is not None:
            return early
        if not requests:
            return MutationAttempt.branch_result(
                self._applied(), self._output(input, revision, [], True), receipt
            )
        body = json.dumps(
            {"requests": requests, "writeControl": {"requiredRevisionId": revision}}, separators=(",", ":")
        ).encode()
        update = await self.client.exchange(
            call,
            credential,
            "POST",
            self.client.batch_url(input.document_id),
            body,
            "application/json; charset=UTF-8",
        )
        return await self._update_result(call, input, credential, update)

    def _applied(self) -> str:
        return "replaced" if self.operation == "replaceDocumentText" else "appended"

    def _output(self, input: Any, revision: str, replies: list[Any], already: bool) -> Any:
        if self.operation == "appendText":
            return AppendTextOutput(input.document_id, revision, already)
        replacements: tuple[PlaceholderReplacementResult, ...] = ()
        if input.target == "placeholders" and replies:
            replacements = tuple(
                PlaceholderReplacementResult(
                    item.placeholder,
                    replies[index].get("replaceAllText", {}).get("occurrencesChanged", 0)
                    if index < len(replies)
                    else 0,
                )
                for index, item in enumerate(input.placeholders)
            )
        return ReplaceDocumentTextOutput(input.document_id, revision, replacements, (), already)

    def _changed(self, input: Any, revision: str, current: str, receipt: Receipt) -> MutationAttempt[Any]:
        if isinstance(input, AppendTextInput):
            held = current == input.text + "\n" or current.endswith("\n" + input.text + "\n")
        elif input.target == "wholeBody":
            held = current == input.text + "\n"
        else:
            held = all(
                item.placeholder not in current and item.text in current for item in input.placeholders
            )
        if held:
            return MutationAttempt.branch_result(
                self._applied(), self._output(input, revision, [], True), receipt
            )
        return MutationAttempt.branch_result(
            "revisionChanged",
            self._output(input, revision, [], False),
            receipt,
            failure(
                self.operation,
                FailureKind.CONFLICT,
                "document changed after the required revision; nothing was written",
            ),
        )

    def _requests(
        self, input: Any, document: dict[str, Any], current: str
    ) -> tuple[list[dict[str, Any]], MutationAttempt[Any] | None]:
        tab_id = document["tabs"][0]["tabProperties"]["tabId"]
        if isinstance(input, AppendTextInput):
            separator = "" if current == "\n" else "\n"
            index = first_content(document)[-1]["endIndex"] - 1
            start = index + utf16_length(separator)
            requests = [
                {
                    "insertText": {
                        "text": separator + input.text,
                        "location": {"index": index, "tabId": tab_id},
                    }
                }
            ]
            requests += _normal_style(tab_id, start, start + utf16_length(input.text))
            return requests, None
        if input.target == "placeholders":
            missing = tuple(
                item.placeholder for item in input.placeholders if item.placeholder not in current
            )
            if missing:
                output = ReplaceDocumentTextOutput(input.document_id, document["revisionId"], (), missing)
                return [], MutationAttempt.branch_result(
                    "placeholderNotFound",
                    output,
                    Receipt(),
                    failure(
                        self.operation,
                        FailureKind.VALIDATION,
                        "a placeholder does not occur in the document body; nothing was written",
                    ),
                )
            return [
                {
                    "replaceAllText": {
                        "replaceText": item.text,
                        "containsText": {"text": item.placeholder, "matchCase": True},
                        "tabsCriteria": {"tabIds": [tab_id]},
                    }
                }
                for item in input.placeholders
            ], None
        content = first_content(document)
        start = content[0]["endIndex"] if "sectionBreak" in content[0] else content[0].get("startIndex", 0)
        end = content[-1]["endIndex"] - 1
        requests = []
        if end > start:
            requests.append(
                {"deleteContentRange": {"range": {"startIndex": start, "endIndex": end, "tabId": tab_id}}}
            )
        if input.text:
            requests.append(
                {"insertText": {"text": input.text, "location": {"index": start, "tabId": tab_id}}}
            )
            requests += _normal_style(tab_id, start, start + utf16_length(input.text))
        return requests, None

    async def _update_result(
        self, call: Call, input: Any, credential: Any, exchange: Exchange
    ) -> MutationAttempt[Any]:
        zero: Any = (
            ReplaceDocumentTextOutput() if self.operation == "replaceDocumentText" else AppendTextOutput()
        )
        receipt = self.client.receipt(call, exchange, input.document_id)
        if exchange.error == "notBuilt":
            return MutationAttempt.branch_result(
                "defect",
                zero,
                receipt,
                failure(self.operation, FailureKind.LOCAL_DEFECT, "provider request could not be built"),
            )
        if exchange.error:
            return MutationAttempt.retry(
                failure(
                    self.operation,
                    FailureKind.TRANSPORT,
                    "provider update outcome is unknown; the next attempt reads the revision",
                )
            )
        tokens = google_error_tokens(exchange.body)
        if 200 <= exchange.status < 300:
            try:
                value = strict_json_loads(exchange.body)
                if not isinstance(value, dict):
                    raise ValueError("invalid update")
                revision = value.get("writeControl", {}).get("requiredRevisionId", "")
                replies = value.get("replies", [])
                if not isinstance(revision, str) or not isinstance(replies, list):
                    raise ValueError("invalid update")
            except (ValueError, TypeError, AttributeError):
                return MutationAttempt.retry(
                    failure(
                        self.operation,
                        FailureKind.PROTOCOL,
                        "provider returned an invalid update response; the next attempt reads the revision",
                    )
                )
            return MutationAttempt.branch_result(
                self._applied(), self._output(input, revision, replies, False), receipt
            )
        if retryable(exchange.status, tokens):
            return MutationAttempt.retry(
                failure(
                    self.operation,
                    status_kind(exchange.status, tokens),
                    "provider temporarily rejected the update",
                ),
                parse_retry_after(exchange.headers.get("Retry-After", ""), datetime.now(UTC)),
            )
        if exchange.status == 400:
            reread = await self.client.exchange(
                call, credential, "GET", self.client.document_url(input.document_id, True)
            )
            branch, problem, reread_receipt, delay = _read_result(
                self.client, call, self.operation, reread, "", too_large_branch="invalidResponse"
            )
            if branch == "retry":
                return MutationAttempt.retry(problem, delay)
            if branch != "success":
                return MutationAttempt.branch_result(branch, zero, reread_receipt or Receipt(), problem)
            try:
                document = decode_document(reread.body)
                current = body_text(document)
            except ValueError:
                return MutationAttempt.branch_result(
                    "invalidResponse",
                    zero,
                    self.client.receipt(call, reread, input.document_id),
                    failure(self.operation, FailureKind.PROTOCOL, "provider returned an invalid document"),
                )
            if document["revisionId"] == input.required_revision_id:
                return MutationAttempt.branch_result(
                    "providerRejected",
                    zero,
                    receipt,
                    failure(
                        self.operation,
                        FailureKind.PROVIDER_REJECTION,
                        "provider rejected the update at the required revision",
                    ),
                )
            return self._changed(
                input, document["revisionId"], current, self.client.receipt(call, reread, input.document_id)
            )
        if exchange.status == 404:
            return MutationAttempt.branch_result(
                "notFound",
                zero,
                receipt,
                failure(
                    self.operation,
                    FailureKind.NOT_FOUND,
                    "document was not found or is not visible to the connection",
                ),
            )
        return MutationAttempt.branch_result(
            "providerRejected",
            zero,
            receipt,
            failure(self.operation, status_kind(exchange.status, tokens), "provider rejected the update"),
        )


class ReplaceDocumentTextOperation(_GuardedWrite):
    """Replace first-tab text at a required revision."""

    def __init__(self, client: Client) -> None:
        super().__init__(client, "replaceDocumentText")

    def definition(self) -> Any:
        """Return the immutable mutation definition."""
        return REPLACE_DOCUMENT_TEXT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ReplaceDocumentTextInput) -> IdempotencyKey:
        """Use the stable Call ID; the revision makes repeats safe."""
        return IdempotencyKey(call_id)


class AppendTextOperation(_GuardedWrite):
    """Append first-tab text at a required revision."""

    def __init__(self, client: Client) -> None:
        super().__init__(client, "appendText")

    def definition(self) -> Any:
        """Return the immutable mutation definition."""
        return APPEND_TEXT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AppendTextInput) -> IdempotencyKey:
        """Use the stable Call ID; the revision makes repeats safe."""
        return IdempotencyKey(call_id)


class CreateDocumentOperation:
    """Create one Google Doc with a single-dispatch checkpoint and Drive read-back."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the immutable mutation definition."""
        return CREATE_DOCUMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateDocumentInput) -> IdempotencyKey:
        """Use the stable Call ID as a private Drive app property."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateDocumentInput) -> MutationAttempt[CreateDocumentOutput]:
        """Validate the create input before entering the single-dispatch fence."""
        from .create import create_document

        return await create_document(self.client, call, input)
