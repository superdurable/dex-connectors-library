# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex Slack connector for channel discovery, thread reads, and message writes."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    Channel,
    GetThreadReplyInput,
    GetThreadReplyOutput,
    GetWorkspaceIdentityInput,
    GetWorkspaceIdentityOutput,
    ListChannelsInput,
    ListChannelsOutput,
    ListThreadMessagesInput,
    ListThreadMessagesOutput,
    Message,
    PostChannelMessageInput,
    PostMessageOutput,
    PostThreadReplyInput,
)
from .steps import (
    new_get_thread_reply_step,
    new_get_workspace_identity_step,
    new_list_channels_step,
    new_list_thread_messages_step,
    new_post_channel_message_step,
    new_post_thread_reply_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "Channel",
    "GetThreadReplyInput",
    "GetThreadReplyOutput",
    "GetWorkspaceIdentityInput",
    "GetWorkspaceIdentityOutput",
    "ListChannelsInput",
    "ListChannelsOutput",
    "ListThreadMessagesInput",
    "ListThreadMessagesOutput",
    "Message",
    "PostChannelMessageInput",
    "PostMessageOutput",
    "PostThreadReplyInput",
    "new_get_thread_reply_step",
    "new_get_workspace_identity_step",
    "new_list_channels_step",
    "new_list_thread_messages_step",
    "new_post_channel_message_step",
    "new_post_thread_reply_step",
]
