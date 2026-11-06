# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import format_rfc3339_nano

from .timestamps import canonical_go_timestamp

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListThreadRepliesInput selects one page of replies to a channel message.
@dataclass(frozen=True, slots=True)
class ListThreadRepliesInput:
    # TeamID is the team's GUID, such as fbe2bf47-16c8-47cf-b4a5-4b9b187c508b.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId'})
    # ChannelID is the channel ID, such as 19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # MessageID is the root message's ID, such as the MessageID a postChannelMessage Result returned.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # PageSize is 1 to 50 replies; zero reads 20. It is ignored with a Cursor, which keeps its own size.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Cursor is a previous page's NextCursor; empty reads the first page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # MaxTextCharacters bounds each reply's Text from 1 to 100000 characters; zero keeps 4000.
    max_text_characters: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxTextCharacters', 'dex_json_omit_default': True})


setattr(ListThreadRepliesInput, '__module__', 'dex_connectors.microsoft_teams.models')


# MessageSender identifies who posted a message. Exactly one of UserID and ApplicationID is
# usually set; both are empty for a system event.
@dataclass(frozen=True, slots=True)
class MessageSender:
    # UserID is the sender's Microsoft Entra object ID when a person posted, including through this connector.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId', 'dex_json_omit_default': True})
    # ApplicationID is the sending app's ID when a bot or connector posted.
    application_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'applicationId', 'dex_json_omit_default': True})
    # DisplayName is the sender's display name at the time Graph read the message.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})


setattr(MessageSender, '__module__', 'dex_connectors.microsoft_teams.models')


# Message is one channel message or reply as Microsoft Graph returned it, with its body converted to text.
@dataclass(frozen=True, slots=True)
class Message:
    # ID is the message ID, a decimal number unique within its channel or thread, such as 1616989753153.
    id: str = ''
    # ReplyToID is the root message ID for a reply.
    reply_to_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'replyToId', 'dex_json_omit_default': True})
    # MessageType is Graph's messageType, such as message or systemEventMessage.
    message_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageType'})
    # CreatedAt is when Teams stored the message.
    created_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'createdAt'})
    # LastEditedAt is when the message was last edited, or nil when it was never edited.
    last_edited_at: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'lastEditedAt', 'dex_json_omit_default': True})
    # IsDeleted reports a deleted message; its Text is then empty.
    is_deleted: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDeleted', 'dex_json_omit_default': True})
    # Sender identifies who posted the message.
    sender: MessageSender = _dataclass_field(default_factory=MessageSender)
    # Subject is the plain-text subject of a root message, or empty.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Importance is normal, high, or urgent.
    importance: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # ContentType is the body's representation in Teams.
    content_type: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'contentType', 'dex_json_omit_default': True})
    # Text is the body as plain text: HTML tags are removed, mentions keep their names, emoji keep their
    # characters, and block elements become line breaks.
    text: str = ''
    # IsTextTruncated reports that Text stopped at the requested maximum number of characters.
    is_text_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isTextTruncated', 'dex_json_omit_default': True})
    # WebURL opens the message in Microsoft Teams.
    web_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'webUrl', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'created_at'))
            object.__setattr__(self, 'created_at', canonical)
        if self.last_edited_at is not None:
            timestamp = self.last_edited_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'last_edited_at'))
            object.__setattr__(self, 'last_edited_at', canonical)


setattr(Message, '__module__', 'dex_connectors.microsoft_teams.models')


# ListThreadRepliesOutput is one page of replies in the order Graph returned them.
@dataclass(frozen=True, slots=True)
class ListThreadRepliesOutput:
    # Replies are the page's replies, including deleted replies and system events, which callers may skip.
    replies: tuple[Message, ...] = ()
    # NextCursor reads the following page; empty means this is the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'replies', _replace_nil_slices(self.replies, 1))


ListThreadRepliesOutput.__annotations__['replies'] = tuple[Message, ...] | None
ListThreadRepliesOutput.__dataclass_fields__['replies'].type = tuple[Message, ...] | None
setattr(ListThreadRepliesOutput, '__module__', 'dex_connectors.microsoft_teams.models')


# PostChannelMessageInput describes one top-level channel message.
@dataclass(frozen=True, slots=True)
class PostChannelMessageInput:
    # TeamID is the team's GUID, such as fbe2bf47-16c8-47cf-b4a5-4b9b187c508b; the teamPicker unit saves it.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId'})
    # ChannelID is the channel ID, such as 19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2; the
    # channelPicker unit saves it.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # Subject is an optional one-line plain-text subject Teams shows above the message.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Content is the message body in ContentType.
    content: str = ''
    # ContentType is text or html; blank means text.
    content_type: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'contentType', 'dex_json_omit_default': True})
    # Importance is normal, high, or urgent; blank leaves Teams' default.
    importance: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(PostChannelMessageInput, '__module__', 'dex_connectors.microsoft_teams.models')


# PostMessageOutput identifies a posted message. On providerRejected, uncertain, and defect only the
# requested team, channel, root message, or chat IDs are set.
@dataclass(frozen=True, slots=True)
class PostMessageOutput:
    # MessageID is the new message's ID.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId', 'dex_json_omit_default': True})
    # TeamID is the team of a channel message or reply.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId', 'dex_json_omit_default': True})
    # ChannelID is the channel of a channel message or reply.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId', 'dex_json_omit_default': True})
    # ReplyToID is the root message a reply belongs to.
    reply_to_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'replyToId', 'dex_json_omit_default': True})
    # ChatID is the chat of a chat message.
    chat_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'chatId', 'dex_json_omit_default': True})
    # CreatedAt is when Teams stored the message.
    created_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'createdAt'})
    # Sender is the signed-in user Teams shows as the sender.
    sender: MessageSender = _dataclass_field(default_factory=MessageSender)
    # WebURL opens the message in Microsoft Teams.
    web_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'webUrl', 'dex_json_omit_default': True})
    # IsConfirmedByReadBack reports that an earlier attempt sent the message without a confirmed outcome
    # and the connector found it by reading the channel or thread back instead of sending it again.
    is_confirmed_by_read_back: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isConfirmedByReadBack', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'created_at'))
            object.__setattr__(self, 'created_at', canonical)


setattr(PostMessageOutput, '__module__', 'dex_connectors.microsoft_teams.models')


# PostThreadReplyInput describes one reply to an existing channel message.
@dataclass(frozen=True, slots=True)
class PostThreadReplyInput:
    # TeamID is the team's GUID, such as fbe2bf47-16c8-47cf-b4a5-4b9b187c508b.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId'})
    # ChannelID is the channel ID, such as 19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # MessageID is the root message's ID, such as the MessageID a postChannelMessage Result returned.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # Content is the reply body in ContentType.
    content: str = ''
    # ContentType is text or html; blank means text.
    content_type: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'contentType', 'dex_json_omit_default': True})
    # Importance is normal, high, or urgent; blank leaves Teams' default.
    importance: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(PostThreadReplyInput, '__module__', 'dex_connectors.microsoft_teams.models')


# PostChatMessageInput describes one message in an existing chat.
@dataclass(frozen=True, slots=True)
class PostChatMessageInput:
    # ChatID is the chat ID, such as 19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2 or a one-on-one chat's
    # 19:...@unq.gbl.spaces ID; the chatPicker unit saves it.
    chat_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'chatId'})
    # Content is the message body in ContentType.
    content: str = ''
    # ContentType is text or html; blank means text.
    content_type: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'contentType', 'dex_json_omit_default': True})
    # Importance is normal, high, or urgent; blank leaves Teams' default.
    importance: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(PostChatMessageInput, '__module__', 'dex_connectors.microsoft_teams.models')
