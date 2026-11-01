# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class ListOutboundMessagesInput:
    """Typed ListOutboundMessagesInput value from the Go connector contract."""

    count: int = 0
    offset: int = 0


setattr(ListOutboundMessagesInput, '__module__', 'dex_connectors.postmark.models')


@dataclass(frozen=True, slots=True)
class MessageDetails:
    """Typed MessageDetails value from the Go connector contract."""

    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'MessageID'})
    subject: str = _dataclass_field(default='', metadata={'dex_json_name': 'Subject'})
    status: str = _dataclass_field(default='', metadata={'dex_json_name': 'Status'})


setattr(MessageDetails, '__module__', 'dex_connectors.postmark.models')


@dataclass(frozen=True, slots=True)
class MessagePage:
    """Typed MessagePage value from the Go connector contract."""

    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'TotalCount'})
    messages: tuple[MessageDetails, ...] | None = _dataclass_field(default=None, metadata={'dex_json_name': 'Messages'})


setattr(MessagePage, '__module__', 'dex_connectors.postmark.models')


@dataclass(frozen=True, slots=True)
class GetMessageDetailsInput:
    """Typed GetMessageDetailsInput value from the Go connector contract."""

    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})


setattr(GetMessageDetailsInput, '__module__', 'dex_connectors.postmark.models')
