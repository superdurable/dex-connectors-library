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


# ListThreadMessagesInput contains the provider request fields for list thread messages.
@dataclass(frozen=True, slots=True)
class ListThreadMessagesInput:
    # ChannelID is the Slack channel identifier.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # ThreadTimestamp is the Slack parent message timestamp.
    thread_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadTimestamp'})
    # Cursor specifies cursor for list thread messages input.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PageSize specifies page size for list thread messages input.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListThreadMessagesInput, '__module__', 'dex_connectors.slack.models')


# Message represents the connector's message data.
@dataclass(frozen=True, slots=True)
class Message:
    # ChannelID is the Slack channel identifier.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # Timestamp is the Slack message timestamp identifier.
    timestamp: str = ''
    # ThreadTimestamp is the Slack parent message timestamp.
    thread_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadTimestamp', 'dex_json_omit_default': True})
    # UserID is the Slack user identifier.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId', 'dex_json_omit_default': True})
    # Text is the text for message.
    text: str = ''


setattr(Message, '__module__', 'dex_connectors.slack.models')


# ListThreadMessagesOutput contains the provider response fields for list thread messages.
@dataclass(frozen=True, slots=True)
class ListThreadMessagesOutput:
    # Messages is the messages returned by Slack.
    messages: tuple[Message, ...] = ()
    # NextCursor is the next cursor returned by Slack.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'messages', _replace_nil_slices(self.messages, 1))


ListThreadMessagesOutput.__annotations__['messages'] = tuple[Message, ...] | None
ListThreadMessagesOutput.__dataclass_fields__['messages'].type = tuple[Message, ...] | None
setattr(ListThreadMessagesOutput, '__module__', 'dex_connectors.slack.models')


# GetThreadReplyInput contains the provider request fields for get thread reply.
@dataclass(frozen=True, slots=True)
class GetThreadReplyInput:
    # ChannelID is the Slack channel identifier.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # ThreadTimestamp is the Slack parent message timestamp.
    thread_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadTimestamp'})
    # ReplyTimestamp is the Slack reply message timestamp.
    reply_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'replyTimestamp'})


setattr(GetThreadReplyInput, '__module__', 'dex_connectors.slack.models')


# GetThreadReplyOutput contains the provider response fields for get thread reply.
@dataclass(frozen=True, slots=True)
class GetThreadReplyOutput:
    # Message is the message returned by Slack.
    message: Message = _dataclass_field(default_factory=Message)


setattr(GetThreadReplyOutput, '__module__', 'dex_connectors.slack.models')


# PostChannelMessageInput contains the provider request fields for post channel message.
@dataclass(frozen=True, slots=True)
class PostChannelMessageInput:
    # ChannelID is the Slack channel identifier.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # Text specifies text for post channel message input.
    text: str = ''


setattr(PostChannelMessageInput, '__module__', 'dex_connectors.slack.models')


# PostMessageOutput contains the provider response fields for post message.
@dataclass(frozen=True, slots=True)
class PostMessageOutput:
    # Message is the message returned by Slack.
    message: Message = _dataclass_field(default_factory=Message)


setattr(PostMessageOutput, '__module__', 'dex_connectors.slack.models')


# PostThreadReplyInput contains the provider request fields for post thread reply.
@dataclass(frozen=True, slots=True)
class PostThreadReplyInput:
    # ChannelID is the Slack channel identifier.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # ThreadTimestamp is the Slack parent message timestamp.
    thread_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadTimestamp'})
    # Text specifies text for post thread reply input.
    text: str = ''


setattr(PostThreadReplyInput, '__module__', 'dex_connectors.slack.models')
