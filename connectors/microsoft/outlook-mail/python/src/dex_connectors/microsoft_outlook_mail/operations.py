# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Outlook Mail Query and repeat-safe Mutation implementations."""

from dataclasses import replace
from datetime import UTC, datetime
from typing import Any, TypeVar

from dex_connectors.sdk import (
    Call,
    CallID,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)

from ._generated.definitions import (
    GET_MESSAGE_DEFINITION,
    MOVE_MESSAGE_DEFINITION,
    REPLY_TO_MESSAGE_DEFINITION,
    SEARCH_MESSAGES_DEFINITION,
    SEND_MESSAGE_DEFINITION,
    SET_MESSAGE_FLAGS_DEFINITION,
)
from .client import (
    Client,
    Exchange,
    failure,
    is_bare_address,
    path_escape,
    validate_graph_id,
    validate_next_page_link,
)
from .message import (
    SUMMARY_PROPERTIES,
    decode_attachment_list,
    decode_full_message,
    decode_graph_message,
    json_object,
    message_summary,
    string_value,
)
from .models import (
    GetMessageInput,
    Message,
    MessageFlags,
    MovedMessage,
    MoveMessageInput,
    ReplyToMessageInput,
    SearchMessagesInput,
    SearchMessagesOutput,
    SendMessageInput,
    SentMessage,
    SetMessageFlagsInput,
)

T = TypeVar("T")
WELL_KNOWN_FOLDERS = frozenset(
    (
        "archive",
        "clutter",
        "conflicts",
        "conversationhistory",
        "deleteditems",
        "drafts",
        "inbox",
        "junkemail",
        "localfailures",
        "msgfolderroot",
        "outbox",
        "recoverableitemsdeletions",
        "scheduled",
        "searchfolders",
        "sentitems",
        "serverfailures",
        "syncissues",
    )
)


def resolve_folder(field: str, value: str, fallback: str = "") -> str:
    """Resolve a well-known Graph folder or opaque folder ID."""
    folder = value.strip() or fallback
    if not folder:
        raise ValueError(
            f"{field} is required: a folder ID from the mail folder picker or a well-known name such as archive"  # noqa: E501
        )
    if folder.lower() in WELL_KNOWN_FOLDERS:
        return folder.lower()
    try:
        validate_graph_id(field, folder)
    except ValueError as error:
        raise ValueError(
            f"{field} must be a folder ID from the mail folder picker or a well-known name such as inbox, archive, or deleteditems"  # noqa: E501
        ) from error
    return folder


def validate_address(field: str, address: str) -> None:
    """Require a bare address without a display name or line break."""
    if not is_bare_address(address):
        raise ValueError(f"{field} must contain bare addresses such as jane@acme.example.com")


def validate_single_line(field: str, value: str, maximum: int) -> None:
    """Apply Graph's character and control-character limits."""
    if len(value) > maximum:
        raise ValueError(f"{field} can be at most {maximum} characters")
    if any(ord(character) < 32 or ord(character) == 127 for character in value):
        raise ValueError(f"{field} must be one line without control characters")


def quote_odata(value: str) -> str:
    """Quote a Graph OData string literal."""
    return "'" + value.replace("'", "''") + "'"


def odata_time(value: str) -> str:
    """Format a generated Go timestamp to UTC second precision for Graph."""
    instant = datetime.fromisoformat(value.replace("Z", "+00:00"))
    return instant.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def query_outcome(
    result: Exchange, receipt: Receipt, rejected: str = "providerRejected", not_found: str = ""
) -> QueryAttempt[Any] | None:
    """Map a failed Graph read to a branch or retry."""
    if result.outcome == "succeeded":
        return None
    assert result.problem is not None
    empty = (
        SearchMessagesOutput() if result.problem.operation == "searchMessages" else Message(flag_status="")
    )
    if result.outcome in ("retry", "ambiguous"):
        return QueryAttempt.retry(result.problem, result.retry_after)
    if result.outcome == "not_found":
        return QueryAttempt.branch_result(not_found or rejected, empty, receipt, result.problem)
    return QueryAttempt.branch_result(
        {"invalid": "invalidResponse", "defect": "defect"}.get(result.outcome, rejected),
        empty,
        receipt,
        result.problem,
    )


def update_outcome(
    result: Exchange, receipt: Receipt, not_found: str = "notFound"
) -> MutationAttempt[Any] | None:
    """Map a failed repeat-safe Graph write to a branch or retry."""
    if result.outcome == "succeeded":
        return None
    assert result.problem is not None
    empty = MovedMessage() if result.problem.operation == "moveMessage" else MessageFlags(flag_status="")
    if result.outcome in ("retry", "ambiguous", "invalid"):
        return MutationAttempt.retry(result.problem, result.retry_after)
    return MutationAttempt.branch_result(
        "defect"
        if result.outcome == "defect"
        else not_found
        if result.outcome == "not_found"
        else "providerRejected",
        empty,
        receipt,
        result.problem,
    )


class SearchMessagesOperation:
    """Search one Graph folder page with the Go connector's filter order."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return stable searchMessages routes and defaults."""
        return SEARCH_MESSAGES_DEFINITION

    async def invoke(self, call: Call, input: SearchMessagesInput) -> QueryAttempt[SearchMessagesOutput]:
        """Read a bounded message page."""
        try:
            folder = resolve_folder("folder", input.folder, "inbox")
            if input.page_cursor:
                validate_next_page_link(input.page_cursor)
                path = ""
                query: tuple[tuple[str, str], ...] = ()
            else:
                after = odata_time(input.received_after) if input.received_after else "1900-01-01T00:00:00Z"
                clauses = ["receivedDateTime ge " + after]
                if input.received_before:
                    before = odata_time(input.received_before)
                    if before <= after:
                        raise ValueError("receivedBefore must be later than receivedAfter")
                    clauses.append("receivedDateTime lt " + before)
                if input.from_.strip():
                    sender = input.from_.strip()
                    validate_address("from", sender)
                    clauses.append("from/emailAddress/address eq " + quote_odata(sender))
                if input.is_unread:
                    clauses.append("isRead eq false")
                if input.subject_contains.strip():
                    subject = input.subject_contains.strip()
                    validate_single_line("subjectContains", subject, 255)
                    clauses.append("contains(subject," + quote_odata(subject) + ")")
                limit = input.limit or 10
                if not 1 <= limit <= 50:
                    raise ValueError("limit must be between 1 and 50")
                path = "/mailFolders/" + path_escape(folder) + "/messages"
                query = (
                    ("$filter", " and ".join(clauses)),
                    ("$orderby", "receivedDateTime desc"),
                    ("$top", str(limit)),
                    ("$select", ",".join(SUMMARY_PROPERTIES)),
                )
        except (ValueError, OverflowError) as error:
            return QueryAttempt.branch_result(
                "defect",
                SearchMessagesOutput(),
                Receipt(),
                failure("searchMessages", FailureKind.VALIDATION, str(error)),
            )
        session, failed = await self.client.open_session(call, "searchMessages")
        if failed is not None:
            return query_outcome(failed, Receipt())  # type: ignore[return-value]
        assert session is not None
        result = await session.exchange("GET", path, query, absolute_url=input.page_cursor)
        receipt = self.client.receipt(call, result)
        mapped = query_outcome(result, receipt)
        if mapped is not None:
            return mapped
        try:
            envelope = json_object(result.body)
            values = envelope.get("value")
            if not isinstance(values, list):
                raise ValueError("response has no value list")
            messages = tuple(message_summary(decode_graph_message(value)) for value in values[:50])
            next_link = string_value(envelope, "@odata.nextLink")
            if next_link:
                validate_next_page_link(next_link)
            page = SearchMessagesOutput(folder, messages, bool(next_link), next_link)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                SearchMessagesOutput(),
                receipt,
                failure(
                    "searchMessages",
                    FailureKind.PROTOCOL,
                    "Microsoft Graph returned an invalid message page: " + str(error),
                ),
            )
        return QueryAttempt.branch_result("searched", page, receipt)


class GetMessageOperation:
    """Read one full message and its attachment metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return stable getMessage routes and defaults."""
        return GET_MESSAGE_DEFINITION

    async def invoke(self, call: Call, input: GetMessageInput) -> QueryAttempt[Message]:
        """Read without marking the message as read."""
        message_id = input.message_id.strip()
        try:
            validate_graph_id("messageId", message_id)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                Message(flag_status=""),
                Receipt(),
                failure("getMessage", FailureKind.VALIDATION, str(error)),
            )
        session, failed = await self.client.open_session(call, "getMessage")
        if failed is not None:
            return query_outcome(failed, Receipt(), not_found="notFound")  # type: ignore[return-value]
        assert session is not None
        path = "/messages/" + path_escape(message_id)
        result = await session.exchange(
            "GET",
            path,
            (("$select", ",".join((*SUMMARY_PROPERTIES, "bccRecipients", "body", "webLink"))),),
            prefers_text_body=True,
        )
        receipt = self.client.receipt(call, result, message_id)
        mapped = query_outcome(result, receipt, not_found="notFound")
        if mapped is not None:
            return mapped
        try:
            message = decode_full_message(result.body)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                Message(flag_status=""),
                receipt,
                failure(
                    "getMessage",
                    FailureKind.PROTOCOL,
                    "Microsoft Graph returned an invalid message: " + str(error),
                ),
            )
        if not message.has_attachments:
            return QueryAttempt.branch_result("found", message, receipt)
        listed = await session.exchange(
            "GET", path + "/attachments", (("$select", "id,name,contentType,size,isInline"),)
        )
        receipt = self.client.receipt(call, listed, message_id)
        mapped = query_outcome(listed, receipt, not_found="notFound")
        if mapped is not None:
            return mapped
        try:
            attachments, is_truncated = decode_attachment_list(listed.body)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                Message(flag_status=""),
                receipt,
                failure(
                    "getMessage",
                    FailureKind.PROTOCOL,
                    "Microsoft Graph returned an invalid attachment list: " + str(error),
                ),
            )
        return QueryAttempt.branch_result(
            "found",
            replace(message, attachments=attachments, is_attachment_list_truncated=is_truncated),
            receipt,
        )


class MoveMessageOperation:
    """Move one message after confirming the destination folder."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return stable moveMessage routes and defaults."""
        return MOVE_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: MoveMessageInput) -> IdempotencyKey:
        """Reuse the stable Dex Call ID for every attempt."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: MoveMessageInput) -> MutationAttempt[MovedMessage]:
        """Read first, then move only when the folder differs."""
        message_id = input.message_id.strip()
        try:
            destination = resolve_folder("destinationFolder", input.destination_folder)
            validate_graph_id("messageId", message_id)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                MovedMessage(),
                Receipt(),
                failure("moveMessage", FailureKind.VALIDATION, str(error)),
            )
        session, failed = await self.client.open_session(call, "moveMessage")
        if failed is not None:
            return update_outcome(failed, Receipt())  # type: ignore[return-value]
        assert session is not None
        path = "/messages/" + path_escape(message_id)
        read = await session.exchange("GET", path, (("$select", "id,parentFolderId"),))
        mapped = update_outcome(read, self.client.receipt(call, read, message_id))
        if mapped is not None:
            return mapped
        try:
            message = decode_graph_message(read.body)
        except ValueError as error:
            return MutationAttempt.retry(
                failure(
                    "moveMessage",
                    FailureKind.PROTOCOL,
                    "Microsoft Graph returned an invalid message: " + str(error),
                )
            )
        folder_result = await session.exchange(
            "GET", "/mailFolders/" + path_escape(destination), (("$select", "id,displayName"),)
        )
        folder_receipt = self.client.receipt(call, folder_result, message_id)
        if folder_result.outcome == "not_found":
            return MutationAttempt.branch_result(
                "providerRejected",
                MovedMessage(),
                folder_receipt,
                failure(
                    "moveMessage",
                    FailureKind.NOT_FOUND,
                    "the destination folder does not exist in the mailbox; the connector never creates folders",  # noqa: E501
                ),
            )
        mapped = update_outcome(folder_result, folder_receipt)
        if mapped is not None:
            return mapped
        try:
            folder = json_object(folder_result.body)
            folder_id = string_value(folder, "id")
            validate_graph_id("folder id", folder_id)
            folder_name = string_value(folder, "displayName")
        except ValueError:
            return MutationAttempt.retry(
                failure(
                    "moveMessage",
                    FailureKind.PROTOCOL,
                    "Microsoft Graph returned a mail folder without a usable id",
                )
            )
        if string_value(message, "parentFolderId") == folder_id:
            return MutationAttempt.branch_result(
                "moved",
                MovedMessage(
                    message_id=string_value(message, "id"),
                    destination_folder_id=folder_id,
                    destination_folder_name=folder_name,
                    was_already_moved=True,
                ),
                self.client.receipt(call, read, string_value(message, "id")),
            )
        moved = await session.exchange("POST", path + "/move", payload={"destinationId": folder_id})
        receipt = self.client.receipt(call, moved, message_id)
        if moved.outcome == "not_found":
            assert moved.problem is not None
            return MutationAttempt.retry(moved.problem)
        mapped = update_outcome(moved, receipt)
        if mapped is not None:
            return mapped
        try:
            moved_message = decode_graph_message(moved.body)
        except ValueError as error:
            return MutationAttempt.retry(
                failure(
                    "moveMessage",
                    FailureKind.PROTOCOL,
                    "Microsoft Graph returned an invalid moved message: " + str(error),
                )
            )
        return MutationAttempt.branch_result(
            "moved",
            MovedMessage(
                message_id=string_value(moved_message, "id"),
                destination_folder_id=folder_id,
                destination_folder_name=folder_name,
            ),
            self.client.receipt(call, moved, string_value(moved_message, "id")),
        )


class SetMessageFlagsOperation:
    """Set absolute read, flag, and category state after a fresh read."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return stable setMessageFlags routes and defaults."""
        return SET_MESSAGE_FLAGS_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SetMessageFlagsInput) -> IdempotencyKey:
        """Reuse the stable Dex Call ID for every attempt."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: SetMessageFlagsInput) -> MutationAttempt[MessageFlags]:
        """Patch only values that differ from Graph's current message."""
        message_id = input.message_id.strip()
        try:
            validate_graph_id("messageId", message_id)
            if input.is_read is None and input.flag_status is None and input.categories is None:
                raise ValueError("set at least one of isRead, flagStatus, and categories")
            if input.flag_status is not None and input.flag_status not in (
                "notFlagged",
                "flagged",
                "complete",
            ):
                raise ValueError("flagStatus must be notFlagged, flagged, or complete")
            if input.categories is not None:
                if len(input.categories) > 25:
                    raise ValueError("categories can hold at most 25 names")
                seen: set[str] = set()
                for category in input.categories:
                    if category != category.strip() or not category:
                        raise ValueError("categories must be names without surrounding spaces")
                    validate_single_line("categories", category, 255)
                    if category.lower() in seen:
                        raise ValueError("categories must not repeat a name")
                    seen.add(category.lower())
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                MessageFlags(flag_status=""),
                Receipt(),
                failure("setMessageFlags", FailureKind.VALIDATION, str(error)),
            )
        session, failed = await self.client.open_session(call, "setMessageFlags")
        if failed is not None:
            return update_outcome(failed, Receipt())  # type: ignore[return-value]
        assert session is not None
        path = "/messages/" + path_escape(message_id)
        read = await session.exchange("GET", path, (("$select", "id,isRead,flag,categories"),))
        mapped = update_outcome(read, self.client.receipt(call, read, message_id))
        if mapped is not None:
            return mapped
        try:
            message = decode_graph_message(read.body)
        except ValueError as error:
            return MutationAttempt.retry(
                failure(
                    "setMessageFlags",
                    FailureKind.PROTOCOL,
                    "Microsoft Graph returned an invalid message: " + str(error),
                )
            )
        current = message_summary(message)
        flags = MessageFlags(
            message_id=string_value(message, "id"),
            is_read=current.is_read,
            flag_status=current.flag_status,
            categories=current.categories,
        )
        changes: dict[str, Any] = {}
        if input.is_read is not None and input.is_read != flags.is_read:
            changes["isRead"] = input.is_read
            flags = replace(flags, is_read=input.is_read)
        if input.flag_status is not None and input.flag_status != flags.flag_status:
            changes["flag"] = {"flagStatus": input.flag_status}
            flags = replace(flags, flag_status=input.flag_status)
        if input.categories is not None and (
            len(input.categories) != len(flags.categories)
            or {item.lower() for item in input.categories} != {item.lower() for item in flags.categories}
        ):
            changes["categories"] = list(input.categories)
            flags = replace(flags, categories=input.categories)
        if not changes:
            return MutationAttempt.branch_result(
                "updated",
                replace(flags, was_already_applied=True),
                self.client.receipt(call, read, string_value(message, "id")),
            )
        updated = await session.exchange("PATCH", path, payload=changes)
        receipt = self.client.receipt(call, updated, string_value(message, "id"))
        mapped = update_outcome(updated, receipt)
        return mapped if mapped is not None else MutationAttempt.branch_result("updated", flags, receipt)


class SendMessageOperation:
    """Send a new message through a marked, checkpointed Outlook draft."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return stable sendMessage routes and defaults."""
        return SEND_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendMessageInput) -> IdempotencyKey:
        """Use the Dex Call ID as the draft marker key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: SendMessageInput) -> MutationAttempt[SentMessage]:
        """Validate and run the durable draft send path."""
        from .draft_send import send_message

        return await send_message(self.client, call, input)


class ReplyToMessageOperation:
    """Send a threaded reply through a marked, checkpointed Outlook draft."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return stable replyToMessage routes and defaults."""
        return REPLY_TO_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ReplyToMessageInput) -> IdempotencyKey:
        """Use the Dex Call ID as the reply draft marker key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: ReplyToMessageInput) -> MutationAttempt[SentMessage]:
        """Validate and run the durable reply send path."""
        from .draft_send import reply_to_message

        return await reply_to_message(self.client, call, input)
