# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Checkpointed Outlook draft sends with marker lookup and read-back recovery."""

import re
from dataclasses import dataclass, field
from datetime import UTC, datetime
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
from .operations import DEFINITE_REFUSAL_STATUSES, quote_odata, validate_address, validate_single_line

MARKER_PROPERTY_ID = "String {194d4363-ceb1-4554-8c26-1077f4961682} Name DexStepIdempotencyKey"
MARKER_PREFIX = "dex-"
MARKER_KEY_PATTERN = re.compile(r"^[A-Za-z0-9._:-]{1,128}$")
SEND_CONFIRMATION_DELAY = 3.0


def _draft_created_key(item: dict[str, Any]) -> tuple[datetime, int]:
    timestamp = timestamp_value(item, "createdDateTime")
    if timestamp is None:
        return datetime.max.replace(tzinfo=UTC), 0
    fraction = re.search(r"\.(\d+)(?=Z|[+-]\d{2}:\d{2}$)", timestamp)
    nanoseconds = int((fraction.group(1) if fraction else "").ljust(9, "0"))
    instant = datetime.fromisoformat(timestamp.replace("Z", "+00:00")).astimezone(UTC)
    return instant, nanoseconds % 1000


@dataclass(frozen=True, slots=True)
class Checkpoint:
    """The connector-owned Dex heartbeat state for a draft send."""

    phase: str = field(metadata={"dex_json_name": "outlookMailSendPhase"})
    draft_message_id: str = field(metadata={"dex_json_name": "outlookMailDraftMessageId"})
    # Written only by releases before 0.1.1; it is read and ignored.
    confirmations: int = field(
        default=0,
        metadata={"dex_json_name": "outlookMailSendConfirmations", "dex_json_omit_default": True},
    )
    # Possibly effective sends: a dispatched checkpoint includes its own send, draftReady only earlier ones.
    send_count: int = field(
        default=0,
        metadata={"dex_json_name": "outlookMailSendCount", "dex_json_omit_default": True},
    )

    def to_wire(self) -> dict[str, object]:
        """Use the Go checkpoint's exact heartbeat keys."""
        result: dict[str, object] = {
            "outlookMailSendPhase": self.phase,
            "outlookMailDraftMessageId": self.draft_message_id,
        }
        if self.confirmations:
            result["outlookMailSendConfirmations"] = self.confirmations
        if self.send_count:
            result["outlookMailSendCount"] = self.send_count
        return result

    def prior_send_count(self) -> int:
        """Bound the sends this Step execution may have made; a legacy dispatch counts as one."""
        if self.phase == "sendDispatched":
            return max(self.send_count, 1)
        return max(self.send_count, 0)


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
    if value.phase not in ("draftReady", "sendDispatched") or value.confirmations < 0 or value.send_count < 0:
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
    result: Exchange, value: SentMessage, receipt: Receipt, has_not_found: bool, prior_send_count: int
) -> MutationAttempt[SentMessage]:
    """Map a failure before this attempt dispatched a send.

    A terminal answer selects uncertain when an earlier attempt may have sent the draft.
    """
    assert result.problem is not None
    if result.outcome in ("retry", "ambiguous", "invalid"):
        return MutationAttempt.retry(result.problem, result.retry_after)
    if prior_send_count > 0:
        return MutationAttempt.uncertain(
            value,
            failure(
                result.problem.operation,
                result.problem.kind,
                result.problem.message + "; an earlier send may have delivered the message, "
                "and it cannot be confirmed or sent again",
            ),
            receipt,
        )
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
            _draft_created_key(item),
            string_value(item, "id"),
        )
    )
    return matches[0], result


async def _confirm_dispatched(
    session: Session,
    input: SendMessageInput | ReplyToMessageInput,
    marker: str,
    checkpoint: Checkpoint,
    prior_send_count: int,
) -> MutationAttempt[SentMessage]:
    """Read a dispatched draft back and send a still-unsent draft again while the cap allows."""
    pending = _pending(marker, checkpoint.draft_message_id)
    try:
        validate_graph_id("draft message id", checkpoint.draft_message_id)
    except ValueError:
        # The marker lookup reads the message back without the draft ID.
        return await _find_create_send(session, input, marker, prior_send_count)
    draft, result = await _read_draft(session, checkpoint.draft_message_id)
    receipt = session.client.receipt(session.call, result, checkpoint.draft_message_id)
    if result.outcome == "succeeded" and draft is not None:
        if not draft.get("isDraft", False):
            return MutationAttempt.branch_result("sent", _sent_message(draft, marker, True), receipt)
        return await _dispatch(session, marker, draft, prior_send_count)
    if result.outcome == "not_found":
        return await _find_create_send(session, input, marker, prior_send_count)
    assert result.problem is not None
    if result.outcome in ("retry", "ambiguous", "invalid"):
        return MutationAttempt.retry(result.problem, result.retry_after)
    return MutationAttempt.uncertain(
        pending,
        failure(
            session.operation,
            result.problem.kind,
            result.problem.message
            + "; the dispatched draft could not be read back, so the message may have been sent; it is not sent again",  # noqa: E501
        ),
        receipt,
    )


def _is_unprocessed_send_answer(result: Exchange) -> bool:
    """Report a 429, a 503, or a connection that never reached Graph."""
    if result.status_code == 0:
        return result.outcome == "retry"
    return result.status_code in (429, 503)


def _is_definite_send_refusal(result: Exchange) -> bool:
    """Report a definite refusal status, or a local request defect before sending."""
    if result.status_code == 0:
        return result.outcome == "defect"
    return result.status_code in DEFINITE_REFUSAL_STATUSES


async def _dispatch(
    session: Session, marker: str, draft: dict[str, Any], prior_send_count: int
) -> MutationAttempt[SentMessage]:
    """Persist dispatch before POST /send while the send cap allows another send.

    Only an answer proving Graph did not process the send releases the checkpoint.
    """
    draft_id = string_value(draft, "id")
    message = _sent_message(draft, marker)
    max_sends = session.call.max_sends
    if prior_send_count >= max_sends:
        return MutationAttempt.uncertain(
            message,
            failure(
                session.operation,
                FailureKind.TRANSPORT,
                f"an earlier send of the draft was not confirmed and the send cap of {max_sends} is reached, so the message may have been sent; it is not sent again",  # noqa: E501
            ),
            session.client.receipt(session.call, Exchange("succeeded"), draft_id),
        )
    dispatched = Checkpoint("sendDispatched", draft_id, send_count=prior_send_count + 1)
    if not await _record(session.call, dispatched):
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
    if _is_unprocessed_send_answer(result):
        await _record(session.call, Checkpoint("draftReady", draft_id, send_count=prior_send_count))
        return MutationAttempt.retry(result.problem, result.retry_after)
    if _is_definite_send_refusal(result) and prior_send_count == 0:
        await _record(session.call, Checkpoint("draftReady", draft_id, send_count=prior_send_count))
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
    if _is_definite_send_refusal(result):
        # An earlier send may have delivered the draft, so the next attempt reads it back.
        return MutationAttempt.retry(
            failure(
                session.operation,
                result.problem.kind,
                result.problem.message
                + "; an earlier send may have delivered the draft, so a later attempt reads it back",
            ),
            SEND_CONFIRMATION_DELAY,
        )
    return MutationAttempt.retry(
        failure(
            session.operation,
            result.problem.kind,
            result.problem.message
            + "; the send was dispatched, so a later attempt reads the draft back before sending again",
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
    session: Session, input: SendMessageInput | ReplyToMessageInput, marker: str, prior_send_count: int
) -> MutationAttempt[SentMessage]:
    """Find the marker before creating or dispatching any draft."""
    existing, found = await _find_marked_draft(session, marker)
    if found.outcome != "succeeded":
        return _before_dispatch(
            found,
            _pending(marker),
            session.client.receipt(session.call, found),
            isinstance(input, ReplyToMessageInput),
            prior_send_count,
        )
    if existing is not None:
        if not existing.get("isDraft", False):
            return MutationAttempt.branch_result(
                "sent",
                _sent_message(existing, marker, True),
                session.client.receipt(session.call, found, string_value(existing, "id")),
            )
        return await _dispatch(session, marker, existing, prior_send_count)
    draft, created = await _create_draft(session, input, marker)
    if created.outcome != "succeeded" or draft is None:
        return _before_dispatch(
            created,
            _pending(marker),
            session.client.receipt(session.call, created),
            isinstance(input, ReplyToMessageInput),
            prior_send_count,
        )
    draft_id = string_value(draft, "id")
    await _record(session.call, Checkpoint("draftReady", draft_id, send_count=prior_send_count))
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
                prior_send_count,
            )
    return await _dispatch(session, marker, draft, prior_send_count)


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
        return MutationAttempt.retry(
            failure(
                operation,
                FailureKind.AVAILABILITY,
                "the send checkpoint of an earlier attempt is unreadable; nothing is sent in this attempt",
            )
        )
    prior_send_count = checkpoint.prior_send_count() if checkpoint is not None else 0
    session, failed = await client.open_session(call, operation)
    if failed is not None:
        return _before_dispatch(
            failed, _pending(marker), Receipt(), isinstance(input, ReplyToMessageInput), prior_send_count
        )
    assert session is not None
    if checkpoint is not None and checkpoint.phase == "sendDispatched":
        return await _confirm_dispatched(session, input, marker, checkpoint, prior_send_count)
    if checkpoint is not None and checkpoint.phase == "draftReady":
        try:
            validate_graph_id("draft message id", checkpoint.draft_message_id)
        except ValueError:
            return await _find_create_send(session, input, marker, prior_send_count)
        draft, read = await _read_draft(session, checkpoint.draft_message_id)
        if read.outcome == "not_found":
            return await _find_create_send(session, input, marker, prior_send_count)
        if read.outcome != "succeeded":
            return _before_dispatch(
                read,
                _pending(marker, checkpoint.draft_message_id),
                client.receipt(call, read, checkpoint.draft_message_id),
                isinstance(input, ReplyToMessageInput),
                prior_send_count,
            )
        assert draft is not None
        if not draft.get("isDraft", False):
            return MutationAttempt.branch_result(
                "sent",
                _sent_message(draft, marker, True),
                client.receipt(call, read, string_value(draft, "id")),
            )
        return await _dispatch(session, marker, draft, prior_send_count)
    return await _find_create_send(session, input, marker, prior_send_count)


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
