# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Front conversation and contact operations for Dex Python Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    Comment,
    Contact,
    ContactHandle,
    Conversation,
    ConversationDetails,
    ConversationReply,
    FindContactByEmailInput,
    FindContactByEmailOutput,
    GetConversationInput,
    Message,
    ReplyToConversationInput,
    SearchConversationsInput,
    SearchConversationsOutput,
    UpdateConversationInput,
    UpdateConversationOutput,
)
from .operations import build_reply_html
from .steps import (
    new_find_contact_by_email_step,
    new_get_conversation_step,
    new_reply_to_conversation_step,
    new_search_conversations_step,
    new_update_conversation_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "Comment",
    "Contact",
    "ContactHandle",
    "Conversation",
    "ConversationDetails",
    "ConversationReply",
    "FindContactByEmailInput",
    "FindContactByEmailOutput",
    "GetConversationInput",
    "Message",
    "ReplyToConversationInput",
    "SearchConversationsInput",
    "SearchConversationsOutput",
    "UpdateConversationInput",
    "UpdateConversationOutput",
    "build_reply_html",
    "new_find_contact_by_email_step",
    "new_get_conversation_step",
    "new_reply_to_conversation_step",
    "new_search_conversations_step",
    "new_update_conversation_step",
]
