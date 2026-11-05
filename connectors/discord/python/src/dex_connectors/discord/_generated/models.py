# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from .timestamps import parse_go_datetime, validate_go_datetime

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# GetChannelInput identifies one channel.
@dataclass(frozen=True, slots=True)
class GetChannelInput:
    # ChannelID is the channel's snowflake ID, 17 to 20 digits, from Copy Channel ID in Discord's Developer Mode.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})


setattr(GetChannelInput, '__module__', 'dex_connectors.discord.models')


# Channel is the part of a Discord channel object that Flows route on.
# Name and Topic are untrusted provider text: escape them before rendering HTML.
@dataclass(frozen=True, slots=True)
class Channel:
    # ID is the channel's snowflake ID.
    id: str = ''
    # Type is Discord's channel type number, such as 0 for a server text channel or 5 for an announcement channel.
    type: int = 0
    # GuildID is the server's snowflake ID, blank for a direct message channel.
    guild_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'guildId', 'dex_json_omit_default': True})
    # Name is the channel name without the leading #, blank for a direct message channel.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Topic is the channel topic, blank when none is set.
    topic: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # LastMessageID is the snowflake ID of the newest message, blank when unknown or empty.
    last_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastMessageId', 'dex_json_omit_default': True})


setattr(Channel, '__module__', 'dex_connectors.discord.models')


# ListChannelMessagesInput selects one page of a channel's messages.
#
# Discord returns messages newest first. Set at most one of BeforeMessageID
# and AfterMessageID: blank both reads the newest page, BeforeMessageID pages
# toward older messages, and AfterMessageID reads messages newer than it.
@dataclass(frozen=True, slots=True)
class ListChannelMessagesInput:
    # ChannelID is the channel's snowflake ID, 17 to 20 digits.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # Limit is the most messages returned, 1 to 100; zero returns up to 50.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # BeforeMessageID lists messages older than this message ID, such as the previous page's OldestMessageID.
    before_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'beforeMessageId', 'dex_json_omit_default': True})
    # AfterMessageID lists messages newer than this message ID.
    after_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'afterMessageId', 'dex_json_omit_default': True})


setattr(ListChannelMessagesInput, '__module__', 'dex_connectors.discord.models')


# MessageAuthor identifies the sender of a message.
@dataclass(frozen=True, slots=True)
class MessageAuthor:
    # ID is the user's snowflake ID.
    id: str = ''
    # Username is the user's unique Discord username.
    username: str = ''
    # IsBot reports that the author is a bot account.
    is_bot: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBot'})


setattr(MessageAuthor, '__module__', 'dex_connectors.discord.models')


# Message is the part of a Discord message object that Flows route on.
# Content is untrusted provider text: escape it before rendering HTML. A bot
# without the Message Content intent reads empty content for most server
# messages.
@dataclass(frozen=True, slots=True)
class Message:
    # ID is the message's snowflake ID.
    id: str = ''
    # ChannelID is the snowflake ID of the channel that holds the message.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # Author is the user or bot that sent the message.
    author: MessageAuthor = _dataclass_field(default_factory=MessageAuthor)
    # Content is the message text, up to 2000 characters.
    content: str = ''
    # Timestamp is when Discord created the message, in UTC.
    timestamp: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_format': 'rfc3339'})
    # EditedTimestamp is when the message was last edited, in UTC, or nil when it never was.
    edited_timestamp: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'editedTimestamp', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # Type is Discord's message type number, such as 0 for a default message or 19 for a reply.
    type: int = 0

    def __post_init__(self) -> None:
        if isinstance(self.timestamp, str):
            object.__setattr__(self, 'timestamp', parse_go_datetime(self.timestamp, 'timestamp'))
        elif self.timestamp is not None:
            validate_go_datetime(self.timestamp, 'timestamp')
        if isinstance(self.edited_timestamp, str):
            object.__setattr__(self, 'edited_timestamp', parse_go_datetime(self.edited_timestamp, 'edited_timestamp'))
        elif self.edited_timestamp is not None:
            validate_go_datetime(self.edited_timestamp, 'edited_timestamp')


setattr(Message, '__module__', 'dex_connectors.discord.models')


# MessagePage is one page of listed messages, newest first.
@dataclass(frozen=True, slots=True)
class MessagePage:
    # Messages holds at most the requested limit of messages; an empty page is an empty list.
    messages: tuple[Message, ...] = ()
    # OldestMessageID is the last message's ID, to pass as BeforeMessageID for the next older page; blank when empty.
    oldest_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'oldestMessageId', 'dex_json_omit_default': True})
    # IsFull reports that the page holds the requested limit, so an older page may exist.
    is_full: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFull'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'messages', _replace_nil_slices(self.messages, 1))


MessagePage.__annotations__['messages'] = tuple[Message, ...] | None
MessagePage.__dataclass_fields__['messages'].type = tuple[Message, ...] | None
setattr(MessagePage, '__module__', 'dex_connectors.discord.models')


# CreateMessageInput sends one text message to a channel as the bot.
#
# Every mention is disabled, so text such as @everyone or <@id> renders but
# notifies nobody.
@dataclass(frozen=True, slots=True)
class CreateMessageInput:
    # ChannelID is the channel's snowflake ID, 17 to 20 digits.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # Content is the message text, 1 to 2000 characters and not only whitespace.
    content: str = ''
    # ReplyToMessageID makes the message a reply to this message in the same channel; blank sends a plain message.
    reply_to_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'replyToMessageId', 'dex_json_omit_default': True})


setattr(CreateMessageInput, '__module__', 'dex_connectors.discord.models')


# AddReactionInput adds the bot's reaction to one message.
@dataclass(frozen=True, slots=True)
class AddReactionInput:
    # ChannelID is the channel's snowflake ID, 17 to 20 digits.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # MessageID is the message's snowflake ID.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # Emoji is a Unicode emoji such as 👍, or a custom emoji as name:id, such as party_blob:123456789012345678.
    emoji: str = ''


setattr(AddReactionInput, '__module__', 'dex_connectors.discord.models')


# AddedReaction reports the reaction the bot now has on the message.
@dataclass(frozen=True, slots=True)
class AddedReaction:
    # ChannelID is the channel's snowflake ID.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # MessageID is the message's snowflake ID.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # Emoji is the emoji as the input named it.
    emoji: str = ''


setattr(AddedReaction, '__module__', 'dex_connectors.discord.models')
