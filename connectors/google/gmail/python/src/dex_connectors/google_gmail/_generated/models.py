# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return value
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# GetMessageInput identifies one Gmail message.
@dataclass(frozen=True, slots=True)
class GetMessageInput:
    # MessageID is the provider message identifier.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})


setattr(GetMessageInput, '__module__', 'dex_connectors.google_gmail.models')


# Message is one decoded Gmail message.
@dataclass(frozen=True, slots=True)
class Message:
    # MessageID is the provider message identifier.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # ThreadID is the provider conversation thread identifier.
    thread_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadId'})
    # RFCMessageID is the decoded Message-ID header.
    rfc_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'rfcMessageId', 'dex_json_omit_default': True})
    # InReplyTo is the decoded In-Reply-To header.
    in_reply_to: str = _dataclass_field(default='', metadata={'dex_json_name': 'inReplyTo', 'dex_json_omit_default': True})
    # References is the decoded References header.
    references: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # From is the sender address.
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from'})
    # ReplyTo is the decoded Reply-To header.
    reply_to: str = _dataclass_field(default='', metadata={'dex_json_name': 'replyTo', 'dex_json_omit_default': True})
    # To lists recipient addresses.
    to: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Subject is the message subject.
    subject: str = ''
    # TextBody is the plain-text message body.
    text_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'textBody', 'dex_json_omit_default': True})
    # HTMLBody is the optional HTML message body.
    html_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'htmlBody', 'dex_json_omit_default': True})
    # Snippet is the snippet for message.
    snippet: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ReceivedAt is the received at for message.
    received_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'receivedAt', 'dex_json_format': 'rfc3339'})
    # LabelIDs lists the Gmail labels applied to the message.
    label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'labelIds', 'dex_json_omit_default': True})


setattr(Message, '__module__', 'dex_connectors.google_gmail.models')


# SendMessageInput contains the provider request fields for send message.
@dataclass(frozen=True, slots=True)
class SendMessageInput:
    # To lists recipient addresses.
    to: tuple[str, ...] = ()
    # Subject is the message subject.
    subject: str = ''
    # TextBody is the plain-text message body.
    text_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'textBody'})
    # HTMLBody is the optional HTML message body.
    html_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'htmlBody', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'to', _replace_nil_slices(self.to, 1))


SendMessageInput.__annotations__['to'] = tuple[str, ...] | None
setattr(SendMessageInput, '__module__', 'dex_connectors.google_gmail.models')


# SendMessageOutput contains the provider response fields for send message.
@dataclass(frozen=True, slots=True)
class SendMessageOutput:
    # Sender is the authenticated sender address.
    sender: str = ''
    # Recipients lists resolved addresses; it is empty when an earlier reply is found before reading its source.
    recipients: tuple[str, ...] = ()
    # MessageID is the provider message identifier; empty when Gmail accepted a send but read-back found no ID.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # ThreadID is the provider conversation thread identifier. When an accepted reply lacks IDs, it is the requested thread.
    # When an accepted new message lacks IDs, it is empty.
    thread_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadId'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'recipients', _replace_nil_slices(self.recipients, 1))


SendMessageOutput.__annotations__['recipients'] = tuple[str, ...] | None
setattr(SendMessageOutput, '__module__', 'dex_connectors.google_gmail.models')


# ReplyToMessageInput identifies a message and supplies the reply body.
@dataclass(frozen=True, slots=True)
class ReplyToMessageInput:
    # MessageID is the provider message identifier.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # TextBody is the plain-text message body.
    text_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'textBody'})
    # HTMLBody is the optional HTML message body.
    html_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'htmlBody', 'dex_json_omit_default': True})


setattr(ReplyToMessageInput, '__module__', 'dex_connectors.google_gmail.models')
