# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Front operation invocation and Go-compatible branch classification."""

import html
import json
import re
import unicodedata
from dataclasses import replace
from datetime import timedelta
from email.utils import parseaddr
from typing import Any
from urllib.parse import quote

from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialRefreshResult,
    CredentialRefreshState,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    RejectedCredentialRefreshingProvider,
    resolve_credential_after_rejection,
)
from dex_connectors.sdk.providerhttp import is_header_safe_credential
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated import definitions as definitions
from .client import (
    Client,
    Credentials,
    Exchange,
    decode_conversation,
    decode_json,
    decode_page,
    failure,
    merged_conversation_id,
    receipt,
    resource_id,
    timestamp,
    truncate_utf8,
)
from .models import (
    Comment,
    Contact,
    ContactHandle,
    Conversation,
    ConversationDetails,
    ConversationReply,
    FindContactByEmailInput,
    FindContactByEmailOutput,
    GetConversationInput,
    Message,
    ReplyToConversationInput,
    SearchConversationsInput,
    SearchConversationsOutput,
    UpdateConversationInput,
    UpdateConversationOutput,
)

_BARE_EMAIL = re.compile(r"[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]{1,64}@[A-Za-z0-9.-]{1,253}\Z")
_PAGE_TOKEN = re.compile(r"[A-Za-z0-9._~-]{1,1000}\Z")
_STATUSES = {
    "open",
    "archived",
    "snoozed",
    "trashed",
    "assigned",
    "unassigned",
    "unreplied",
    "waiting",
    "resolved",
}


def _bare_email(value: str, field: str) -> None:
    display, address = parseaddr(value)
    local, _, domain = value.partition("@")
    has_invalid_dots = any(
        part.startswith(".") or part.endswith(".") or ".." in part for part in (local, domain)
    )
    if display or address != value or not _BARE_EMAIL.fullmatch(value) or has_invalid_dots:
        raise ValueError(f"{field} must be one bare email address such as jane@acme.example.com")


def _query(input: SearchConversationsInput) -> str:
    terms: list[str] = []
    text = input.text.strip()
    if text:
        if len(text.encode()) > 512 or any(
            unicodedata.category(character) == "Cc" for character in text
        ):
            raise ValueError("text must be at most 512 bytes without control characters")
        terms.append(text)
    for field, value, kind, example in (
        ("inboxId", input.inbox_id, "inbox", "inb_41w25"),
        ("tagId", input.tag_id, "tag", "tag_13o8r1"),
        ("assigneeId", input.assignee_id, "teammate", "tea_2thf"),
    ):
        if value:
            resource_id(value, kind, field, example)
    if input.inbox_id:
        terms.append("inbox:" + input.inbox_id)
    if input.tag_id:
        terms.append("tag:" + input.tag_id)
    for status in dict.fromkeys(input.statuses):
        if status not in _STATUSES:
            raise ValueError(
                f"statuses must be Front is: values such as open, archived, or unassigned, not {status!r}"
            )
        terms.append("is:" + status)
    if input.recipient_email:
        _bare_email(input.recipient_email, "recipientEmail")
        terms.append("recipient:" + input.recipient_email)
    if input.assignee_id:
        terms.append("assignee:" + input.assignee_id)
    if input.activity_after is not None:
        seconds = int(input.activity_after.timestamp())
        if seconds < 1:
            raise ValueError("activityAfter must be after 1970")
        terms.append("after:" + str(seconds))
    if not terms:
        raise ValueError("set text or at least one filter; Front has no empty search")
    return " ".join(terms)


def _search_input(input: SearchConversationsInput) -> str:
    query = _query(input)
    if input.page_size < 0 or input.page_size > 100:
        raise ValueError("pageSize must be 1 to 100, or zero for 25")
    if input.page_token and not _PAGE_TOKEN.fullmatch(input.page_token):
        raise ValueError("pageToken must be a nextPageToken from an earlier search")
    return query


def _conversation_id(value: str) -> None:
    resource_id(value, "conversation", "conversationId", "cnv_55c8c149")


def _retry_query(exchange: Exchange) -> QueryAttempt[Any]:
    assert exchange.error is not None
    return QueryAttempt.retry(exchange.error, exchange.retry_after)


def _retry_mutation(exchange: Exchange) -> MutationAttempt[Any]:
    assert exchange.error is not None
    return MutationAttempt.retry(exchange.error, exchange.retry_after)


class SearchConversationsOperation:
    """Search one bounded conversation page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated operation identity and defaults."""
        return definitions.SEARCH_CONVERSATIONS_DEFINITION

    async def invoke(
        self, call: Call, input: SearchConversationsInput
    ) -> QueryAttempt[SearchConversationsOutput]:
        """Validate filters, read one page, and choose a typed route."""
        operation = "searchConversations"
        try:
            query = _search_input(input)
        except ValueError as error:
            output = SearchConversationsOutput(query="")
            return QueryAttempt.branch_result(
                "defect", output, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        output = SearchConversationsOutput(query=query)
        credentials, credential_failure, _ = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return QueryAttempt.branch_result("defect", output, Receipt(), credential_failure)
        assert credentials is not None
        exchange = await self.client.exchange(
            credentials,
            operation,
            "GET",
            "/conversations/search/" + quote(query, safe=""),
            parameters={
                "limit": str(input.page_size or 25),
                **({"page_token": input.page_token} if input.page_token else {}),
            },
        )
        observed = receipt(call)
        if exchange.retryable:
            return _retry_query(exchange)
        if exchange.outcome != "succeeded":
            branch = (
                "defect"
                if exchange.outcome == "defect"
                else "invalidResponse"
                if exchange.outcome == "invalid"
                else "providerRejected"
            )
            return QueryAttempt.branch_result(
                branch, output, Receipt() if branch == "defect" else observed, exchange.error
            )
        try:
            page = decode_page(exchange.body, "the page is not a conversation list")
            conversations = tuple(decode_conversation(item) for item in page["_results"])
            total = page.get("_total")
            token = self.client.next_page_token(
                page.get("_pagination", {}).get("next"), "/conversations/search/"
            )
            output = SearchConversationsOutput(
                conversations, total if isinstance(total, int) and total >= 0 else 0, token, query
            )
        except (ValueError, TypeError, KeyError, json.JSONDecodeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                SearchConversationsOutput(query=query),
                observed,
                failure(
                    FailureKind.PROTOCOL, operation, "Front returned an invalid search page: " + str(error)
                ),
            )
        return QueryAttempt.branch_result("searched", output, observed)


class GetConversationOperation:
    """Read a conversation, newest messages, and comments."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated operation identity and defaults."""
        return definitions.GET_CONVERSATION_DEFINITION

    async def invoke(self, call: Call, input: GetConversationInput) -> QueryAttempt[ConversationDetails]:
        """Read three provider resources in the Go connector's order."""
        operation = "getConversation"
        output = ConversationDetails(Conversation(status=""))
        try:
            _conversation_id(input.conversation_id)
            if input.message_limit < 0 or input.message_limit > 25:
                raise ValueError("messageLimit must be 1 to 25, or zero for 10")
            if input.comment_limit < 0 or input.comment_limit > 25:
                raise ValueError("commentLimit must be 1 to 25, or zero for 10")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", output, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials, credential_failure, _ = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return QueryAttempt.branch_result("defect", output, Receipt(), credential_failure)
        assert credentials is not None
        path = "/conversations/" + input.conversation_id
        observed = receipt(call, input.conversation_id)
        for index, (read_path, parameters) in enumerate(
            (
                (path, None),
                (path + "/messages", {"limit": str(input.message_limit or 10)}),
                (path + "/comments", None),
            )
        ):
            exchange = await self.client.exchange(
                credentials, operation, "GET", read_path, parameters=parameters
            )
            if exchange.retryable:
                return _retry_query(exchange)
            if exchange.outcome != "succeeded":
                if exchange.outcome == "merged":
                    output = replace(
                        output,
                        merged_into_conversation_id=merged_conversation_id(
                            exchange.headers.get("Location", "") if exchange.headers else ""
                        ),
                    )
                branch = {
                    "merged": "merged",
                    "notFound": "notFound",
                    "defect": "defect",
                    "invalid": "invalidResponse",
                }.get(exchange.outcome, "providerRejected")
                return QueryAttempt.branch_result(
                    branch, output, Receipt() if branch == "defect" else observed, exchange.error
                )
            try:
                if index == 0:
                    conversation = decode_conversation(decode_json(exchange.body))
                    if conversation.id != input.conversation_id:
                        raise ValueError("Front returned another conversation")
                    output = replace(output, conversation=conversation)
                elif index == 1:
                    page = decode_page(exchange.body, "the message page is not a message list")
                    messages = []
                    for item in page["_results"]:
                        resource_id(item.get("id", ""), "message", "message", "msg_1q15qmtq")
                        body, is_truncated = truncate_utf8(item.get("body", ""))
                        messages.append(
                            Message(
                                id=item["id"],
                                type=item.get("type", ""),
                                is_inbound=item.get("is_inbound", False),
                                is_draft=bool(item.get("draft_mode")),
                                subject=item.get("subject", ""),
                                blurb=item.get("blurb", ""),
                                body=body,
                                is_body_truncated=is_truncated,
                                author=_teammate_for_operation(item.get("author")),
                                recipients=tuple(
                                    _recipient_for_operation(recipient)
                                    for recipient in item.get("recipients") or ()
                                ),
                                created_at=timestamp(item.get("created_at"))
                                or Conversation(status="").created_at,
                            )
                        )
                    token = self.client.next_page_token(
                        page.get("_pagination", {}).get("next"), path + "/messages"
                    )
                    output = replace(output, messages=tuple(messages), has_older_messages=bool(token))
                else:
                    page = decode_page(exchange.body, "the comments answer has no _results list")
                    comments: list[Comment] = []
                    for item in page["_results"]:
                        if len(comments) == (input.comment_limit or 10):
                            break
                        resource_id(item.get("id", ""), "comment", "comment", "com_1ywg3f2")
                        body, is_truncated = truncate_utf8(item.get("body", ""))
                        comments.append(
                            Comment(
                                id=item["id"],
                                body=body,
                                is_body_truncated=is_truncated,
                                author=_teammate_for_operation(item.get("author")),
                                is_pinned=item.get("is_pinned", False),
                                posted_at=timestamp(item.get("posted_at"))
                                or Conversation(status="").created_at,
                            )
                        )
                    output = replace(
                        output,
                        comments=tuple(comments),
                        has_older_comments=len(page["_results"]) > (input.comment_limit or 10),
                    )
            except (ValueError, TypeError, KeyError, json.JSONDecodeError) as error:
                return QueryAttempt.branch_result(
                    "invalidResponse",
                    ConversationDetails(Conversation(status="")),
                    observed,
                    failure(
                        FailureKind.PROTOCOL,
                        operation,
                        "Front returned an invalid conversation: " + str(error),
                    ),
                )
        return QueryAttempt.branch_result("found", output, observed)


def _teammate_for_operation(wire: dict[str, Any] | None) -> Any:
    from .client import _teammate

    return _teammate(wire)


def _recipient_for_operation(wire: dict[str, Any]) -> Any:
    from .client import _recipient

    return _recipient(wire)


class FindContactByEmailOperation:
    """Read a contact by its exact email alias."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated operation identity and defaults."""
        return definitions.FIND_CONTACT_BY_EMAIL_DEFINITION

    async def invoke(
        self, call: Call, input: FindContactByEmailInput
    ) -> QueryAttempt[FindContactByEmailOutput]:
        """Resolve one email alias and verify it belongs to the returned contact."""
        operation = "findContactByEmail"
        output = FindContactByEmailOutput()
        try:
            _bare_email(input.email, "email")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", output, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials, credential_failure, _ = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return QueryAttempt.branch_result("defect", output, Receipt(), credential_failure)
        assert credentials is not None
        exchange = await self.client.exchange(
            credentials, operation, "GET", "/contacts/alt:email:" + quote(input.email, safe="-._~+:=&@$")
        )
        observed = receipt(call)
        if exchange.retryable:
            return _retry_query(exchange)
        if exchange.outcome != "succeeded":
            branch = {"notFound": "notFound", "defect": "defect", "invalid": "invalidResponse"}.get(
                exchange.outcome, "providerRejected"
            )
            return QueryAttempt.branch_result(
                branch, output, Receipt() if branch == "defect" else observed, exchange.error
            )
        try:
            wire = decode_json(exchange.body)
            resource_id(wire.get("id", ""), "contact", "contact", "crd_1y8sp71")
            handles = tuple(
                ContactHandle(item.get("handle", ""), item.get("source", ""))
                for item in wire.get("handles") or ()
            )
            if not any(
                item.source == "email" and item.handle.lower() == input.email.lower() for item in handles
            ):
                raise ValueError("the contact does not hold the requested email handle")
            contact = Contact(
                wire["id"],
                wire.get("name", ""),
                wire.get("description", ""),
                handles,
                tuple(item["name"] for item in wire.get("lists") or () if item.get("name")),
                wire.get("is_private", False),
                timestamp(wire.get("updated_at")),
            )
        except (ValueError, TypeError, KeyError, json.JSONDecodeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                output,
                observed,
                failure(FailureKind.PROTOCOL, operation, "Front returned an invalid contact: " + str(error)),
            )
        return QueryAttempt.branch_result(
            "found", FindContactByEmailOutput((contact,)), receipt(call, contact.id)
        )


def _validate_update(input: UpdateConversationInput) -> None:
    _conversation_id(input.conversation_id)
    if input.status and input.status not in ("open", "archived", "deleted"):
        raise ValueError(f"status must be open, archived, or deleted, not {input.status!r}")
    if input.status and input.status_id:
        raise ValueError("set status or statusId, not both")
    if input.assignee_id and input.is_unassigned:
        raise ValueError("set assigneeId or isUnassigned, not both")
    if len(input.add_tag_ids) > 25 or len(input.remove_tag_ids) > 25:
        raise ValueError("addTagIds and removeTagIds hold at most 25 tag IDs each")
    if not (
        input.status
        or input.status_id
        or input.assignee_id
        or input.is_unassigned
        or input.add_tag_ids
        or input.remove_tag_ids
    ):
        raise ValueError(
            "set at least one of status, statusId, assigneeId, isUnassigned, addTagIds, or removeTagIds"
        )
    if input.status_id:
        resource_id(input.status_id, "status", "statusId", "sts_5x")
    if input.assignee_id:
        resource_id(input.assignee_id, "teammate", "assigneeId", "tea_2thf")
    for tag in (*input.add_tag_ids, *input.remove_tag_ids):
        resource_id(tag, "tag", "addTagIds and removeTagIds", "tag_13o8r1")
    for tag in input.add_tag_ids:
        if tag in input.remove_tag_ids:
            raise ValueError(f"tag {tag} is both added and removed")


def _planned_writes(
    input: UpdateConversationInput, current: Conversation, path: str
) -> list[tuple[str, str, dict[str, Any]]]:
    patch: dict[str, Any] = {}
    status_applied = (
        input.status == "open" and current.status in ("assigned", "unassigned")
    ) or current.status == input.status
    if input.status and not status_applied:
        patch["status"] = input.status
    if input.status_id and current.status_id != input.status_id:
        patch["status_id"] = input.status_id
    assignee_id = current.assignee.id if current.assignee else ""
    if input.assignee_id and assignee_id != input.assignee_id:
        patch["assignee_id"] = input.assignee_id
    elif input.is_unassigned and assignee_id:
        patch["assignee_id"] = None
    writes = [("PATCH", path, patch)] if patch else []
    current_tags = {tag.id for tag in current.tags}
    additions = list(dict.fromkeys(tag for tag in input.add_tag_ids if tag not in current_tags))
    removals = list(dict.fromkeys(tag for tag in input.remove_tag_ids if tag in current_tags))
    if additions:
        writes.append(("POST", path + "/tags", {"tag_ids": additions}))
    if removals:
        writes.append(("DELETE", path + "/tags", {"tag_ids": removals}))
    return writes


def _update_failure(
    exchange: Exchange, output: UpdateConversationOutput, observed: Receipt, note: str = ""
) -> MutationAttempt[UpdateConversationOutput]:
    assert exchange.error is not None
    if exchange.retryable:
        return _retry_mutation(exchange)
    error = exchange.error
    if exchange.outcome == "merged":
        merged = merged_conversation_id(exchange.headers.get("Location", "") if exchange.headers else "")
        if merged:
            error = failure(
                error.kind,
                "updateConversation",
                f"the conversation was merged into {merged}; update that conversation",
            )
    if note:
        error = failure(error.kind, error.operation, error.message + note)
    branch = {
        "merged": "notFound",
        "notFound": "notFound",
        "defect": "defect",
        "invalid": "invalidResponse",
    }.get(exchange.outcome, "providerRejected")
    return MutationAttempt.branch_result(branch, output, Receipt() if branch == "defect" else observed, error)


class UpdateConversationOperation:
    """Read, apply only missing changes, and verify converged state."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated operation identity and defaults."""
        return definitions.UPDATE_CONVERSATION_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateConversationInput) -> IdempotencyKey:
        """Use the stable Call ID; Front does not accept a provider key."""
        return IdempotencyKey(call_id)

    async def invoke(
        self, call: Call, input: UpdateConversationInput
    ) -> MutationAttempt[UpdateConversationOutput]:
        """Repeat absolute writes safely after lost answers."""
        operation = "updateConversation"
        empty = UpdateConversationOutput(Conversation(status=""))
        try:
            _validate_update(input)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials, credential_failure, _ = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return MutationAttempt.branch_result("defect", empty, Receipt(), credential_failure)
        assert credentials is not None
        path = "/conversations/" + input.conversation_id
        observed = receipt(call, input.conversation_id)

        async def read() -> tuple[Conversation | None, MutationAttempt[UpdateConversationOutput] | None]:
            answer = await self.client.exchange(credentials, operation, "GET", path)
            if answer.outcome != "succeeded":
                return None, _update_failure(answer, empty, observed)
            try:
                current = decode_conversation(decode_json(answer.body))
                if current.id != input.conversation_id:
                    raise ValueError("Front returned another conversation")
                return current, None
            except (ValueError, TypeError, KeyError, json.JSONDecodeError) as error:
                return None, MutationAttempt.branch_result(
                    "invalidResponse",
                    empty,
                    observed,
                    failure(
                        FailureKind.PROTOCOL,
                        operation,
                        "Front returned an invalid conversation: " + str(error),
                    ),
                )

        current, terminal = await read()
        if terminal:
            return terminal
        assert current is not None
        writes = _planned_writes(input, current, path)
        for index, (method, write_path, payload) in enumerate(writes):
            answer = await self.client.exchange(credentials, operation, method, write_path, payload=payload)
            if answer.outcome == "notFound":
                answer = replace(answer, outcome="rejected")
            if answer.outcome != "succeeded":
                return _update_failure(
                    answer, empty, observed, "; writes sent before it stay applied" if index else ""
                )
        if not writes:
            return MutationAttempt.branch_result("updated", UpdateConversationOutput(current, True), observed)
        updated, terminal = await read()
        if terminal:
            return terminal
        assert updated is not None
        if _planned_writes(input, updated, path):
            return MutationAttempt.retry(
                failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    "Front accepted the change but does not show it on the conversation yet",
                )
            )
        return MutationAttempt.branch_result("updated", UpdateConversationOutput(updated), observed)


def build_reply_html(text: str) -> str:
    """Escape each blank-line-separated paragraph for Front's HTML message body."""
    text = text.replace("\r\n", "\n").replace("\r", "\n")
    return "".join(
        "<p>"
        + html.escape(block, quote=True)
        .replace("&#x27;", "&#39;")
        .replace("&quot;", "&#34;")
        .replace("\n", "<br>")
        + "</p>"
        for block in (part.strip("\n") for part in text.split("\n\n"))
        if block.strip()
    )


def _validate_reply(input: ReplyToConversationInput) -> None:
    _conversation_id(input.conversation_id)
    if not input.text.strip():
        raise ValueError("text is required")
    if len(input.text.encode()) > 65536:
        raise ValueError("text is longer than 65536 bytes")
    if input.is_internal_note and input.should_archive:
        raise ValueError("shouldArchive applies only to a reply, not to an internal comment")
    if input.author_id:
        resource_id(input.author_id, "teammate", "authorId", "tea_2thf")


def _reply_payload(input: ReplyToConversationInput) -> tuple[str, dict[str, Any]]:
    path = "/conversations/" + input.conversation_id
    if input.is_internal_note:
        payload: dict[str, Any] = {"body": input.text}
        if input.author_id:
            payload["author_id"] = input.author_id
        return path + "/comments", payload
    payload = {
        "body": build_reply_html(input.text),
        "text": input.text,
        "options": {"archive": input.should_archive},
    }
    if input.author_id:
        payload["author_id"] = input.author_id
    return path + "/messages", payload


class ReplyToConversationOperation:
    """Fence a non-idempotent Front message or comment send."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return generated sync-durable operation defaults."""
        return definitions.REPLY_TO_CONVERSATION_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ReplyToConversationInput) -> IdempotencyKey:
        """Record the Call ID without sending it as a provider header."""
        return IdempotencyKey(call_id)

    def _uncertain(
        self, call: Call, output: ConversationReply, kind: FailureKind, message: str, object_id: str = ""
    ) -> MutationAttempt[ConversationReply]:
        return MutationAttempt.uncertain(
            output,
            failure(kind, "replyToConversation", message),
            receipt(call, object_id or output.conversation_id),
        )

    async def _rejected_credential(
        self, call: Call, output: ConversationReply, rejected: Credentials, fence: Fence
    ) -> MutationAttempt[ConversationReply]:
        operation = "replyToConversation"
        observed = receipt(call, output.conversation_id)
        if not isinstance(self.client.credentials, RejectedCredentialRefreshingProvider):
            return MutationAttempt.branch_result(
                "providerRejected",
                output,
                observed,
                failure(
                    FailureKind.AUTHENTICATION,
                    operation,
                    "Front rejected the API token (HTTP 401); reconnect the Front connection",
                ),
            )
        refresh_error: Exception | None = None
        try:
            replacement = await resolve_credential_after_rejection(
                self.client.credentials, call, _FrontCredentialRefreshDriver()
            )
        except Exception as error:
            replacement = None
            refresh_error = error
        if (
            replacement is not None
            and replacement.api_token.reveal()
            and replacement.api_token.reveal() != rejected.api_token.reveal()
            and is_header_safe_credential(replacement.api_token.reveal())
        ):
            try:
                await fence.record_can_resend()
            except Exception:
                return MutationAttempt.retry(
                    failure(FailureKind.AVAILABILITY, operation, "the credential resend checkpoint failed")
                )
            return MutationAttempt.retry(
                failure(
                    FailureKind.AUTHENTICATION,
                    operation,
                    "Front rejected the prior API token; retry with its replacement",
                )
            )
        message = "Front rejected the API token (HTTP 401); reconnect the Front connection"
        if refresh_error is not None and not isinstance(refresh_error, ReauthorizationRequiredError):
            message = "Front rejected the API token (HTTP 401); credential refresh is temporarily unavailable"
        return MutationAttempt.branch_result(
            "providerRejected", output, observed, failure(FailureKind.AUTHENTICATION, operation, message)
        )

    async def _read_back(
        self, call: Call, input: ReplyToConversationInput, output: ConversationReply, marker: Any
    ) -> MutationAttempt[ConversationReply]:
        if marker is None or marker.call_id != call.id:
            return self._uncertain(
                call,
                output,
                FailureKind.AVAILABILITY,
                "an earlier attempt has no trusted dispatch checkpoint",
            )
        if not marker.provider_id:
            if marker.state.value == "claimed":
                return self._uncertain(
                    call,
                    output,
                    FailureKind.TRANSPORT,
                    "an earlier attempt may have sent the reply or comment without a provider ID",
                )
            return self._uncertain(
                call,
                output,
                FailureKind.AVAILABILITY,
                "a stale checkpoint cannot establish whether the reply or comment was sent",
            )
        pattern = r"com_[A-Za-z0-9]{1,40}\Z" if input.is_internal_note else r"[A-Za-z0-9]{1,64}\Z"
        if not re.fullmatch(pattern, marker.provider_id):
            return self._uncertain(
                call, output, FailureKind.PROTOCOL, "an earlier attempt stored an invalid provider ID"
            )
        credentials, credential_failure, credential_error = await self.client.resolve_credentials(
            call, "replyToConversation"
        )
        if credential_failure:
            if credential_error is None or isinstance(credential_error, ReauthorizationRequiredError):
                return self._uncertain(
                    call,
                    output,
                    FailureKind.AUTHENTICATION,
                    (
                        "Front accepted an earlier reply or comment, but its connection needs reconnection "
                        "before read-back; verify the saved provider ID before retrying manually"
                    ),
                    marker.provider_id,
                )
            return MutationAttempt.retry(
                failure(
                    FailureKind.AVAILABILITY,
                    "replyToConversation",
                    "Front credentials were temporarily unavailable during read-back",
                )
            )
        assert credentials is not None
        path = ("/comments/" if input.is_internal_note else "/messages/alt:uid:") + marker.provider_id
        answer = await self.client.exchange(credentials, "replyToConversation", "GET", path)
        if answer.outcome == "succeeded":
            confirmed = (
                replace(output, comment_id=marker.provider_id)
                if input.is_internal_note
                else replace(output, message_uid=marker.provider_id)
            )
            return MutationAttempt.branch_result("replied", confirmed, receipt(call, marker.provider_id))
        if answer.retryable or (answer.outcome == "notFound" and not input.is_internal_note):
            return _retry_mutation(answer)
        assert answer.error is not None
        message = "Front could not confirm an earlier accepted reply or comment"
        if answer.error.kind == FailureKind.AUTHENTICATION:
            message = (
                "Front rejected credentials during accepted-write read-back; reconnect and verify "
                "the saved provider ID before retrying manually"
            )
        return self._uncertain(call, output, answer.error.kind, message, marker.provider_id)

    async def invoke(self, call: Call, input: ReplyToConversationInput) -> MutationAttempt[ConversationReply]:
        """Checkpoint before sending and fail closed after ambiguous delivery."""
        operation = "replyToConversation"
        output = ConversationReply(input.conversation_id, input.is_internal_note, author_id=input.author_id)
        try:
            _validate_reply(input)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", output, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        try:
            fence = Fence(call, timedelta(seconds=30))
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", output, Receipt(), failure(FailureKind.LOCAL_DEFECT, operation, str(error))
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return self._uncertain(
                call,
                output,
                FailureKind.AVAILABILITY,
                "the dispatch checkpoint could not be trusted; the reply or comment is not sent again",
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return await self._read_back(call, input, output, decision.marker)
        credentials, credential_failure, credential_error = await self.client.resolve_credentials(
            call, operation
        )
        if credential_failure:
            try:
                await fence.record_not_sent()
            except (CheckpointError, DispatchNotAllowedError, Exception):
                return MutationAttempt.retry(
                    failure(
                        FailureKind.AVAILABILITY,
                        operation,
                        "Front credentials were temporarily unavailable and the not-sent checkpoint failed",
                    )
                )
            if credential_error is None or isinstance(credential_error, ReauthorizationRequiredError):
                return MutationAttempt.branch_result(
                    "defect",
                    output,
                    Receipt(),
                    failure(
                        FailureKind.AUTHENTICATION,
                        operation,
                        "Reconnect the Front connection before sending the reply or comment",
                    ),
                )
            return MutationAttempt.retry(
                failure(FailureKind.AVAILABILITY, operation, "Front credentials were temporarily unavailable")
            )
        assert credentials is not None
        path, payload = _reply_payload(input)

        async def send(deadline: Any) -> Exchange:
            return await self.client.exchange(credentials, operation, "POST", path, payload=payload)

        try:
            answer = await fence.dispatch(send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    "the reply was not dispatched; retry after the checkpoint or attempt budget recovers",
                )
            )
        except Exception:
            return self._uncertain(
                call, output, FailureKind.TRANSPORT, "the fenced reply outcome could not be determined"
            )
        observed = receipt(call, input.conversation_id)
        if 200 <= answer.status < 300:
            try:
                body = decode_json(answer.body)
            except (ValueError, json.JSONDecodeError):
                body = {}
            provider_id = body.get("id", "") if input.is_internal_note else body.get("message_uid", "")
            pattern = r"com_[A-Za-z0-9]{1,40}\Z" if input.is_internal_note else r"[A-Za-z0-9]{1,64}\Z"
            if not isinstance(provider_id, str) or not re.fullmatch(pattern, provider_id):
                kind = answer.error.kind if answer.error else FailureKind.PROTOCOL
                return self._uncertain(
                    call,
                    output,
                    kind,
                    "Front accepted the reply or comment, but its answer has no usable provider ID; "
                    "do not resend",
                )
            try:
                await fence.record_provider_id(provider_id)
            except Exception:
                pass  # Acceptance and provider identity are already known; the checkpoint is best effort.
            output = (
                replace(output, comment_id=provider_id)
                if input.is_internal_note
                else replace(output, message_uid=provider_id)
            )
            return MutationAttempt.branch_result("replied", output, receipt(call, provider_id))
        assert answer.error is not None
        if answer.outcome == "defect":
            return MutationAttempt.branch_result("defect", output, Receipt(), answer.error)
        if answer.outcome in ("notSent", "rateLimited") or answer.status == 503:
            try:
                await fence.record_can_resend()
            except Exception:
                return MutationAttempt.retry(
                    failure(FailureKind.AVAILABILITY, operation, "the resend checkpoint failed")
                )
            return _retry_mutation(answer)
        if answer.outcome == "unconfirmed":
            return MutationAttempt.uncertain(
                output,
                failure(
                    answer.error.kind, operation, answer.error.message + "; the reply or comment may exist"
                ),
                observed,
            )
        if answer.outcome in ("notFound", "merged"):
            reply_failure = answer.error
            if answer.outcome == "merged":
                merged = merged_conversation_id(answer.headers.get("Location", "") if answer.headers else "")
                if merged:
                    reply_failure = failure(
                        reply_failure.kind,
                        operation,
                        f"the conversation was merged into {merged}; reply to that conversation",
                    )
            return MutationAttempt.branch_result("notFound", output, observed, reply_failure)
        if answer.status == 409:
            return self._uncertain(
                call,
                output,
                FailureKind.CONFLICT,
                "Front returned a conflict without a key or unique value for read-back",
            )
        if answer.status == 401:
            return await self._rejected_credential(call, output, credentials, fence)
        return MutationAttempt.branch_result("providerRejected", output, observed, answer.error)


class _FrontCredentialRefreshDriver:
    def refresh_required(self, state: CredentialRefreshState[Credentials]) -> bool:
        return False

    async def refresh(
        self, state: CredentialRefreshState[Credentials]
    ) -> CredentialRefreshResult[Credentials]:
        raise ReauthorizationRequiredError()
