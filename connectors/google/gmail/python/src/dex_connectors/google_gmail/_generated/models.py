# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime

@dataclass(frozen=True, slots=True)
class GetMessageInput:
    """Typed GetMessageInput value from the Go connector contract."""

    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})


setattr(GetMessageInput, '__module__', 'dex_connectors.google_gmail.models')


@dataclass(frozen=True, slots=True)
class Message:
    """Typed Message value from the Go connector contract."""

    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    thread_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadId'})
    rfc_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'rfcMessageId', 'dex_json_omit_default': True})
    in_reply_to: str = _dataclass_field(default='', metadata={'dex_json_name': 'inReplyTo', 'dex_json_omit_default': True})
    references: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from'})
    reply_to: str = _dataclass_field(default='', metadata={'dex_json_name': 'replyTo', 'dex_json_omit_default': True})
    to: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    subject: str = ''
    text_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'textBody', 'dex_json_omit_default': True})
    html_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'htmlBody', 'dex_json_omit_default': True})
    snippet: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    received_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'receivedAt', 'dex_json_format': 'rfc3339'})
    label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'labelIds', 'dex_json_omit_default': True})


setattr(Message, '__module__', 'dex_connectors.google_gmail.models')


@dataclass(frozen=True, slots=True)
class SendMessageInput:
    """Typed SendMessageInput value from the Go connector contract."""

    to: tuple[str, ...] | None = None
    subject: str = ''
    text_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'textBody'})
    html_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'htmlBody', 'dex_json_omit_default': True})


setattr(SendMessageInput, '__module__', 'dex_connectors.google_gmail.models')


@dataclass(frozen=True, slots=True)
class SendMessageOutput:
    """Typed SendMessageOutput value from the Go connector contract."""

    sender: str = ''
    recipients: tuple[str, ...] | None = None
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    thread_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadId'})


setattr(SendMessageOutput, '__module__', 'dex_connectors.google_gmail.models')


@dataclass(frozen=True, slots=True)
class ReplyToMessageInput:
    """Typed ReplyToMessageInput value from the Go connector contract."""

    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    text_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'textBody'})
    html_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'htmlBody', 'dex_json_omit_default': True})


setattr(ReplyToMessageInput, '__module__', 'dex_connectors.google_gmail.models')
