# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Microsoft Teams channel, thread, and chat operations."""

from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .connection import Connection as Connection
from .connection import new_connection as new_connection
from .connection import new_connection_with_operations as new_connection_with_operations
from .connection import new_project_connection as new_project_connection
from .models import ListThreadRepliesInput as ListThreadRepliesInput
from .models import ListThreadRepliesOutput as ListThreadRepliesOutput
from .models import Message as Message
from .models import MessageSender as MessageSender
from .models import PostChannelMessageInput as PostChannelMessageInput
from .models import PostChatMessageInput as PostChatMessageInput
from .models import PostMessageOutput as PostMessageOutput
from .models import PostThreadReplyInput as PostThreadReplyInput
from .steps import new_list_thread_replies_step as new_list_thread_replies_step
from .steps import new_post_channel_message_step as new_post_channel_message_step
from .steps import new_post_chat_message_step as new_post_chat_message_step
from .steps import new_post_thread_reply_step as new_post_thread_reply_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "ListThreadRepliesInput",
    "ListThreadRepliesOutput",
    "Message",
    "MessageSender",
    "PostChannelMessageInput",
    "PostChatMessageInput",
    "PostMessageOutput",
    "PostThreadReplyInput",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "new_list_thread_replies_step",
    "new_post_channel_message_step",
    "new_post_chat_message_step",
    "new_post_thread_reply_step",
]
