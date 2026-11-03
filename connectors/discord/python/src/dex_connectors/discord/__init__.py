# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Discord channel, message, and reaction operations."""

from ._generated.config import Config, Credentials
from .client import Client
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    AddedReaction,
    AddReactionInput,
    Channel,
    CreateMessageInput,
    GetChannelInput,
    ListChannelMessagesInput,
    Message,
    MessageAuthor,
    MessagePage,
)
from .operations import (
    ADD_REACTION_DEFINITION,
    CREATE_MESSAGE_DEFINITION,
    GET_CHANNEL_DEFINITION,
    LIST_CHANNEL_MESSAGES_DEFINITION,
    AddReactionOperation,
    CreateMessageOperation,
    GetChannelOperation,
    ListChannelMessagesOperation,
)
from .steps import (
    new_add_reaction_step,
    new_create_message_step,
    new_get_channel_step,
    new_list_channel_messages_step,
)

__all__ = [
    "ADD_REACTION_DEFINITION",
    "CREATE_MESSAGE_DEFINITION",
    "GET_CHANNEL_DEFINITION",
    "LIST_CHANNEL_MESSAGES_DEFINITION",
    "AddReactionInput",
    "AddedReaction",
    "AddReactionOperation",
    "Channel",
    "Client",
    "Config",
    "Connection",
    "CreateMessageInput",
    "CreateMessageOperation",
    "Credentials",
    "GetChannelInput",
    "GetChannelOperation",
    "ListChannelMessagesInput",
    "ListChannelMessagesOperation",
    "Message",
    "MessageAuthor",
    "MessagePage",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "new_add_reaction_step",
    "new_create_message_step",
    "new_get_channel_step",
    "new_list_channel_messages_step",
]
