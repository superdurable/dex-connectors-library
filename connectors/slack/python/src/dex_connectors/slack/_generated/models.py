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


# ListChannelsInput selects one page of Slack channels visible to the connection's app.
@dataclass(frozen=True, slots=True)
class ListChannelsInput:
    # NameFilter keeps only channels whose name contains this text, ignoring case, surrounding spaces, and one leading #,
    # so "#General" matches general and general-ops. Blank keeps every channel on the page.
    name_filter: str = _dataclass_field(default='', metadata={'dex_json_name': 'nameFilter', 'dex_json_omit_default': True})
    # Visibility is public for public channels only or private for private channels only. Blank lists both.
    visibility: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # MemberOnly lists only channels the app has joined, which are the channels it can post to. False also lists
    # public channels the app has not joined.
    member_only: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'memberOnly', 'dex_json_omit_default': True})
    # Limit is the number of channels Slack examines for this page, from 1 through 999, Slack's maximum. Zero uses 200. The name
    # filter runs after Slack returns the page, so fewer channels can be returned.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Cursor is the nextCursor from the previous page. Blank reads the first page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListChannelsInput, '__module__', 'dex_connectors.slack.models')


# Channel describes one Slack channel.
@dataclass(frozen=True, slots=True)
class Channel:
    # ChannelID is the Slack channel identifier, such as C0123456789, accepted as channelId by the other Slack operations.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # Name is the channel name without the leading #.
    name: str = ''
    # IsPrivate is true for a private channel.
    is_private: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPrivate'})
    # IsMember is true when the app has joined the channel and can post to it.
    is_member: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isMember'})
    # Topic is the channel topic; blank when none is set.
    topic: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Purpose is the channel purpose; blank when none is set.
    purpose: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(Channel, '__module__', 'dex_connectors.slack.models')


# ListChannelsOutput contains one filtered page of Slack channels.
@dataclass(frozen=True, slots=True)
class ListChannelsOutput:
    # Channels are the page's channels that match the input filters, in Slack's order.
    channels: tuple[Channel, ...] = ()
    # NextCursor continues the listing when non-blank, even if Channels is empty; blank means the listing ended.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'channels', _replace_nil_slices(self.channels, 1))


ListChannelsOutput.__annotations__['channels'] = tuple[Channel, ...] | None
ListChannelsOutput.__dataclass_fields__['channels'].type = tuple[Channel, ...] | None
setattr(ListChannelsOutput, '__module__', 'dex_connectors.slack.models')


# GetWorkspaceIdentityInput takes no fields because the connection determines the workspace.
@dataclass(frozen=True, slots=True)
class GetWorkspaceIdentityInput:
    pass


setattr(GetWorkspaceIdentityInput, '__module__', 'dex_connectors.slack.models')


# GetWorkspaceIdentityOutput identifies the Slack workspace and bot that the connection is authorized for.
@dataclass(frozen=True, slots=True)
class GetWorkspaceIdentityOutput:
    # TeamID is the Slack workspace identifier, such as T0123456789.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId'})
    # TeamName is the workspace name shown in Slack.
    team_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamName'})
    # WorkspaceURL is the workspace's Slack URL, such as https://acme.slack.com/.
    workspace_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'workspaceUrl'})
    # EnterpriseID is the Enterprise Grid organization identifier; blank outside Enterprise Grid.
    enterprise_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'enterpriseId', 'dex_json_omit_default': True})
    # IsEnterpriseInstall is true when the app is installed for a whole Enterprise Grid organization.
    is_enterprise_install: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isEnterpriseInstall'})
    # BotUserID is the Slack user identifier of the bot that posts messages, such as U0BOT123456.
    bot_user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'botUserId'})
    # BotUserName is the bot user's Slack name.
    bot_user_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'botUserName'})
    # BotID is the Slack bot identifier, such as B0123456789.
    bot_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'botId'})


setattr(GetWorkspaceIdentityOutput, '__module__', 'dex_connectors.slack.models')


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
