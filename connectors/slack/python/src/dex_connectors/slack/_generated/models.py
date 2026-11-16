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


# ListChannelMessagesInput selects one page of a Slack channel's top-level message history.
@dataclass(frozen=True, slots=True)
class ListChannelMessagesInput:
    # ChannelID is the Slack channel identifier, such as C0123456789, from listChannels or the channel picker.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # OldestTimestamp keeps only messages after this Slack message timestamp, such as 1710000000.000100.
    # Blank reads from the start of the channel's history.
    oldest_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'oldestTimestamp', 'dex_json_omit_default': True})
    # LatestTimestamp keeps only messages before this Slack message timestamp, such as 1710000099.000100.
    # Blank reads up to the newest message.
    latest_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'latestTimestamp', 'dex_json_omit_default': True})
    # IsInclusive also keeps messages whose timestamp equals OldestTimestamp or LatestTimestamp.
    # Slack ignores it when both timestamps are blank.
    is_inclusive: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isInclusive', 'dex_json_omit_default': True})
    # PageSize is the maximum number of messages on this page: 1 through 15; blank or 0 reads 15, the most
    # Slack returns to a non-Marketplace app. Slack may return fewer messages while NextCursor continues the history.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Cursor is the nextCursor from the previous page. Blank reads the newest page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListChannelMessagesInput, '__module__', 'dex_connectors.slack.models')


# ChannelMessage is one top-level message from a Slack channel's history.
@dataclass(frozen=True, slots=True)
class ChannelMessage:
    # ChannelID is the Slack channel identifier that was read.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # Timestamp is the Slack message timestamp, which identifies the message within its channel.
    timestamp: str = ''
    # ThreadTimestamp is the parent timestamp of the message's thread: equal to Timestamp for a thread parent,
    # different for a thread reply that was also sent to the channel, and blank for a message without a thread.
    thread_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadTimestamp', 'dex_json_omit_default': True})
    # UserID is the Slack user identifier of the poster, such as the bot user for the app's own posts.
    # Blank for messages posted by integrations without a user.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId', 'dex_json_omit_default': True})
    # BotID is the Slack bot identifier when an app or integration posted the message; blank for people.
    bot_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'botId', 'dex_json_omit_default': True})
    # Text is the message text in Slack's markup.
    text: str = ''
    # ReplyCount is the number of thread replies when the message starts a thread; zero otherwise.
    reply_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'replyCount', 'dex_json_omit_default': True})


setattr(ChannelMessage, '__module__', 'dex_connectors.slack.models')


# ListChannelMessagesOutput contains one newest-first page of a channel's top-level messages.
@dataclass(frozen=True, slots=True)
class ListChannelMessagesOutput:
    # Messages are the page's top-level messages, newest first. A thread reply appears only when its poster
    # also sent it to the channel; read the other replies with ListThreadMessages.
    messages: tuple[ChannelMessage, ...] = ()
    # NextCursor continues to older messages when non-blank; blank means the requested range ended.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'messages', _replace_nil_slices(self.messages, 1))


ListChannelMessagesOutput.__annotations__['messages'] = tuple[ChannelMessage, ...] | None
ListChannelMessagesOutput.__dataclass_fields__['messages'].type = tuple[ChannelMessage, ...] | None
setattr(ListChannelMessagesOutput, '__module__', 'dex_connectors.slack.models')


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


# MessageButton is one Block Kit button in a message that postButtonMessage posts.
@dataclass(frozen=True, slots=True)
class MessageButton:
    # ActionID identifies the button in the buttonClicked Trigger's ButtonClickEvent.ActionID and in its
    # ActionIDs filter. It is required, unique within the message, and at most 255 characters.
    action_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'actionId'})
    # Text is the required plain-text button label, at most 75 characters; Slack may truncate it near 30.
    text: str = ''
    # Value is returned unchanged in ButtonClickEvent.Value, such as an order ID, at most 2000 characters.
    # Empty sends no value. Like message text, every member who can see the message can read it, and a click
    # delivers it to the Flow and records it in the durable inbox, so it must never be a secret or token.
    value: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Style is ButtonStylePrimary, ButtonStyleDanger, or empty for Slack's default style.
    style: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(MessageButton, '__module__', 'dex_connectors.slack.models')


# PostButtonMessageInput contains the provider request fields for post button message.
@dataclass(frozen=True, slots=True)
class PostButtonMessageInput:
    # ChannelID is the Slack channel identifier.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # ThreadTimestamp is the parent message timestamp to reply in that thread; empty posts a top-level message.
    thread_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadTimestamp', 'dex_json_omit_default': True})
    # Text is the required Slack mrkdwn message shown above the buttons and used in notifications, at most
    # 3000 characters and the connection's maxMessageCharacters.
    text: str = ''
    # Buttons are the 1 through 25 buttons shown below the text, in order.
    buttons: tuple[MessageButton, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'buttons', _replace_nil_slices(self.buttons, 1))


PostButtonMessageInput.__annotations__['buttons'] = tuple[MessageButton, ...] | None
PostButtonMessageInput.__dataclass_fields__['buttons'].type = tuple[MessageButton, ...] | None
setattr(PostButtonMessageInput, '__module__', 'dex_connectors.slack.models')


# MessageEvent is one normalized message provider event.
@dataclass(frozen=True, slots=True)
class MessageEvent:
    # TeamID is the team ID returned by Slack.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId'})
    # ChannelID is the Slack channel identifier.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # Timestamp is the Slack message timestamp identifier.
    timestamp: str = ''
    # ThreadTimestamp is the Slack parent message timestamp.
    thread_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadTimestamp'})
    # UserID is the Slack user identifier.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId'})
    # Text is the text returned by Slack.
    text: str = ''


setattr(MessageEvent, '__module__', 'dex_connectors.slack.models')


# MessageMatcher filters Slack messages by text and posting user.
@dataclass(frozen=True, slots=True)
class MessageMatcher:
    # MessageContains requires this case-insensitive substring when non-empty.
    message_contains: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageContains', 'dex_json_omit_default': True})
    # PosterUserIDs limits matches to these Slack user IDs when non-empty.
    poster_user_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'posterUserIds', 'dex_json_omit_default': True})


setattr(MessageMatcher, '__module__', 'dex_connectors.slack.models')


# ChannelThreadCreatedTriggerConfiguration configures channel thread created trigger.
@dataclass(frozen=True, slots=True)
class ChannelThreadCreatedTriggerConfiguration:
    # ChannelID is the Slack channel identifier.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # ThreadTriggerMatcher specifies thread trigger matcher for channel thread created trigger configuration.
    thread_trigger_matcher: MessageMatcher = _dataclass_field(default_factory=MessageMatcher, metadata={'dex_json_name': 'threadTriggerMatcher'})


setattr(ChannelThreadCreatedTriggerConfiguration, '__module__', 'dex_connectors.slack.models')


# ThreadReplyCreatedTriggerConfiguration configures thread reply created trigger.
@dataclass(frozen=True, slots=True)
class ThreadReplyCreatedTriggerConfiguration:
    # ChannelID is the Slack channel identifier.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # ThreadReplyMatcher specifies thread reply matcher for thread reply created trigger configuration.
    thread_reply_matcher: MessageMatcher = _dataclass_field(default_factory=MessageMatcher, metadata={'dex_json_name': 'threadReplyMatcher'})


setattr(ThreadReplyCreatedTriggerConfiguration, '__module__', 'dex_connectors.slack.models')


# ButtonClickEvent is one click on a message button, decoded from a Slack block_actions payload. It carries
# IDs and the button's value, never the Slack response URL, trigger ID, or verification token.
@dataclass(frozen=True, slots=True)
class ButtonClickEvent:
    # TeamID is the Slack workspace ID of the clicked message.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId'})
    # ChannelID is the Slack channel identifier of the clicked message.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # MessageTimestamp is the Slack timestamp identifier of the message that holds the button.
    message_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageTimestamp'})
    # ThreadTimestamp is the parent message timestamp of the thread that holds the message, or
    # MessageTimestamp for a top-level message, as MessageEvent reports a thread root.
    thread_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'threadTimestamp'})
    # UserID is the Slack user ID of the member who clicked.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId'})
    # ActionID is the clicked button's action ID.
    action_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'actionId'})
    # Value is the clicked button's value, or empty when the button has none.
    value: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ActionTimestamp is Slack's timestamp of the click; each click has its own.
    action_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'actionTimestamp'})


setattr(ButtonClickEvent, '__module__', 'dex_connectors.slack.models')


# ButtonClickedTriggerConfiguration selects the message button clicks that a buttonClicked binding receives.
@dataclass(frozen=True, slots=True)
class ButtonClickedTriggerConfiguration:
    # ChannelID is the Slack channel whose message buttons the binding receives.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})
    # ActionIDs limits matches to buttons with these action IDs, the MessageButton.ActionID values that
    # postButtonMessage posted. Empty matches every button in the channel.
    action_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'actionIds', 'dex_json_omit_default': True})
    # ClickerUserIDs limits matches to clicks by these Slack user IDs. Empty matches a click by anyone who can
    # see the message.
    clicker_user_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'clickerUserIds', 'dex_json_omit_default': True})


setattr(ButtonClickedTriggerConfiguration, '__module__', 'dex_connectors.slack.models')
