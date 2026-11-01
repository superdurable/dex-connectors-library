# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListOutboundMessagesInput chooses a bounded outbound search page.
@dataclass(frozen=True, slots=True)
class ListOutboundMessagesInput:
    # Count is the number of messages requested, up to 500.
    count: int = 0
    # Offset is the zero-based page offset.
    offset: int = 0


setattr(ListOutboundMessagesInput, '__module__', 'dex_connectors.postmark.models')


# MessageDetails contains non-secret outbound message metadata.
@dataclass(frozen=True, slots=True)
class MessageDetails:
    # MessageID is Postmark's outbound message identifier.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'MessageID'})
    # Subject is the email subject.
    subject: str = _dataclass_field(default='', metadata={'dex_json_name': 'Subject'})
    # Status is the provider's current status.
    status: str = _dataclass_field(default='', metadata={'dex_json_name': 'Status'})


setattr(MessageDetails, '__module__', 'dex_connectors.postmark.models')


# MessagePage is one outbound message search page.
@dataclass(frozen=True, slots=True)
class MessagePage:
    # TotalCount is the count of matching provider records.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'TotalCount'})
    # Messages contains the current outbound page.
    messages: tuple[MessageDetails, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'Messages'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'messages', _replace_nil_slices(self.messages, 1))


MessagePage.__annotations__['messages'] = tuple[MessageDetails, ...] | None
MessagePage.__dataclass_fields__['messages'].type = tuple[MessageDetails, ...] | None
setattr(MessagePage, '__module__', 'dex_connectors.postmark.models')


# GetMessageDetailsInput identifies an outbound message.
@dataclass(frozen=True, slots=True)
class GetMessageDetailsInput:
    # MessageID is Postmark's outbound message identifier.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})


setattr(GetMessageDetailsInput, '__module__', 'dex_connectors.postmark.models')
