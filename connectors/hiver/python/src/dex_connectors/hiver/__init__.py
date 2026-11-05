# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Hiver shared inbox connector."""

from ._generated.config import Config, Credentials
from .client import Client
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    AddNoteInput,
    AddNoteOutput,
    Conversation,
    ConversationAssignee,
    ConversationDetails,
    ConversationMessage,
    CreateSharedDraftInput,
    GetConversationInput,
    Inbox,
    ListConversationsInput,
    ListConversationsOutput,
    ListInboxesInput,
    ListInboxesOutput,
    Note,
    SharedDraft,
    Tag,
    UpdateConversationInput,
    UpdateConversationOutput,
)
from .operations import (
    AddNoteOperation,
    CreateSharedDraftOperation,
    GetConversationOperation,
    ListConversationsOperation,
    ListInboxesOperation,
    UpdateConversationOperation,
)
from .steps import (
    new_add_note_step,
    new_create_shared_draft_step,
    new_get_conversation_step,
    new_list_conversations_step,
    new_list_inboxes_step,
    new_update_conversation_step,
)

__all__ = [
    "AddNoteInput",
    "AddNoteOperation",
    "AddNoteOutput",
    "Client",
    "Config",
    "Connection",
    "Conversation",
    "ConversationAssignee",
    "ConversationDetails",
    "ConversationMessage",
    "CreateSharedDraftInput",
    "CreateSharedDraftOperation",
    "Credentials",
    "GetConversationInput",
    "GetConversationOperation",
    "Inbox",
    "ListConversationsInput",
    "ListConversationsOperation",
    "ListConversationsOutput",
    "ListInboxesInput",
    "ListInboxesOperation",
    "ListInboxesOutput",
    "Note",
    "SharedDraft",
    "Tag",
    "UpdateConversationInput",
    "UpdateConversationOperation",
    "UpdateConversationOutput",
    "new_add_note_step",
    "new_connection",
    "new_connection_with_operations",
    "new_create_shared_draft_step",
    "new_get_conversation_step",
    "new_list_conversations_step",
    "new_list_inboxes_step",
    "new_project_connection",
    "new_update_conversation_step",
]
