# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Checkpointed Outlook draft sends with marker lookup and read-back recovery."""

import re
from dataclasses import dataclass, field
from typing import Any

from dex_connectors.sdk import Call, FailureKind, MutationAttempt, Receipt

from .client import Client, Exchange, Session, failure, path_escape, validate_graph_id
from .message import (
    DRAFT_PROPERTIES,
    address_list,
    decode_graph_message,
    json_object,
    string_value,
    timestamp_value,
)
from .models import ReplyToMessageInput, SendMessageInput, SentMessage
from .operations import quote_odata, validate_address, validate_single_line

MARKER_PROPERTY_ID = "String {194d4363-ceb1-4554-8c26-1077f4961682} Name DexStepIdempotencyKey"
MARKER_PREFIX = "dex-"
MARKER_KEY_PATTERN = re.compile(r"^[A-Za-z0-9._:-]{1,128}$")
SEND_CONFIRMATION_DELAY = 3.0
MAXIMUM_SEND_CONFIRMATIONS = 3


@dataclass(frozen=True, slots=True)
class Checkpoint:
    """The connector-owned Dex heartbeat state for a draft send."""

    phase: str = field(metadata={"dex_json_name": "outlookMailSendPhase"})
    draft_message_id: str = field(metadata={"dex_json_name": "outlookMailDraftMessageId"})
    confirmations: int = field(
        default=0,
        metadata={"dex_json_name": "outlookMailSendConfirmations", "dex_json_omit_default": True},
    )

    def to_wire(self) -> dict[str, object]:
        """Use the Go checkpoint's exact heartbeat keys."""
        result: dict[str, object] = {
            "outlookMailSendPhase": self.phase,
            "outlookMailDraftMessageId": self.draft_message_id,
        }
        if self.confirmations:
            result["outlookMailSendConfirmations"] = self.confirmations
        return result


async def _record(call: Call, checkpoint: Checkpoint) -> bool:
    """Record one checkpoint before an operation can change send state."""
    if call.context is None:
        return False
    try:
        await call.context.heartbeat(checkpoint)
        return True
    except Exception:
        return False


def _last_checkpoint(call: Call) -> Checkpoint | None:
    """Read a prior attempt's heartbeat, rejecting foreign checkpoint shapes."""
    if call.context is None or not call.context.has_last_heartbeat_value():
        return None
    value = call.context.get_last_heartbeat_value(Checkpoint)
    if not isinstance(value, Checkpoint):
        raise ValueError("the heartbeat checkpoint is not a send checkpoint")
    if value.phase not in ("draftReady", "sendDispatched") or value.confirmations < 0:
        raise ValueError("the heartbeat checkpoint is not a send checkpoint")
    return value


def _sent_message(resource: dict[str, Any], marker: str, already_sent: bool = False) -> SentMessage:
    """Return Graph's draft or sent-copy identity with its original recipients."""
    recipients = tuple(
        address.address
        for key in ("toRecipients", "ccRecipients", "bccRecipients")
        for address in address_list(resource, key, 1 << 20)
    )
    return SentMessage(
        message_id=string_value(resource, "id"),
        internet_message_id=string_value(resource, "internetMessageId"),
        conversation_id=string_value(resource, "conversationId"),
        subject=string_value(resource, "subject"),
        recipients=recipients,
        idempotency_marker=marker,
        was_already_sent=already_sent,
    )


def _pending(marker: str, draft_id: str = "") -> SentMessage:
    return SentMessage(message_id=draft_id, idempotency_marker=marker)


def _before_dispatch(
    result: Exchange, value: SentMessage, receipt: Receipt, has_not_found: bool
) -> MutationAttempt[SentMessage]:
    """Map a failure while no send request has left this attempt."""
    assert result.problem is not None
    if result.outcome in ("retry", "ambiguous", "invalid"):
        return MutationAttempt.retry(result.problem, result.retry_after)
    if result.outcome == "not_found" and has_not_found:
        return MutationAttempt.branch_result("notFound", value, receipt, result.problem)
    if result.outcome == "defect":
        return MutationAttempt.branch_result("defect", value, receipt, result.problem)
    if result.outcome == "not_found":
        return MutationAttempt.branch_result("providerRejected", value, receipt, result.problem)
    return MutationAttempt.branch_result(
        "providerRejected",
        value,
        receipt,
        failure(result.problem.operation, result.problem.kind, result.problem.message + "; nothing was sent"),
    )


async def _read_draft(session: Session, draft_id: str) -> tuple[dict[str, Any] | None, Exchange]:
    """Read one immutable draft ID or its Sent Items copy."""
    result = await session.exchange(
        "GET", "/messages/" + path_escape(draft_id), (("$select", ",".join(DRAFT_PROPERTIES)),)
    )
    if result.outcome != "succeeded":
        return None, result
    try:
        return decode_graph_message(result.body), result
    except ValueError as error:
        return None, Exchange(
            "invalid",
            result.status_code,
            request_id=result.request_id,
            problem=failure(
                session.operation,
                FailureKind.PROTOCOL,
                "Microsoft Graph returned an invalid answer: the draft read returned " + str(error),
            ),
        )


async def _find_marked_draft(session: Session, marker: str) -> tuple[dict[str, Any] | None, Exchange]:
    """Prefer a sent copy, then the earliest created draft carrying the marker."""
    filter_value = (
        "singleValueExtendedProperties/Any(ep: ep/id eq "
        + quote_odata(MARKER_PROPERTY_ID)
        + " and ep/value eq "
        + quote_odata(marker)
        + ")"
    )
    result = await session.exchange(
        "GET",
        "/messages",
        (("$filter", filter_value), ("$select", ",".join(DRAFT_PROPERTIES)), ("$top", "10")),
    )
    if result.outcome != "succeeded":
        return None, result
    try:
        envelope = json_object(result.body)
        values = envelope.get("value")
        if not isinstance(values, list):
            raise ValueError("the draft lookup has no value list")
        matches = [decode_graph_message(item) for item in values[:10]]
    except ValueError as error:
        return None, Exchange(
            "invalid",
            result.status_code,
            request_id=result.request_id,
            problem=failure(
                session.operation,
                FailureKind.PROTOCOL,
                "Microsoft Graph returned an invalid answer: the draft lookup returned " + str(error),
            ),
        )
    if not matches:
        return None, result
    matches.sort(
        key=lambda item: (
            bool(item.get("isDraft", False)),
            timestamp_value(item, "createdDateTime") or "9999-12-31T23:59:59Z",
            string_value(item, "id"),
        )
    )
    return matches[0], result


async def _confirm_dispatched(
    session: Session, marker: str, checkpoint: Checkpoint
) -> MutationAttempt[SentMessage]:
    """Read a dispatched draft without sending it again."""
    pending = _pending(marker, checkpoint.draft_message_id)
    try:
        validate_graph_id("draft message id", checkpoint.draft_message_id)
    except ValueError:
        return MutationAttempt.uncertain(
            pending,
            failure(
                session.operation,
                FailureKind.LOCAL_DEFECT,
                "an earlier attempt dispatched the send without naming its draft, so the message may have been sent; it is not sent again",  # noqa: E501
            ),
            Receipt(),
        )
    draft, result = await _read_draft(session, checkpoint.draft_message_id)
    receipt = session.client.receipt(session.call, result, checkpoint.draft_message_id)
    if result.outcome == "succeeded" and draft is not None and not draft.get("isDraft", False):
        return MutationAttempt.branch_result("sent", _sent_message(draft, marker, True), receipt)
    if result.outcome == "succeeded" and draft is not None:
        pending = _sent_message(draft, marker)
    if checkpoint.confirmations + 1 < MAXIMUM_SEND_CONFIRMATIONS:
        if await _record(
            session.call,
            Checkpoint("sendDispatched", checkpoint.draft_message_id, checkpoint.confirmations + 1),
        ):
            return MutationAttempt.retry(
                failure(
                    session.operation,
                    FailureKind.AVAILABILITY,
                    "the send was dispatched but its answer was lost; the draft has not appeared as sent yet, so a later attempt reads it again",  # noqa: E501
                ),
                SEND_CONFIRMATION_DELAY,
            )
    return MutationAttempt.uncertain(
        pending,
        failure(
            session.operation,
            FailureKind.TRANSPORT,
            "the send was dispatched but its answer was lost and the draft has not appeared as sent, so the message may have been sent; it is not sent again",  # noqa: E501
        ),
        receipt,
    )


async def _dispatch(session: Session, marker: str, draft: dict[str, Any]) -> MutationAttempt[SentMessage]:
    """Persist dispatch before POST /send; clear only after a conclusive refusal."""
    draft_id = string_value(draft, "id")
    message = _sent_message(draft, marker)
    if not await _record(session.call, Checkpoint("sendDispatched", draft_id)):
        return MutationAttempt.retry(
            failure(
                session.operation,
                FailureKind.AVAILABILITY,
                "Dex did not record the send checkpoint; nothing was sent",
            )
        )
    result = await session.exchange("POST", "/messages/" + path_escape(draft_id) + "/send")
    receipt = session.client.receipt(session.call, result, draft_id)
    if result.outcome in ("succeeded", "invalid"):
        return MutationAttempt.branch_result("sent", message, receipt)
    assert result.problem is not None
    if result.outcome == "retry":
        await _record(session.call, Checkpoint("draftReady", draft_id))
        return MutationAttempt.retry(result.problem, result.retry_after)
    if result.outcome in ("rejected", "not_found", "defect"):
        await _record(session.call, Checkpoint("draftReady", draft_id))
        return MutationAttempt.branch_result(
            "providerRejected",
            message,
            receipt,
            failure(
                session.operation,
                result.problem.kind,
                result.problem.message + "; nothing was sent and the draft stays in Drafts",
            ),
        )
    return MutationAttempt.retry(
        failure(
            session.operation,
            result.problem.kind,
            result.problem.message
            + "; the send was dispatched, so a later attempt confirms it from the draft instead of sending again",  # noqa: E501
        ),
        SEND_CONFIRMATION_DELAY,
    )


async def _create_draft(
    session: Session, input: SendMessageInput | ReplyToMessageInput, marker: str
) -> tuple[dict[str, Any] | None, Exchange]:
    """Create a new message draft or Outlook's reply draft."""
    if isinstance(input, SendMessageInput):
        payload: dict[str, Any] = {
            "subject": input.subject,
            "body": {"contentType": "text", "content": input.text},
            "toRecipients": [{"emailAddress": {"address": address, "name": ""}} for address in input.to],
            "singleValueExtendedProperties": [{"id": MARKER_PROPERTY_ID, "value": marker}],
        }
        for key, recipients in (
            ("ccRecipients", input.cc),
            ("bccRecipients", input.bcc),
            ("replyTo", input.reply_to),
        ):
            if recipients:
                payload[key] = [{"emailAddress": {"address": address, "name": ""}} for address in recipients]
        result = await session.exchange("POST", "/messages", payload=payload)
    else:
        action = "/createReplyAll" if input.is_reply_all else "/createReply"
        result = await session.exchange(
            "POST",
            "/messages/" + path_escape(input.message_id.strip()) + action,
            payload={"comment": input.text},
        )
        if result.outcome == "invalid":
            return None, Exchange(
                "rejected",
                result.status_code,
                request_id=result.request_id,
                problem=failure(
                    session.operation,
                    FailureKind.PROTOCOL,
                    "Microsoft Graph created a reply draft but its answer is unusable; the unsent draft stays in Drafts",  # noqa: E501
                ),
            )
    if result.outcome != "succeeded":
        return None, result
    try:
        draft = decode_graph_message(result.body)
    except ValueError as error:
        if isinstance(input, ReplyToMessageInput):
            return None, Exchange(
                "rejected",
                result.status_code,
                request_id=result.request_id,
                problem=failure(
                    session.operation,
                    FailureKind.PROTOCOL,
                    "Microsoft Graph created a reply draft but its answer has no usable draft; the unsent draft stays in Drafts",  # noqa: E501
                ),
            )
        return None, Exchange(
            "invalid",
            result.status_code,
            request_id=result.request_id,
            problem=failure(
                session.operation,
                FailureKind.PROTOCOL,
                "Microsoft Graph returned an invalid answer: the created draft " + str(error),
            ),
        )
    if isinstance(input, ReplyToMessageInput) and not _sent_message(draft, marker).recipients:
        return None, Exchange(
            "rejected",
            result.status_code,
            request_id=result.request_id,
            problem=failure(
                session.operation,
                FailureKind.PROTOCOL,
                "Microsoft Graph created a reply draft but the original has no address to reply to; the unsent draft stays in Drafts",  # noqa: E501
            ),
        )
    return draft, result


async def _find_create_send(
    session: Session, input: SendMessageInput | ReplyToMessageInput, marker: str
) -> MutationAttempt[SentMessage]:
    """Find the marker before creating or dispatching any draft."""
    existing, found = await _find_marked_draft(session, marker)
    if found.outcome != "succeeded":
        return _before_dispatch(
            found,
            _pending(marker),
            session.client.receipt(session.call, found),
            isinstance(input, ReplyToMessageInput),
        )
    if existing is not None:
        if not existing.get("isDraft", False):
            return MutationAttempt.branch_result(
                "sent",
                _sent_message(existing, marker, True),
                session.client.receipt(session.call, found, string_value(existing, "id")),
            )
        return await _dispatch(session, marker, existing)
    draft, created = await _create_draft(session, input, marker)
    if created.outcome != "succeeded" or draft is None:
        return _before_dispatch(
            created,
            _pending(marker),
            session.client.receipt(session.call, created),
            isinstance(input, ReplyToMessageInput),
        )
    draft_id = string_value(draft, "id")
    await _record(session.call, Checkpoint("draftReady", draft_id))
    if isinstance(input, ReplyToMessageInput):
        marked = await session.exchange(
            "PATCH",
            "/messages/" + path_escape(draft_id),
            payload={"singleValueExtendedProperties": [{"id": MARKER_PROPERTY_ID, "value": marker}]},
        )
        if marked.outcome == "not_found":
            assert marked.problem is not None
            return MutationAttempt.retry(marked.problem)
        if marked.outcome != "succeeded":
            return _before_dispatch(
                marked,
                _sent_message(draft, marker),
                session.client.receipt(session.call, marked, draft_id),
                True,
            )
    return await _dispatch(session, marker, draft)


async def _send_draft_once(
    client: Client, call: Call, input: SendMessageInput | ReplyToMessageInput, operation: str
) -> MutationAttempt[SentMessage]:
    """Resume, find, create, or confirm one operation's marked draft."""
    key = str(call.idempotency_key)
    if not MARKER_KEY_PATTERN.fullmatch(key):
        return MutationAttempt.branch_result(
            "defect",
            SentMessage(),
            Receipt(),
            failure(
                operation,
                FailureKind.LOCAL_DEFECT,
                "the Step has no usable idempotency key for the draft marker",
            ),
        )
    marker = MARKER_PREFIX + key
    try:
        checkpoint = _last_checkpoint(call)
    except ValueError:
        return MutationAttempt.uncertain(
            _pending(marker),
            failure(
                operation,
                FailureKind.LOCAL_DEFECT,
                "the send checkpoint of an earlier attempt is unreadable, so the message may have been sent; it is not sent again",  # noqa: E501
            ),
            Receipt(),
        )
    session, failed = await client.open_session(call, operation)
    if failed is not None:
        return _before_dispatch(failed, _pending(marker), Receipt(), isinstance(input, ReplyToMessageInput))
    assert session is not None
    if checkpoint is not None and checkpoint.phase == "sendDispatched":
        return await _confirm_dispatched(session, marker, checkpoint)
    if checkpoint is not None and checkpoint.phase == "draftReady":
        try:
            validate_graph_id("draft message id", checkpoint.draft_message_id)
        except ValueError:
            return await _find_create_send(session, input, marker)
        draft, read = await _read_draft(session, checkpoint.draft_message_id)
        if read.outcome == "not_found":
            return await _find_create_send(session, input, marker)
        if read.outcome != "succeeded":
            return _before_dispatch(
                read,
                _pending(marker, checkpoint.draft_message_id),
                client.receipt(call, read, checkpoint.draft_message_id),
                isinstance(input, ReplyToMessageInput),
            )
        assert draft is not None
        if not draft.get("isDraft", False):
            return MutationAttempt.branch_result(
                "sent",
                _sent_message(draft, marker, True),
                client.receipt(call, read, string_value(draft, "id")),
            )
        return await _dispatch(session, marker, draft)
    return await _find_create_send(session, input, marker)


async def send_message(client: Client, call: Call, input: SendMessageInput) -> MutationAttempt[SentMessage]:
    """Validate and send one new plain text message."""
    try:
        if not input.to:
            raise ValueError("to needs at least one recipient")
        if len(input.to) + len(input.cc) + len(input.bcc) > 500:
            raise ValueError("to, cc, and bcc can hold at most 500 recipients together")
        for key, recipients in (
            ("to", input.to),
            ("cc", input.cc),
            ("bcc", input.bcc),
            ("replyTo", input.reply_to),
        ):
            for address in recipients:
                validate_address(key, address)
        if not input.subject.strip():
            raise ValueError("subject is required")
        validate_single_line("subject", input.subject, 255)
        _validate_send_text(input.text)
    except ValueError as error:
        return MutationAttempt.branch_result(
            "defect", SentMessage(), Receipt(), failure("sendMessage", FailureKind.VALIDATION, str(error))
        )
    return await _send_draft_once(client, call, input, "sendMessage")


async def reply_to_message(
    client: Client, call: Call, input: ReplyToMessageInput
) -> MutationAttempt[SentMessage]:
    """Validate and send one Outlook threaded reply."""
    try:
        validate_graph_id("messageId", input.message_id.strip())
        _validate_send_text(input.text)
    except ValueError as error:
        return MutationAttempt.branch_result(
            "defect", SentMessage(), Receipt(), failure("replyToMessage", FailureKind.VALIDATION, str(error))
        )
    return await _send_draft_once(client, call, input, "replyToMessage")


def _validate_send_text(text: str) -> None:
    if not text.strip():
        raise ValueError("text is required")
    if len(text.encode()) > 512 << 10:
        raise ValueError("text can be at most 524288 bytes")
