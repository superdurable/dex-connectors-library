# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-equivalent scripted Gmail outcomes for application tests."""

from datetime import UTC, datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection
from ..definitions import (
    GET_MESSAGE_DEFINITION,
    PROVIDER,
    REPLY_TO_MESSAGE_DEFINITION,
    SEND_MESSAGE_DEFINITION,
)
from ..models import GetMessageInput, Message, ReplyToMessageInput, SendMessageInput, SendMessageOutput


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "gmail", operation, message)


class Mock:
    """Three scripted Gmail operations with the manifest's happy-path defaults."""

    def __init__(self, connection_name: str) -> None:
        if not connection_name.strip():
            raise ValueError("Gmail mock connection name is required")
        self.reference = ConnectionRef(PROVIDER, connection_name)
        self.get_message: Query[GetMessageInput, Message] = Query(GET_MESSAGE_DEFINITION)
        self.send_message: Mutation[SendMessageInput, SendMessageOutput] = Mutation(SEND_MESSAGE_DEFINITION)
        self.reply_to_message: Mutation[ReplyToMessageInput, SendMessageOutput] = Mutation(
            REPLY_TO_MESSAGE_DEFINITION
        )
        self.get_message.default(get_message_customer_question())
        self.send_message.default(send_message_sent_to_customer())
        self.reply_to_message.default(reply_to_message_replied_in_thread())
        self.connection = Connection(
            self.reference, self.get_message, self.send_message, self.reply_to_message
        )


def get_message_customer_question() -> Case[Message]:
    """Return the manifest's plain-text and HTML customer question."""
    return Case.branch_result(
        "read",
        Message(
            message_id="18c0a1b2c3d4e5f6",
            thread_id="18c0a1b2c3d4e5f0",
            rfc_message_id="<question-1@mail.example.com>",
            in_reply_to="<welcome-1@mail.example.com>",
            references="<welcome-1@mail.example.com>",
            from_address="Ada Customer <ada@example.com>",
            reply_to="ada.support@example.com",
            to=["support@example.org"],
            subject="Re: Order 1042 has not arrived",
            text_body="Hello, my order 1042 has not arrived yet. Can you check?",
            html_body="<p>Hello, my order 1042 has not arrived yet. Can you check?</p>",
            snippet="Hello, my order 1042 has not arrived yet.",
            received_at=datetime(2026, 3, 4, 9, 15, tzinfo=UTC),
            label_ids=["INBOX", "UNREAD"],
        ),
    )


def get_message_deleted_message() -> Case[Message]:
    """Select notFound for a deleted or missing Gmail message."""
    return Case.branch_result(
        "notFound",
        Message(),
        _failure("getMessage", FailureKind.NOT_FOUND, "The Gmail message does not exist."),
    )


def get_message_revoked_grant() -> Case[Message]:
    """Select providerRejected for a grant missing the required read scope."""
    return Case.branch_result(
        "providerRejected",
        Message(),
        _failure(
            "getMessage",
            FailureKind.AUTHORIZATION,
            "Gmail rejected the message query for the granted scopes.",
        ),
    )


def get_message_malformed_payload() -> Case[Message]:
    """Select invalidResponse for a malformed Gmail payload."""
    return Case.branch_result(
        "invalidResponse",
        Message(),
        _failure("getMessage", FailureKind.PROTOCOL, "Gmail returned an invalid message payload."),
    )


def get_message_empty_message_id() -> Case[Message]:
    """Select defect before a provider request for an empty message ID."""
    return Case.branch_result(
        "defect", Message(), _failure("getMessage", FailureKind.VALIDATION, "The message ID is required.")
    )


def get_message_rate_limited() -> Case[Message]:
    """Retry once after Gmail throttles the read."""
    return Case.retry(_failure("getMessage", FailureKind.RATE_LIMIT, "Gmail rate limit was reached."), 1)


def send_message_sent_to_customer() -> Case[SendMessageOutput]:
    """Return the manifest's accepted new message identity."""
    return Case.branch_result(
        "sent",
        SendMessageOutput(
            sender="support@example.org",
            recipients=["ada@example.com"],
            message_id="18c0a1b2c3d4e600",
            thread_id="18c0a1b2c3d4e600",
        ),
    )


def send_message_invalid_recipient() -> Case[SendMessageOutput]:
    """Select providerRejected for Gmail's invalid-recipient response."""
    return Case.branch_result(
        "providerRejected",
        SendMessageOutput(),
        _failure("sendMessage", FailureKind.PROVIDER_REJECTION, "Gmail rejected the message recipients."),
    )


def send_message_connection_lost() -> Case[SendMessageOutput]:
    """Retry an ambiguous send after Sent Mail did not confirm it."""
    return Case.retry(
        _failure("sendMessage", FailureKind.TRANSPORT, "The connection closed before Gmail answered.")
    )


def send_message_no_recipients() -> Case[SendMessageOutput]:
    """Select defect for a message with no recipients."""
    return Case.branch_result(
        "defect",
        SendMessageOutput(),
        _failure("sendMessage", FailureKind.VALIDATION, "At least one recipient is required."),
    )


def send_message_unavailable() -> Case[SendMessageOutput]:
    """Retry a temporarily unavailable Gmail send."""
    return Case.retry(_failure("sendMessage", FailureKind.AVAILABILITY, "Gmail is temporarily unavailable."))


def reply_to_message_replied_in_thread() -> Case[SendMessageOutput]:
    """Return the manifest's accepted reply identity."""
    return Case.branch_result(
        "sent",
        SendMessageOutput(
            sender="support@example.org",
            recipients=["ada.support@example.com"],
            message_id="18c0a1b2c3d4e700",
            thread_id="18c0a1b2c3d4e5f0",
        ),
    )


def reply_to_message_source_missing() -> Case[SendMessageOutput]:
    """Select providerRejected when the source Gmail message disappeared."""
    return Case.branch_result(
        "providerRejected",
        SendMessageOutput(),
        _failure("replyToMessage", FailureKind.NOT_FOUND, "The source Gmail message does not exist."),
    )


def reply_to_message_malformed_source() -> Case[SendMessageOutput]:
    """Select invalidResponse before sending from malformed source metadata."""
    return Case.branch_result(
        "invalidResponse",
        SendMessageOutput(),
        _failure("replyToMessage", FailureKind.PROTOCOL, "Gmail returned an invalid source message."),
    )


def reply_to_message_connection_lost() -> Case[SendMessageOutput]:
    """Retry an ambiguous reply after Sent Mail did not confirm it."""
    return Case.retry(
        _failure("replyToMessage", FailureKind.TRANSPORT, "The connection closed before Gmail answered.")
    )


def reply_to_message_empty_body() -> Case[SendMessageOutput]:
    """Select defect for a reply without a body."""
    return Case.branch_result(
        "defect",
        SendMessageOutput(),
        _failure("replyToMessage", FailureKind.VALIDATION, "The reply body is required."),
    )


__all__ = [
    "Mock",
    "get_message_customer_question",
    "get_message_deleted_message",
    "get_message_revoked_grant",
    "get_message_malformed_payload",
    "get_message_empty_message_id",
    "get_message_rate_limited",
    "send_message_sent_to_customer",
    "send_message_invalid_recipient",
    "send_message_connection_lost",
    "send_message_no_recipients",
    "send_message_unavailable",
    "reply_to_message_replied_in_thread",
    "reply_to_message_source_missing",
    "reply_to_message_malformed_source",
    "reply_to_message_connection_lost",
    "reply_to_message_empty_body",
]
