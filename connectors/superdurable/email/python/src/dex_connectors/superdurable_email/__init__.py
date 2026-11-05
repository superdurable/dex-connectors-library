# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Email connector for Dex Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    Attachment,
    EmailAddress,
    GetMessageInput,
    Message,
    MessageFlags,
    MessageReference,
    MessageSummary,
    MovedMessage,
    MoveMessageInput,
    ReplyToMessageInput,
    SearchMessagesInput,
    SearchMessagesOutput,
    SendMessageInput,
    SentMessage,
    SetFlagsInput,
)
from .steps import (
    new_get_message_step,
    new_move_message_step,
    new_reply_to_message_step,
    new_search_messages_step,
    new_send_message_step,
    new_set_flags_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "Attachment",
    "EmailAddress",
    "GetMessageInput",
    "Message",
    "MessageFlags",
    "MessageReference",
    "MessageSummary",
    "MoveMessageInput",
    "MovedMessage",
    "ReplyToMessageInput",
    "SearchMessagesInput",
    "SearchMessagesOutput",
    "SendMessageInput",
    "SentMessage",
    "SetFlagsInput",
    "new_get_message_step",
    "new_move_message_step",
    "new_reply_to_message_step",
    "new_search_messages_step",
    "new_send_message_step",
    "new_set_flags_step",
]
